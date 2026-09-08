package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const sessionCookie = "admin_session"

// requestScheme determine le protocole vu par le visiteur. Derriere un reverse
// proxy qui termine TLS, la requete arrive en clair : seul l'en-tete le dit.
func requestScheme(req *http.Request) string {
	if p := req.Header.Get("X-Forwarded-Proto"); p != "" {
		return strings.TrimSpace(strings.Split(p, ",")[0])
	}
	if req.TLS != nil {
		return "https"
	}
	return "http"
}

// publicOrigin renvoie l'origine publique du service (scheme://hote).
// PUBLIC_URL fait autorite si elle est definie, sinon on deduit de la requete.
func publicOrigin(req *http.Request) string {
	if raw := strings.TrimSpace(os.Getenv("PUBLIC_URL")); raw != "" {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			return u.Scheme + "://" + u.Host
		}
	}
	return requestScheme(req) + "://" + req.Host
}

// setSessionCookie pose le cookie de session. HttpOnly : contrairement a
// l'ancien schema qui stockait le mot de passe en clair, le jeton est
// inaccessible au JavaScript de la page.
func setSessionCookie(w http.ResponseWriter, req *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestScheme(req) == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 3600,
	})
	clearLegacyCookies(w)
}

func clearSessionCookie(w http.ResponseWriter, req *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestScheme(req) == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	clearLegacyCookies(w)
}

// clearLegacyCookies efface les cookies de l'ancien systeme, qui contenaient
// le nom d'utilisateur et le mot de passe en clair, lisibles en JavaScript.
func clearLegacyCookies(w http.ResponseWriter) {
	for _, name := range []string{"admin_username", "admin_password"} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1})
	}
}

func sessionToken(req *http.Request) string {
	c, err := req.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// currentAdmin resout l'administrateur connecte, ou nil.
func currentAdmin(req *http.Request) (*postsql.AdminUser, string) {
	token := sessionToken(req)
	if token == "" {
		return nil, ""
	}
	user, ok := postsql.LookupSession(token)
	if !ok {
		return nil, ""
	}
	return user, token
}

// requireAdmin protege une route d'API : renvoie 401 et false si la session
// est absente ou invalide.
func requireAdmin(w http.ResponseWriter, req *http.Request) (*postsql.AdminUser, string, bool) {
	user, token := currentAdmin(req)
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "NOT_AUTHENTICATED"})
		return nil, "", false
	}
	return user, token, true
}

// serveHTML sert une page en forcant le navigateur a revalider.
// Sans Cache-Control explicite, seul Last-Modified est envoye et les navigateurs
// appliquent leur cache heuristique : une page mise a jour peut alors rester
// invisible pendant des heures, avec un JavaScript perime qui dialogue avec un
// serveur qui, lui, a change.
func serveHTML(w http.ResponseWriter, req *http.Request, path string) {
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	http.ServeFile(w, req, path)
}

/* Reponses JSON */

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeOK(w http.ResponseWriter, payload interface{}) {
	writeJSON(w, http.StatusOK, payload)
}

func writeErr(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
