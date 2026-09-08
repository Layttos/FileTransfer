package routes

import "net/http"

func HandleDefault(w http.ResponseWriter, req *http.Request) {
	serveHTML(w, req, "public/index.html")
}
