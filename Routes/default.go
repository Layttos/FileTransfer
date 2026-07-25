package routes

import "net/http"

func HandleDefault(w http.ResponseWriter, req *http.Request) {
	http.ServeFile(w, req, "public/index.html")
}
