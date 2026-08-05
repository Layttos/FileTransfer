package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
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

func HandleUpload(w http.ResponseWriter, rq *http.Request) {
	if rq.Method != "POST" {
		fmt.Fprintf(w, "<h1>Can't identify the request</h1>")
		return
	}

	reader, err := rq.MultipartReader()
	if err != nil {
		fmt.Fprintf(w, "<h1>%s</h1>", err.Error())
	}

	for {
		part, err := reader.NextPart()
		if err == io.EOF { // zé fini de liiiire :3
			break
		}
		if err != nil {
			http.Error(w, "Mince ! Une erreur est survenue lors du téléversement...", http.StatusInternalServerError)
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
				http.Error(w, `{"error": "Server error creating directory"}`, http.StatusInternalServerError)
				return
			}

			dest, err := os.Create(filepath.Join(dirPath, fn))
			if err != nil {
				http.Error(w, `{"error":"Server error creating the file"}`, http.StatusInternalServerError)
				return
			}
			defer dest.Close()

			/*size, err := io.Copy(dest, part) // on va essayer de faire propre en copiant cette fois
			if err != nil {
				http.Error(w, `{"error":"Server error copying file"}`, http.StatusInternalServerError)
				return
			}*/

			buffer := bufferPool.Get().([]byte)
			size, err := io.CopyBuffer(dest, part, buffer)
			bufferPool.Put(buffer)
			dest.Close()

			if err != nil {
				http.Error(w, `{"error":"Server error copying the file"}`, http.StatusInternalServerError)
				return
			}

			ip, _, err := net.SplitHostPort(rq.RemoteAddr)
			if err != nil {
				ip = rq.RemoteAddr
			}

			postsql.PushFile(id, fn, size, ip, rq.URL.Query().Get("password"))
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
	http.Error(w, `{"error":"Can't identify the request or file not found"}`, http.StatusBadRequest)
}
