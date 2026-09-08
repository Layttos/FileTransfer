package postsql

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

/* Dossiers */

// CreateFolder materialise un dossier, y compris vide.
func CreateFolder(userID int, path string) (string, error) {
	ReconnectDB()
	p := NormalizeFolder(path)
	if p == "/" {
		return "", fmt.Errorf("la racine existe deja")
	}
	// ON CONFLICT plutot qu'une verification prealable : deux creations
	// simultanees ne doivent pas produire d'erreur.
	if _, err := connPool.Exec(`
		INSERT INTO personal_folders (user_id, path) VALUES ($1, $2)
		ON CONFLICT (user_id, path) DO NOTHING;`, userID, p); err != nil {
		return "", err
	}
	return p, nil
}

// PersonalSubfolders renvoie les sous-dossiers directs d'un dossier : ceux
// crees explicitement et ceux impliques par les fichiers qu'ils contiennent.
func PersonalSubfolders(userID int, folder string) []string {
	ReconnectDB()
	parent := NormalizeFolder(folder)
	seen := map[string]bool{}
	out := []string{}

	collect := func(query string) {
		rows, err := connPool.Query(query, userID, parent+"%", parent)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Listing des dossiers impossible : %v\n", err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var f string
			if err := rows.Scan(&f); err != nil {
				return
			}
			// On ne remonte que le premier niveau sous le dossier courant.
			rest := strings.TrimSuffix(strings.TrimPrefix(f, parent), "/")
			if rest == "" {
				continue
			}
			first := strings.Split(rest, "/")[0]
			if first != "" && !seen[first] {
				seen[first] = true
				out = append(out, first)
			}
		}
	}

	collect(`SELECT DISTINCT folder FROM personal_files
	         WHERE user_id = $1 AND folder LIKE $2 AND folder != $3`)
	collect(`SELECT path FROM personal_folders
	         WHERE user_id = $1 AND path LIKE $2 AND path != $3`)

	sort.Strings(out)
	return out
}

// FolderTree renvoie tous les fichiers situes dans un dossier et ses
// sous-dossiers, pour l'archivage ou la suppression recursive.
func FolderTree(userID int, folder string) []PersonalFile {
	ReconnectDB()
	root := NormalizeFolder(folder)
	out := []PersonalFile{}

	rows, err := connPool.Query(`
		SELECT id, file_name, file_size, folder, created_at
		FROM personal_files WHERE user_id = $1 AND folder LIKE $2
		ORDER BY folder, file_name;`, userID, root+"%")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Parcours du dossier impossible : %v\n", err)
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

// DeleteFolder supprime un dossier, ses sous-dossiers et tous leurs fichiers.
func DeleteFolder(userID int, folder string) (int, error) {
	ReconnectDB()
	root := NormalizeFolder(folder)
	if root == "/" {
		return 0, fmt.Errorf("impossible de supprimer la racine")
	}

	files := FolderTree(userID, root)
	for _, f := range files {
		if err := os.RemoveAll(filepath.Join(PersonalRoot(userID), f.ID)); err != nil {
			return 0, fmt.Errorf("suppression de %s impossible : %v", f.FileName, err)
		}
	}
	if _, err := connPool.Exec(
		"DELETE FROM personal_files WHERE user_id = $1 AND folder LIKE $2;", userID, root+"%"); err != nil {
		return 0, err
	}
	if _, err := connPool.Exec(
		"DELETE FROM personal_folders WHERE user_id = $1 AND path LIKE $2;", userID, root+"%"); err != nil {
		return 0, err
	}
	return len(files), nil
}

// RenameFolder deplace un dossier et tout son contenu vers un nouveau chemin.
func RenameFolder(userID int, oldPath, newName string) (string, error) {
	ReconnectDB()
	from := NormalizeFolder(oldPath)
	if from == "/" {
		return "", fmt.Errorf("impossible de renommer la racine")
	}
	newName = strings.TrimSpace(strings.ReplaceAll(newName, "/", ""))
	if newName == "" || newName == "." || newName == ".." {
		return "", fmt.Errorf("nom de dossier invalide")
	}

	// On remplace le dernier segment du chemin, le parent ne bouge pas.
	segments := strings.Split(strings.Trim(from, "/"), "/")
	segments[len(segments)-1] = newName
	to := NormalizeFolder("/" + strings.Join(segments, "/"))
	if to == from {
		return to, nil
	}

	var clash bool
	if err := connPool.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM personal_folders WHERE user_id = $1 AND path = $2
			UNION ALL
			SELECT 1 FROM personal_files WHERE user_id = $1 AND folder = $2
		);`, userID, to).Scan(&clash); err != nil {
		return "", err
	}
	if clash {
		return "", fmt.Errorf("un dossier porte deja ce nom")
	}

	// Le chemin n'est qu'une etiquette : rien ne bouge sur le disque, les
	// fichiers sont ranges par identifiant.
	if _, err := connPool.Exec(`
		UPDATE personal_files SET folder = $3 || substring(folder from char_length($2) + 1)
		WHERE user_id = $1 AND folder LIKE $2 || '%';`, userID, from, to); err != nil {
		return "", err
	}
	if _, err := connPool.Exec(`
		UPDATE personal_folders SET path = $3 || substring(path from char_length($2) + 1)
		WHERE user_id = $1 AND path LIKE $2 || '%';`, userID, from, to); err != nil {
		return "", err
	}
	return to, nil
}

/* Creation de fichier */

// PersonalCreateFile cree un fichier vide dans l'espace personnel.
func PersonalCreateFile(userID int, folder, name string) (*PersonalFile, error) {
	ReconnectDB()
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == "/" || strings.HasPrefix(name, "..") {
		return nil, fmt.Errorf("nom de fichier invalide")
	}

	id, err := GenPersonalID()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(PersonalRoot(userID), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	f.Close()

	if err := PersonalRecord(userID, id, name, 0, folder); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &PersonalFile{ID: id, FileName: name, FileSize: 0, Folder: NormalizeFolder(folder)}, nil
}

/* Partage vers l'espace public */

// ShareToPublic publie un fichier personnel dans l'espace de transfert public,
// avec un mot de passe facultatif. Le fichier est partage par lien materiel
// quand le systeme de fichiers le permet : pas de copie, et supprimer un cote
// laisse l'autre intact.
func ShareToPublic(userID int, fileID, password string) (string, error) {
	ReconnectDB()
	src, ok := PersonalGet(userID, fileID)
	if !ok {
		return "", fmt.Errorf("fichier introuvable")
	}

	srcPath := PersonalPath(userID, src.ID, src.FileName)
	info, err := os.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("fichier absent du disque")
	}

	publicID := GenFileID()
	dstDir := filepath.Join(os.Getenv("FILES_PATH"), publicID)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", err
	}
	dstPath := filepath.Join(dstDir, src.FileName)

	if err := os.Link(srcPath, dstPath); err != nil {
		// Lien impossible (montages differents) : on recopie.
		if cerr := copyFile(srcPath, dstPath); cerr != nil {
			os.RemoveAll(dstDir)
			return "", fmt.Errorf("partage impossible : %v", cerr)
		}
	}

	PushFile(publicID, src.FileName, info.Size(), "cloud personnel", password)
	return publicID, nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// PersonalSetSize met a jour la taille enregistree apres modification du contenu.
func PersonalSetSize(userID int, id string, size int64) error {
	ReconnectDB()
	_, err := connPool.Exec(
		"UPDATE personal_files SET file_size = $1 WHERE id = $2 AND user_id = $3;", size, id, userID)
	return err
}
