package routes

import (
	"filetransfer-backend/postsql"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Le marqueur de la page de telechargement, remplace par les metadonnees.
const ogMarker = "<!--OPENGRAPH-->"

var (
	downloadPageOnce sync.Once
	downloadPage     string
	downloadPageErr  error
)

func loadDownloadPage() (string, error) {
	downloadPageOnce.Do(func() {
		data, err := os.ReadFile("public/download.html")
		if err != nil {
			downloadPageErr = err
			return
		}
		downloadPage = string(data)
	})
	return downloadPage, downloadPageErr
}

// mediaKind classe un fichier d'apres son extension, pour choisir le type
// d'apercu que les messageries sauront afficher.
func mediaKind(name string) string {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(name), ".")) {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "avif":
		return "image"
	case "mp4", "webm", "mov", "m4v":
		return "video"
	case "mp3", "wav", "ogg", "flac", "m4a", "opus":
		return "audio"
	}
	return "file"
}

func mediaMime(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	switch ext {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "mp4", "m4v":
		return "video/mp4"
	case "webm":
		return "video/webm"
	case "mov":
		return "video/quicktime"
	}
	return "application/octet-stream"
}

// serveDownloadPage sert la page de telechargement en y injectant les
// metadonnees Open Graph du fichier. Sans elles, un lien colle dans Discord,
// Slack ou iMessage n'affiche qu'une URL brute : ces clients lisent uniquement
// les balises <meta> de la page, ils n'executent pas son JavaScript.
func serveDownloadPage(w http.ResponseWriter, req *http.Request, id string) {
	page, err := loadDownloadPage()
	if err != nil {
		http.Error(w, "Page indisponible", http.StatusInternalServerError)
		return
	}

	name := postsql.GetFileName(id)
	size := parseSize(postsql.GetFileSize(id))
	protected := postsql.HasPassword(id)
	kind := mediaKind(name)

	origin := publicOrigin(req)
	pageURL := origin + "/" + id
	mediaURL := pageURL + "?preview=true"

	esc := html.EscapeString
	var b strings.Builder

	add := func(property, content string) {
		fmt.Fprintf(&b, "\n    <meta property=\"%s\" content=\"%s\">", property, esc(content))
	}
	addName := func(n, content string) {
		fmt.Fprintf(&b, "\n    <meta name=\"%s\" content=\"%s\">", n, esc(content))
	}

	description := size
	if protected {
		description = size + " · protégé par mot de passe"
	}

	add("og:site_name", "FileTransfer")
	add("og:url", pageURL)
	add("og:title", name)
	add("og:description", description)
	addName("description", description)

	// Un fichier protege ne doit rien laisser voir de son contenu : l'apercu
	// serait accessible sans le mot de passe.
	switch {
	case protected:
		add("og:type", "website")
		addName("twitter:card", "summary")

	case kind == "image":
		add("og:type", "website")
		add("og:image", mediaURL)
		add("og:image:type", mediaMime(name))
		add("og:image:alt", name)
		addName("twitter:card", "summary_large_image")
		addName("twitter:image", mediaURL)

	case kind == "video":
		add("og:type", "video.other")
		add("og:video", mediaURL)
		add("og:video:secure_url", mediaURL)
		add("og:video:type", mediaMime(name))
		addName("twitter:card", "player")
		addName("twitter:player:stream", mediaURL)

	case kind == "audio":
		add("og:type", "music.song")
		add("og:audio", mediaURL)
		add("og:audio:type", mediaMime(name))
		addName("twitter:card", "summary")

	default:
		add("og:type", "website")
		addName("twitter:card", "summary")
	}

	addName("twitter:title", name)
	addName("twitter:description", description)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	fmt.Fprint(w, strings.Replace(page, ogMarker, b.String(), 1))
}
