package postsql

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx"
)

// PersonalFile est une entree du cloud personnel d'un administrateur.
type PersonalFile struct {
	ID        string    `json:"id"`
	FileName  string    `json:"file_name"`
	FileSize  int64     `json:"file_size"`
	Folder    string    `json:"folder"`
	CreatedAt time.Time `json:"created_at"`
}

// PersonalRoot est la racine de stockage d'un administrateur. Les fichiers
// personnels sont ranges a part des transferts publics pour qu'aucune route
// publique ne puisse les servir par construction.
func PersonalRoot(userID int) string {
	return filepath.Join(os.Getenv("FILES_PATH"), "personal", fmt.Sprintf("u%d", userID))
}

// PersonalPath renvoie le chemin disque complet d'un fichier personnel.
func PersonalPath(userID int, id, fileName string) string {
	return filepath.Join(PersonalRoot(userID), id, fileName)
}

// NormalizeFolder ramene un chemin de dossier a une forme sure : toujours
// absolu depuis la racine personnelle, sans "..", toujours termine par "/".
func NormalizeFolder(folder string) string {
	if folder == "" {
		return "/"
	}
	folder = strings.ReplaceAll(folder, "\\", "/")
	cleaned := filepath.ToSlash(filepath.Clean("/" + folder))
	if cleaned == "." || cleaned == "/" {
		return "/"
	}
	if !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	if len(cleaned) > 512 {
		return "/"
	}
	return cleaned
}

// GenPersonalID produit un identifiant libre pour un fichier personnel.
func GenPersonalID() (string, error) {
	ReconnectDB()
	for attempt := 0; attempt < 20; attempt++ {
		id, err := randomID(12)
		if err != nil {
			return "", err
		}
		var exists bool
		if err := connPool.QueryRow(
			"SELECT EXISTS(SELECT 1 FROM personal_files WHERE id = $1);", id).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("impossible de generer un identifiant libre")
}

// PersonalRecord enregistre un fichier deja ecrit sur le disque.
func PersonalRecord(userID int, id, fileName string, size int64, folder string) error {
	ReconnectDB()
	_, err := connPool.Exec(`
		INSERT INTO personal_files (id, user_id, file_name, file_size, folder)
		VALUES ($1, $2, $3, $4, $5);`,
		id, userID, fileName, size, NormalizeFolder(folder))
	return err
}

// PersonalList renvoie les fichiers d'un dossier donne.
func PersonalList(userID int, folder string) []PersonalFile {
	ReconnectDB()
	out := []PersonalFile{}
	rows, err := connPool.Query(`
		SELECT id, file_name, file_size, folder, created_at
		FROM personal_files WHERE user_id = $1 AND folder = $2
		ORDER BY created_at DESC;`, userID, NormalizeFolder(folder))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Listing du cloud personnel impossible : %v\n", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var f PersonalFile
		if err := rows.Scan(&f.ID, &f.FileName, &f.FileSize, &f.Folder, &f.CreatedAt); err != nil {
			return out
		}
		out = append(out, f)
	}
	return out
}

// PersonalSubfolders renvoie les sous-dossiers directs d'un dossier.
// Les dossiers n'existent qu'a travers les fichiers qu'ils contiennent.
func PersonalSubfolders(userID int, folder string) []string {
	ReconnectDB()
	parent := NormalizeFolder(folder)
	out := []string{}
	seen := map[string]bool{}

	rows, err := connPool.Query(`
		SELECT DISTINCT folder FROM personal_files
		WHERE user_id = $1 AND folder LIKE $2 AND folder != $3
		ORDER BY folder;`, userID, parent+"%", parent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Listing des dossiers impossible : %v\n", err)
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return out
		}
		// On ne remonte que le premier niveau sous le dossier courant.
		rest := strings.TrimPrefix(f, parent)
		rest = strings.TrimSuffix(rest, "/")
		if rest == "" {
			continue
		}
		first := strings.Split(rest, "/")[0]
		if first != "" && !seen[first] {
			seen[first] = true
			out = append(out, first)
		}
	}
	return out
}

// PersonalGet renvoie un fichier personnel, a condition qu'il appartienne bien
// a l'administrateur qui le demande.
func PersonalGet(userID int, id string) (*PersonalFile, bool) {
	ReconnectDB()
	var f PersonalFile
	err := connPool.QueryRow(`
		SELECT id, file_name, file_size, folder, created_at
		FROM personal_files WHERE id = $1 AND user_id = $2;`, id, userID,
	).Scan(&f.ID, &f.FileName, &f.FileSize, &f.Folder, &f.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, false
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Lecture d'un fichier personnel impossible : %v\n", err)
		return nil, false
	}
	return &f, true
}

// PersonalDelete supprime un fichier personnel et son contenu sur le disque.
func PersonalDelete(userID int, id string) error {
	ReconnectDB()
	f, ok := PersonalGet(userID, id)
	if !ok {
		return fmt.Errorf("fichier introuvable")
	}
	if err := os.RemoveAll(filepath.Join(PersonalRoot(userID), f.ID)); err != nil {
		return fmt.Errorf("suppression sur le disque impossible : %v", err)
	}
	_, err := connPool.Exec("DELETE FROM personal_files WHERE id = $1 AND user_id = $2;", id, userID)
	return err
}

// PersonalRename change le nom affiche et le nom sur le disque.
func PersonalRename(userID int, id, newName string) error {
	ReconnectDB()
	newName = filepath.Base(strings.TrimSpace(newName))
	if newName == "" || newName == "." || newName == "/" || strings.HasPrefix(newName, "..") {
		return fmt.Errorf("nom de fichier invalide")
	}
	f, ok := PersonalGet(userID, id)
	if !ok {
		return fmt.Errorf("fichier introuvable")
	}
	oldPath := PersonalPath(userID, f.ID, f.FileName)
	newPath := PersonalPath(userID, f.ID, newName)
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("renommage sur le disque impossible : %v", err)
	}
	_, err := connPool.Exec(
		"UPDATE personal_files SET file_name = $1 WHERE id = $2 AND user_id = $3;", newName, id, userID)
	return err
}

// PersonalMove deplace un fichier dans un autre dossier (purement logique).
func PersonalMove(userID int, id, folder string) error {
	ReconnectDB()
	if _, ok := PersonalGet(userID, id); !ok {
		return fmt.Errorf("fichier introuvable")
	}
	_, err := connPool.Exec(
		"UPDATE personal_files SET folder = $1 WHERE id = $2 AND user_id = $3;",
		NormalizeFolder(folder), id, userID)
	return err
}

// PersonalUsage renvoie le nombre de fichiers et l'espace occupe par un compte.
func PersonalUsage(userID int) (int64, int64) {
	ReconnectDB()
	var count, size int64
	if err := connPool.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(file_size), 0) FROM personal_files WHERE user_id = $1;`,
		userID).Scan(&count, &size); err != nil {
		return 0, 0
	}
	return count, size
}
