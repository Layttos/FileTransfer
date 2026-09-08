package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
	"filetransfer-backend/vault"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 1024*1024)
	},
}

// Derriere un reverse proxy (Nginx Proxy Manager), RemoteAddr vaut l'IP du
// proxy et non celle du visiteur. On prefere donc les en-tetes qu'il pose.
// Le conteneur n'expose aucun port et n'est joignable que par le proxy :
// ces en-tetes ne peuvent pas etre falsifies depuis l'exterieur.
func clientIP(rq *http.Request) string {
	if ip := strings.TrimSpace(rq.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	if fwd := rq.Header.Get("X-Forwarded-For"); fwd != "" {
		if ip := strings.TrimSpace(strings.Split(fwd, ",")[0]); ip != "" {
			return ip
		}
	}
	ip, _, err := net.SplitHostPort(rq.RemoteAddr)
	if err != nil {
		return rq.RemoteAddr
	}
	return ip
}

func HandleUpload(w http.ResponseWriter, rq *http.Request) {
	if rq.Method != "POST" {
		fmt.Fprintf(w, "<h1>Can't identify the request</h1>")
		return
	}

	reader, err := rq.MultipartReader()
	if err != nil {
		// Sans ce return, reader est nil et la boucle ci-dessous dereference nil.
		writeUploadError(w, http.StatusBadRequest, "Requete multipart invalide")
		return
	}

	for {
		part, err := reader.NextPart()
		if err == io.EOF { // zé fini de liiiire :3
			break
		}
		if err != nil {
			writeUploadError(w, http.StatusInternalServerError, "Erreur pendant la lecture du flux envoye")
			return
		}

		if part.FormName() == "file" {
			fn := filepath.Base(part.FileName())
			if fn == "" || strings.HasPrefix(fn, ".") || strings.HasPrefix(fn, "_") { // on va éviter les conneries ici
				continue
			}

			id := postsql.GenFileID()

			dirPath := filepath.Join(os.Getenv("FILES_PATH"), id)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				writeUploadError(w, http.StatusInternalServerError, "Impossible de creer le repertoire de destination")
				return
			}

			dest, err := os.Create(filepath.Join(dirPath, fn))
			if err != nil {
				os.RemoveAll(dirPath)
				writeUploadError(w, http.StatusInternalServerError, "Impossible de creer le fichier sur le serveur")
				return
			}

			// Le contenu traverse le coffre : chiffre, et compresse s'il s'y prete.
			enc, encErr := vault.NewWriter(dest)
			if encErr != nil {
				dest.Close()
				os.RemoveAll(dirPath)
				writeUploadError(w, http.StatusInternalServerError, "Chiffrement indisponible")
				return
			}

			buffer := bufferPool.Get().([]byte)
			size, err := io.CopyBuffer(enc, part, buffer)
			bufferPool.Put(buffer)
			if err == nil {
				err = enc.Close()
			}
			closeErr := dest.Close()

			// Sur un transfert de plusieurs Go, une coupure client ou un disque plein
			// laisse un fichier tronque. Aucune ligne n'est encore ecrite en base :
			// on efface le repertoire pour ne pas accumuler d'orphelins sur le disque.
			if err != nil || closeErr != nil {
				os.RemoveAll(dirPath)
				if err == nil {
					err = closeErr
				}
				fmt.Fprintf(os.Stderr, "Upload interrompu (%s, %d octets ecrits) : %v\n", fn, size, err)
				auditAnon(rq, postsql.LevelError, postsql.CatUpload, "depot-interrompu", fn,
					fmt.Sprintf("%d octets ecrits : %v", size, err))
				writeUploadError(w, http.StatusInternalServerError, "Transfert interrompu avant la fin")
				return
			}

			ip := clientIP(rq)

			postsql.PushFile(id, fn, size, ip, rq.URL.Query().Get("password"))

			protege := ""
			if rq.URL.Query().Get("password") != "" {
				protege = ", protege par mot de passe"
			}
			auditAnon(rq, postsql.LevelInfo, postsql.CatUpload, "depot", id,
				fmt.Sprintf("%s (%d octets%s)", fn, size, protege))
			response := map[string]interface{}{
				"id":   id,
				"size": size,
				"ip":   ip,
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
			return
		}
	}
	writeUploadError(w, http.StatusBadRequest, "Aucun fichier trouve dans la requete")
}

// writeUploadError renvoie une erreur en JSON, avec le bon code HTTP, pour que le
// frontend puisse afficher autre chose qu'un generique "Erreur serveur".
func writeUploadError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
