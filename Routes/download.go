package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func HandleFile(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")

	if req.Method == http.MethodGet {

		if postsql.Exists(id) == false {
			auditAnon(req, postsql.LevelWarn, postsql.CatDownload, "introuvable", id, "")
			serveHTML(w, req, "public/404.html")
			return
		}

		if len(req.URL.Query()) == 0 {
			serveHTML(w, req, "public/download.html")
			return
		}

		if req.URL.Query().Get("jsonInfo") == "only" {
			w.Header().Set("Content-Type", "application/json")
			response := map[string]interface{}{
				"filename": postsql.GetFileName(id),
				"size":     parseSize(postsql.GetFileSize(id)),
			}

			json.NewEncoder(w).Encode(response)
			return

		}

		isDownload := req.URL.Query().Get("download") == "true"
		isPreview := req.URL.Query().Get("preview") == "true"

		if isDownload || isPreview {

			// La condition etait inversee : le fichier n'etait refuse que lorsque le
			// mot de passe etait CORRECT, et servi sinon. Tout fichier protege etait
			// donc telechargeable sans mot de passe.
			if postsql.HasPassword(id) && !passwordAccepted(id, req) {
				auditAnon(req, postsql.LevelWarn, postsql.CatDownload, "mot-de-passe-refuse", id,
					postsql.GetFileName(id))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": "Le mot de passe est requis pour ce fichier ou vous avez entré le mauvais mot de passe",
				})
				return
			}

			action := "telechargement"
			if isPreview {
				action = "apercu"
			}
			auditAnon(req, postsql.LevelInfo, postsql.CatDownload, action, id, postsql.GetFileName(id))

			fullPath := filepath.Join(os.Getenv("FILES_PATH"), id, postsql.GetFileName(id))
			if isDownload {
				w.Header().Set("Content-Disposition", "attachment; filename=\""+postsql.GetFileName(id)+"\"")
				w.Header().Set("Content-Type", "application/octet-stream")
			} else if isPreview {
				w.Header().Set("Content-Disposition", "inline; filename=\""+postsql.GetFileName(id)+"\"")
			}
			http.ServeFile(w, req, fullPath)
			return
		}

	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(`{"id": "` + id + `"}`)
}

// passwordAccepted verifie le mot de passe d'un fichier protege. Il peut arriver
// par l'en-tete X-File-Password, ce qu'envoie la page de telechargement, ou par
// le parametre d'URL pour un lien direct. L'ancienne version exigeait les deux
// a la fois, ce qu'aucun client n'envoyait.
func passwordAccepted(id string, req *http.Request) bool {
	if candidate := req.URL.Query().Get("password"); candidate != "" && postsql.IsPassword(id, candidate) {
		return true
	}
	if candidate := req.Header.Get("X-File-Password"); candidate != "" && postsql.IsPassword(id, candidate) {
		return true
	}
	return false
}

func parseSize(bytes int64) string {
	sign := ""
	if bytes < 0 {
		sign = "-"
		bytes = -bytes
	}

	if bytes == 0 {
		return "0 octet"
	}
	units := []string{"o", "ko", "Mo", "Go", "To", "Po", "Eo"}
	value := float64(bytes)
	unitIndex := 0

	for value >= 1024 && unitIndex < len(units)-1 {
		value /= 1024
		unitIndex++
	}

	formattedSize := fmt.Sprintf("%.2f", value)
	formattedSize = strings.TrimRight(formattedSize, "0")
	formattedSize = strings.TrimRight(formattedSize, ".")

	return fmt.Sprintf("%s%s %s", sign, formattedSize, units[unitIndex])
}
