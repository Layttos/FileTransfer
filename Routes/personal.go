package routes

import (
	"archive/zip"
	"filetransfer-backend/postsql"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// HandlePersonalUpload recoit un fichier dans l'espace personnel de
// l'administrateur connecte. Meme approche que le transfert public : lecture en
// flux vers le disque, sans limite de taille et sans bufferisation en memoire.
func HandlePersonalUpload(w http.ResponseWriter, req *http.Request) {
	user, _, ok := requireAdmin(w, req)
	if !ok {
		return
	}
	if req.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "Methode non autorisee")
		return
	}

	folder := postsql.NormalizeFolder(req.URL.Query().Get("folder"))

	reader, err := req.MultipartReader()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Requete multipart invalide")
		return
	}

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Erreur pendant la lecture du flux envoye")
			return
		}
		if part.FormName() != "file" {
			continue
		}

		fn := filepath.Base(part.FileName())
		if fn == "" || fn == "." || strings.HasPrefix(fn, "..") {
			continue
		}

		id, err := postsql.GenPersonalID()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Identifiant indisponible")
			return
		}

		dirPath := filepath.Join(postsql.PersonalRoot(user.ID), id)
		if err := os.MkdirAll(dirPath, 0o755); err != nil {
			writeErr(w, http.StatusInternalServerError, "Impossible de creer le repertoire de destination")
			return
		}

		dest, err := os.Create(filepath.Join(dirPath, fn))
		if err != nil {
			os.RemoveAll(dirPath)
			writeErr(w, http.StatusInternalServerError, "Impossible de creer le fichier sur le serveur")
			return
		}

		buffer := bufferPool.Get().([]byte)
		size, err := io.CopyBuffer(dest, part, buffer)
		bufferPool.Put(buffer)
		closeErr := dest.Close()

		if err != nil || closeErr != nil {
			os.RemoveAll(dirPath)
			if err == nil {
				err = closeErr
			}
			fmt.Fprintf(os.Stderr, "Upload personnel interrompu (%s, %d octets) : %v\n", fn, size, err)
			writeErr(w, http.StatusInternalServerError, "Transfert interrompu avant la fin")
			return
		}

		if err := postsql.PersonalRecord(user.ID, id, fn, size, folder); err != nil {
			os.RemoveAll(dirPath)
			writeErr(w, http.StatusInternalServerError, "Enregistrement impossible : "+err.Error())
			return
		}

		audit(req, user, postsql.LevelInfo, postsql.CatPersonal, "depot", id,
			fmt.Sprintf("%s%s (%d octets)", folder, fn, size))
		writeOK(w, map[string]interface{}{
			"id": id, "file_name": fn, "file_size": size, "folder": folder,
		})
		return
	}

	writeErr(w, http.StatusBadRequest, "Aucun fichier trouve dans la requete")
}

// HandlePersonalDownload sert un fichier de l'espace personnel. La requete SQL
// filtre sur l'identifiant du proprietaire : un administrateur ne peut pas
// atteindre l'espace d'un autre, meme en devinant un identifiant.
func HandlePersonalDownload(w http.ResponseWriter, req *http.Request) {
	user, _ := currentAdmin(req)
	if user == nil {
		http.Redirect(w, req, "/admin/login", http.StatusFound)
		return
	}

	id := req.URL.Query().Get("id")
	f, ok := postsql.PersonalGet(user.ID, id)
	if !ok {
		http.Error(w, "Fichier introuvable", http.StatusNotFound)
		return
	}

	fullPath := postsql.PersonalPath(user.ID, f.ID, f.FileName)
	if _, err := os.Stat(fullPath); err != nil {
		http.Error(w, "Fichier absent du disque", http.StatusNotFound)
		return
	}

	audit(req, user, postsql.LevelInfo, postsql.CatPersonal, "telechargement", f.ID, f.FileName)

	disposition := "attachment"
	if req.URL.Query().Get("preview") == "true" {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename=\""+sanitizeFilename(f.FileName)+"\"")
	if disposition == "attachment" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	http.ServeFile(w, req, fullPath)
}

// HandlePersonalZip envoie un dossier de l'espace personnel sous forme
// d'archive zip, construite en flux : rien n'est ecrit sur le disque ni garde
// en memoire, un dossier de plusieurs gigaoctets passe donc sans difficulte.
func HandlePersonalZip(w http.ResponseWriter, req *http.Request) {
	user, _ := currentAdmin(req)
	if user == nil {
		http.Redirect(w, req, "/admin/login", http.StatusFound)
		return
	}

	folder := postsql.NormalizeFolder(req.URL.Query().Get("folder"))
	files := postsql.FolderTree(user.ID, folder)

	name := "cloud"
	if folder != "/" {
		parts := strings.Split(strings.Trim(folder, "/"), "/")
		name = parts[len(parts)-1]
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(name)+".zip\"")

	zw := zip.NewWriter(w)
	defer zw.Close()

	for _, f := range files {
		// Chemin relatif au dossier demande, pour conserver l'arborescence.
		rel := strings.TrimPrefix(f.Folder, folder) + f.FileName

		header := &zip.FileHeader{Name: rel, Method: zip.Deflate, Modified: f.CreatedAt}
		entry, err := zw.CreateHeader(header)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Archive : entree %s impossible : %v\n", rel, err)
			return
		}

		src, err := os.Open(postsql.PersonalPath(user.ID, f.ID, f.FileName))
		if err != nil {
			// Un fichier absent du disque ne doit pas faire echouer toute l'archive.
			fmt.Fprintf(os.Stderr, "Archive : %s illisible, ignore : %v\n", rel, err)
			continue
		}

		buffer := bufferPool.Get().([]byte)
		_, err = io.CopyBuffer(entry, src, buffer)
		bufferPool.Put(buffer)
		src.Close()

		if err != nil {
			fmt.Fprintf(os.Stderr, "Archive : copie de %s interrompue : %v\n", rel, err)
			return
		}
	}

	audit(req, user, postsql.LevelInfo, postsql.CatPersonal, "archive-dossier", folder,
		fmt.Sprintf("%d fichier(s)", len(files)))
}
