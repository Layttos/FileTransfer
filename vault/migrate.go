package vault

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IsEncrypted indique si un fichier porte deja l'en-tete du coffre.
func IsEncrypted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	header := make([]byte, 4)
	if _, err := io.ReadFull(f, header); err != nil {
		return false
	}
	return string(header) == string(magic[:])
}

// encryptInPlace chiffre un fichier existant. L'ecriture passe par un fichier
// temporaire suivi d'un renommage : une migration interrompue laisse l'original
// intact, et il reste lisible puisque le lecteur accepte les deux formats.
func encryptInPlace(path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	tmpPath := path + ".vault-tmp"
	dst, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	w, err := NewWriter(dst)
	if err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return err
	}

	buffer := make([]byte, frameSize)
	if _, err := io.CopyBuffer(w, src, buffer); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := w.Close(); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return err
	}
	// On force l'ecriture sur le disque avant de remplacer l'original.
	if err := dst.Sync(); err != nil {
		dst.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// MigrateAll chiffre les fichiers deposes avant l'activation du coffre.
// Appelee en arriere-plan au demarrage : le service repond immediatement, et les
// fichiers non encore convertis restent lisibles pendant l'operation.
func MigrateAll(root string) {
	if !enabled {
		return
	}

	var converted, skipped, failed int
	var bytesDone int64

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		// La cle et les restes d'une migration precedente ne sont pas des donnees.
		if base == ".storage-key" || strings.HasSuffix(base, ".vault-tmp") ||
			strings.HasSuffix(base, ".tmp-save") {
			return nil
		}
		if IsEncrypted(path) {
			skipped++
			return nil
		}
		if e := encryptInPlace(path); e != nil {
			fmt.Fprintf(os.Stderr, "Migration de %s impossible : %v\n", path, e)
			failed++
			return nil
		}
		converted++
		bytesDone += info.Size()
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Parcours du stockage impossible : %v\n", err)
	}

	if converted > 0 || failed > 0 {
		fmt.Printf("Chiffrement du stockage : %d fichier(s) convertis (%.1f Mio), %d deja chiffres, %d en echec\n",
			converted, float64(bytesDone)/(1<<20), skipped, failed)
	} else {
		fmt.Printf("Chiffrement du stockage : %d fichier(s) deja chiffres, rien a convertir\n", skipped)
	}
}
