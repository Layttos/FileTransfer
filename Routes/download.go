package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

var reqBody struct {
	Password string `json:"X-File-Password"`
}

func HandleFile(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")

	if req.Method == http.MethodGet {

		if postsql.Exists(id) == false {
			http.ServeFile(w, req, "public/404.html")
			return
		}

		if len(req.URL.Query()) == 0 {
			http.ServeFile(w, req, "public/download.html")
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

			if postsql.HasPassword(id) == true {
				// Décentralisation de la vérification du mdp (azy la condition était tarpin longue sinon)
				if verifyPassword(id, req) {
					w.Header().Set("Content-Type", "application/json")
					response := map[string]interface{}{
						"error": "Le mot de passe est requis pour ce fichier ou vous avez entré le mauvais mot de passe",
					}

					json.NewEncoder(w).Encode(response)
					return

				}
			}

			fullPath := filepath.Join("./files", id, postsql.GetFileName(id))
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

func verifyPassword(id string, req *http.Request) bool {

	_ = json.NewDecoder(req.Body).Decode(&reqBody)
	password := req.Header.Get("X-File-Password")

	if len(req.URL.Query()) == 1 || req.URL.Query().Get("password") == "" || postsql.IsPassword(id, req.URL.Query().Get("password")) == false || password == "" || !postsql.IsPassword(id, password) {
		return false
	}
	return true
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
