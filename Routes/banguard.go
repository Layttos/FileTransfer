package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// BanGuard refuse l'acces au site aux adresses bannies. Il enveloppe l'ensemble
// des routes : un bannissement ferme la porte partout, pas seulement sur la
// connexion — page d'accueil, telechargements, API, tout est refuse.
//
// La verification lit un cache memoire, elle ne coute donc rien meme sur le
// chemin d'un telechargement de plusieurs gigaoctets.
func BanGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ip := clientIP(req)

		until, banned := postsql.IsBanned(ip)
		if !banned {
			next.ServeHTTP(w, req)
			return
		}

		// Soupape : une session administrateur valable passe malgre le
		// bannissement. Sans cela, un administrateur dont l'adresse serait bannie
		// ne pourrait plus atteindre le panneau pour lever ce bannissement.
		// La verification ne coute une requete que pour une adresse deja bannie.
		if user, _ := currentAdmin(req); user != nil {
			next.ServeHTTP(w, req)
			return
		}

		remaining := int(time.Until(until).Minutes()) + 1 // durée issue du cache, cohérente
		w.Header().Set("Retry-After", strconv.Itoa(remaining*60))
		w.Header().Set("Cache-Control", "no-store")

		// Un appel d'API attend du JSON, un navigateur une page lisible.
		if wantsJSON(req) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "Accès refusé : cette adresse est bannie.",
				"banned":  true,
				"minutes": remaining,
			})
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, bannedPage(remaining))
	})
}

func wantsJSON(req *http.Request) bool {
	if strings.Contains(req.Header.Get("Content-Type"), "application/json") {
		return true
	}
	accept := req.Header.Get("Accept")
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}

// bannedPage est autonome : elle ne charge ni feuille de style ni script, pour
// rester lisible meme si tout le reste du site est refuse a cette adresse.
func bannedPage(minutes int) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="fr">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Accès refusé</title>
<style>
  :root { color-scheme: light dark; }
  body { margin:0; min-height:100vh; display:flex; align-items:center; justify-content:center;
         font-family: system-ui, -apple-system, "Segoe UI", sans-serif;
         background:#f3f4f6; color:#111827; padding:1.5rem; }
  @media (prefers-color-scheme: dark) { body { background:#0f172a; color:#f8fafc; } }
  .card { max-width:30rem; text-align:center; }
  h1 { font-size:1.75rem; margin:0 0 .75rem; }
  p { margin:.5rem 0; line-height:1.6; opacity:.85; }
  .en { margin-top:1.5rem; padding-top:1.25rem; border-top:1px solid rgba(128,128,128,.3); }
</style>
</head>
<body>
  <div class="card">
    <h1>Accès refusé</h1>
    <p>Cette adresse IP est bannie de ce service.</p>
    <p>Le bannissement expire dans <strong>%d minute(s)</strong>.</p>
    <div class="en">
      <p><strong>Access denied.</strong> This IP address is banned from this service.</p>
      <p>The ban expires in <strong>%d minute(s)</strong>.</p>
    </div>
  </div>
</body>
</html>`, minutes, minutes)
}
