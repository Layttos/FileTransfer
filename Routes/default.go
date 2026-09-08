package routes

import "net/http"

func HandleDefault(w http.ResponseWriter, req *http.Request) {
	serveHTML(w, req, "public/index.html")
}

// HandleAPIDoc sert la reference de l'API, en acces libre.
func HandleAPIDoc(w http.ResponseWriter, req *http.Request) {
	serveHTML(w, req, "public/api.html")
}
