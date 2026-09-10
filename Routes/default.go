package routes

import (
	"net/http"
	"path/filepath"
	"strings"
)

func HandleDefault(w http.ResponseWriter, req *http.Request) {
	serveHTML(w, req, "public/index.html")
}

// HandleAssets sert les ressources partagees entre les pages (traductions,
// scripts communs). Sans cette route, chaque page devrait embarquer sa propre
// copie du dictionnaire.
func HandleAssets(w http.ResponseWriter, req *http.Request) {
	name := strings.TrimPrefix(req.URL.Path, "/assets/")
	// Aucun chemin ne doit sortir du dossier des ressources.
	if name == "" || strings.Contains(name, "..") {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	http.ServeFile(w, req, filepath.Join("public/assets", filepath.Base(name)))
}

// HandleAPIDoc sert la reference de l'API, en acces libre.
//
// La page est de la prose : plutot que de decouper des centaines de phrases en
// cles de traduction, deux fichiers coexistent et le serveur choisit. Le
// parametre ?lang= fait autorite, sinon on suit l'en-tete Accept-Language.
func HandleAPIDoc(w http.ResponseWriter, req *http.Request) {
	page := "public/api.en.html"
	if preferredLang(req) == "fr" {
		page = "public/api.html"
	}
	serveHTML(w, req, page)
}

// preferredLang resout la langue voulue pour une page rendue par le serveur.
func preferredLang(req *http.Request) string {
	switch req.URL.Query().Get("lang") {
	case "fr":
		return "fr"
	case "en":
		return "en"
	}

	// Accept-Language : "fr-FR,fr;q=0.9,en;q=0.8". On ne regarde que la
	// premiere langue citee, la plus souhaitee.
	header := strings.ToLower(strings.TrimSpace(req.Header.Get("Accept-Language")))
	if header == "" {
		return "en"
	}
	first := strings.TrimSpace(strings.Split(header, ",")[0])
	if strings.HasPrefix(first, "fr") {
		return "fr"
	}
	return "en"
}
