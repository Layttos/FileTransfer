// Package vault chiffre les fichiers stockes sur le disque, et les compresse
// quand cela vaut la peine.
//
// Format d'un fichier chiffre :
//
//	en-tete 48 octets :
//	  0  [4]  "FTE1"
//	  4  [1]  version
//	  5  [1]  drapeaux : bit 0 = contenu compresse
//	  6  [2]  reserve
//	  8  [4]  taille des trames en clair
//	  12 [16] sel, propre au fichier
//	  28 [8]  base du nonce
//	  36 [8]  taille en clair du fichier
//	  44 [4]  reserve
//	puis, pour chaque trame : [4] longueur du chiffre, puis le chiffre.
//
// Chaque trame est scellee en AES-256-GCM avec un nonce forme de la base et de
// l'indice de la trame, ce qui interdit de les reordonner. La derniere porte une
// donnee associee differente, ce qui interdit de tronquer le fichier.
//
// Une trame non compressee a toujours la meme longueur : la position d'une trame
// se calcule alors directement, et les requetes Range restent servies sans lire
// le fichier depuis le debut. C'est ce qui fait fonctionner le deplacement dans
// une video et la reprise de telechargement.
package vault

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"
)

const (
	headerSize = 48
	frameSize  = 1 << 20 // 1 Mio en clair
	gcmOvert   = 16      // etiquette d'authentification
	lenPrefix  = 4

	flagCompressed = 1 << 0

	// En dessous de ce gain, compresser ne rapporte rien et coute du debit.
	minCompressionGain = 0.10
)

var magic = [4]byte{'F', 'T', 'E', '1'}

var (
	masterKey []byte
	enabled   bool
)

// Enabled indique si le chiffrement est actif.
func Enabled() bool { return enabled }

// Init charge la cle maitre. STORAGE_KEY fait autorite ; sinon une cle est
// generee et rangee a cote des donnees, ce qui protege moins et est signale.
func Init(filesPath string) error {
	if raw := os.Getenv("STORAGE_KEY"); raw != "" {
		key, err := hex.DecodeString(raw)
		if err != nil || len(key) != 32 {
			return fmt.Errorf("STORAGE_KEY doit etre 64 caracteres hexadecimaux (32 octets)")
		}
		masterKey, enabled = key, true
		fmt.Println("Chiffrement du stockage actif (cle fournie par STORAGE_KEY)")
		return nil
	}

	keyPath := filepath.Join(filesPath, ".storage-key")
	if data, err := os.ReadFile(keyPath); err == nil {
		key, err := hex.DecodeString(string(data))
		if err != nil || len(key) != 32 {
			return fmt.Errorf("cle de stockage illisible dans %s", keyPath)
		}
		masterKey, enabled = key, true
		fmt.Println("Chiffrement du stockage actif (cle lue dans " + keyPath + ")")
		return nil
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	if err := os.MkdirAll(filesPath, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0o600); err != nil {
		return err
	}
	masterKey, enabled = key, true

	fmt.Println("Chiffrement du stockage actif : une cle a ete generee dans " + keyPath)
	fmt.Println("  ATTENTION : cette cle est rangee a cote des donnees qu'elle protege.")
	fmt.Println("  Une sauvegarde du dossier emporte donc la cle avec les fichiers.")
	fmt.Println("  Pour une protection reelle, mettez-la dans STORAGE_KEY et retirez le fichier :")
	fmt.Println("    STORAGE_KEY=" + hex.EncodeToString(key))
	return nil
}

// deriveKey produit la cle d'un fichier a partir de la cle maitre et de son sel.
// Chaque fichier a ainsi sa propre cle, et un fichier partage par lien materiel
// reste dechiffrable puisque son sel voyage dans son en-tete.
func deriveKey(salt []byte) ([]byte, error) {
	key := make([]byte, 32)
	r := hkdf.New(sha256.New, masterKey, salt, []byte("filetransfer-file-v1"))
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

func frameNonce(base []byte, index uint32) []byte {
	nonce := make([]byte, 12)
	copy(nonce, base)
	binary.BigEndian.PutUint32(nonce[8:], index)
	return nonce
}

// aadFor distingue la derniere trame des autres, ce qui empeche de tronquer un
// fichier sans que le dechiffrement echoue.
func aadFor(last bool) []byte {
	if last {
		return []byte{1}
	}
	return []byte{0}
}
