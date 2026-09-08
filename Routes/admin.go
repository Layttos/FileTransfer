package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type rqBody struct {
	// Inscription et connexion
	Email      string `json:"email"`
	Password   string `json:"password"`
	Username   string `json:"username"`
	FirstName  string `json:"firstName"`
	LastName   string `json:"lastName"`
	InviteCode string `json:"inviteCode"`
	Action     string `json:"action"`

	// Fichiers publics
	FileID    string `json:"fileID"`
	NewName   string `json:"newFileName"`
	NewFileID string `json:"newFileID"`

	// Compte et securite
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	AuthPolicy      string `json:"authPolicy"`
	SessionID       string `json:"sessionID"`
	CredID          int    `json:"credID"`
	CredName        string `json:"credName"`

	// Administrateurs et invitations
	AdminID int    `json:"adminID"`
	Token   string `json:"token"`

	// Cloud personnel
	Folder string `json:"folder"`

	// Journal
	Category string `json:"category"`
	Level    string `json:"level"`
	Search   string `json:"search"`
	Offset   int    `json:"offset"`
	Limit    int    `json:"limit"`
}

func writeJSONMessage(w http.ResponseWriter, message string) {
	writeOK(w, map[string]string{"message": message})
}

/* Pages */

// HandleAdmin redirige vers le tableau de bord.
func HandleAdmin(w http.ResponseWriter, req *http.Request) {
	http.Redirect(w, req, "/admin/dashboard", http.StatusFound)
}

// HandleAdminDashboard sert le tableau de bord. La page est protegee ici, et
// chaque appel d'API revalide la session de son cote.
func HandleAdminDashboard(w http.ResponseWriter, req *http.Request) {
	if user, _ := currentAdmin(req); user == nil {
		http.Redirect(w, req, "/admin/login", http.StatusFound)
		return
	}
	http.ServeFile(w, req, "./public/admin/dashboard.html")
}

func HandleAdminLogin(w http.ResponseWriter, req *http.Request) {
	if user, _ := currentAdmin(req); user != nil {
		http.Redirect(w, req, "/admin/dashboard", http.StatusFound)
		return
	}
	http.ServeFile(w, req, "./public/admin/login.html")
}

func HandleAdminRegister(w http.ResponseWriter, req *http.Request) {
	http.ServeFile(w, req, "./public/admin/signin.html")
}

/* Telechargement administrateur */

// HandleAdminDownload sert un fichier depuis le panneau d'administration.
//
// C'est une route GET pour que le navigateur puisse la suivre directement :
// l'ancienne version faisait un POST dont le corps etait jete, puis naviguait
// vers une URL sans identifiants, ce qui ne pouvait pas fonctionner.
//
// Le mot de passe eventuel du fichier n'est pas demande : c'est un privilege
// d'administration assume. Les fichiers sont stockes en clair sur le disque,
// un administrateur y a de toute facon acces. L'acces est trace.
func HandleAdminDownload(w http.ResponseWriter, req *http.Request) {
	user, _ := currentAdmin(req)
	if user == nil {
		http.Redirect(w, req, "/admin/login", http.StatusFound)
		return
	}

	id := req.URL.Query().Get("id")
	if id == "" || !postsql.Exists(id) {
		http.Error(w, "Fichier introuvable", http.StatusNotFound)
		return
	}

	name := postsql.GetFileName(id)
	fullPath := filepath.Join(os.Getenv("FILES_PATH"), id, name)
	if _, err := os.Stat(fullPath); err != nil {
		http.Error(w, "Fichier absent du disque", http.StatusNotFound)
		return
	}

	protected := ""
	if postsql.HasPassword(id) {
		protected = " (protege par mot de passe, contourne)"
	}
	fmt.Printf("[ADMIN] %s telecharge %s%s\n", user.Username, id, protected)
	audit(req, user, postsql.LevelInfo, postsql.CatDownload, "telechargement-admin", id, name+protected)

	w.Header().Set("Content-Disposition", "attachment; filename=\""+sanitizeFilename(name)+"\"")
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, req, fullPath)
}

// sanitizeFilename neutralise les caracteres qui casseraient l'en-tete.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\"", "'")
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	return name
}

/* API */

func HandleAdminAPI(w http.ResponseWriter, req *http.Request) {
	var p rqBody
	if err := json.NewDecoder(req.Body).Decode(&p); err != nil {
		writeErr(w, http.StatusBadRequest, "Corps de requete JSON invalide")
		return
	}

	// Actions accessibles sans session.
	switch p.Action {
	case "login":
		adminLogin(w, req, p)
		return
	case "register":
		adminRegister(w, req, p)
		return
	}

	user, token, ok := requireAdmin(w, req)
	if !ok {
		return
	}

	switch p.Action {
	case "me":
		count, size := postsql.PersonalUsage(user.ID)
		writeOK(w, map[string]interface{}{
			"user":           user,
			"passkeys":       postsql.CountCredentials(user.ID),
			"personal_files": count,
			"personal_size":  size,
		})

	case "logout":
		audit(req, user, postsql.LevelInfo, postsql.CatAuth, "deconnexion", user.Username, "")
		postsql.RevokeSession(token)
		clearSessionCookie(w, req)
		writeOK(w, map[string]string{"status": "LOGGED_OUT"})

	case "overview":
		writeOK(w, postsql.GetOverview())

	case "storage":
		writeOK(w, postsql.GetStorage())

	/* Journal */

	case "audit_list":
		entries, total := postsql.ListAudit(postsql.AuditFilter{
			Category: p.Category, Level: p.Level, Search: p.Search,
			Offset: p.Offset, Limit: p.Limit,
		})
		writeOK(w, map[string]interface{}{"entries": entries, "total": total})

	case "audit_stats":
		writeOK(w, postsql.AuditStats())

	/* Fichiers publics */

	case "list_files":
		offset, err1 := strconv.Atoi(req.URL.Query().Get("offset"))
		limit, err2 := strconv.Atoi(req.URL.Query().Get("limit"))
		if err1 != nil || err2 != nil {
			writeErr(w, http.StatusBadRequest, "offset ou limit invalide")
			return
		}
		files, total := postsql.SearchFiles(p.Search, offset, limit)
		writeOK(w, map[string]interface{}{"files": files, "total": total})

	case "delete":
		if !postsql.Exists(p.FileID) {
			writeErr(w, http.StatusNotFound, "Fichier introuvable")
			return
		}
		nom := postsql.GetFileName(p.FileID)
		if !postsql.DeleteFile(p.FileID) {
			writeErr(w, http.StatusInternalServerError, "Suppression impossible")
			return
		}
		audit(req, user, postsql.LevelWarn, postsql.CatFile, "suppression", p.FileID, nom)
		writeOK(w, map[string]string{"status": "FILE_DELETED"})

	case "rename":
		if !postsql.Exists(p.FileID) {
			writeErr(w, http.StatusNotFound, "Fichier introuvable")
			return
		}
		ancien := postsql.GetFileName(p.FileID)
		if !postsql.RenameFile(p.FileID, p.NewName) {
			writeErr(w, http.StatusInternalServerError, "Renommage impossible")
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatFile, "renommage", p.FileID, ancien+" -> "+p.NewName)
		writeOK(w, map[string]string{"status": "FILE_RENAMED"})

	case "change_id":
		if !postsql.Exists(p.FileID) {
			writeErr(w, http.StatusNotFound, "Fichier introuvable")
			return
		}
		if !postsql.ChangeFileID(p.FileID, p.NewFileID) {
			writeErr(w, http.StatusInternalServerError, "Changement d'identifiant impossible")
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatFile, "changement-identifiant", p.FileID, p.FileID+" -> "+p.NewFileID)
		writeOK(w, map[string]string{"status": "FILE_ID_CHANGED"})

	/* Securite du compte */

	case "sessions_list":
		writeOK(w, postsql.ListSessions(user.ID, token))

	case "session_revoke":
		if !postsql.RevokeSessionByPrefix(user.ID, p.SessionID) {
			writeErr(w, http.StatusNotFound, "Session introuvable")
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatAuth, "session-revoquee", p.SessionID, "")
		writeOK(w, map[string]string{"status": "SESSION_REVOKED"})

	case "sessions_revoke_others":
		n := postsql.RevokeAllSessions(user.ID, token)
		audit(req, user, postsql.LevelInfo, postsql.CatAuth, "sessions-revoquees", user.Username,
			fmt.Sprintf("%d session(s)", n))
		writeOK(w, map[string]interface{}{"status": "SESSIONS_REVOKED", "count": n})

	case "change_password":
		if err := postsql.AdminChangePassword(user.ID, p.CurrentPassword, p.NewPassword); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		// Un changement de mot de passe deconnecte les autres appareils.
		postsql.RevokeAllSessions(user.ID, token)
		audit(req, user, postsql.LevelWarn, postsql.CatAuth, "mot-de-passe-change", user.Username, "")
		writeOK(w, map[string]string{"status": "PASSWORD_CHANGED"})

	case "set_auth_policy":
		if err := postsql.AdminSetAuthPolicy(user.ID, p.AuthPolicy); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		audit(req, user, postsql.LevelWarn, postsql.CatAuth, "methode-connexion-changee", user.Username, p.AuthPolicy)
		writeOK(w, map[string]string{"status": "POLICY_UPDATED", "policy": p.AuthPolicy})

	case "passkeys_list":
		writeOK(w, postsql.ListCredentials(user.ID))

	case "passkey_rename":
		if err := postsql.RenameCredential(user.ID, p.CredID, strings.TrimSpace(p.CredName)); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatAuth, "passkey-renommee", p.CredName, "")
		writeOK(w, map[string]string{"status": "PASSKEY_RENAMED"})

	case "passkey_delete":
		if err := postsql.DeleteCredential(user.ID, p.CredID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		audit(req, user, postsql.LevelWarn, postsql.CatAuth, "passkey-revoquee", fmt.Sprintf("#%d", p.CredID), "")
		writeOK(w, map[string]string{"status": "PASSKEY_DELETED"})

	/* Administrateurs et invitations */

	case "admins_list":
		writeOK(w, postsql.ListAdmins())

	case "admin_delete":
		if err := postsql.DeleteAdmin(p.AdminID, user.ID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		audit(req, user, postsql.LevelWarn, postsql.CatAdmin, "compte-supprime", fmt.Sprintf("#%d", p.AdminID), "")
		writeOK(w, map[string]string{"status": "ADMIN_DELETED"})

	case "invites_list":
		writeOK(w, postsql.ListInvitations())

	case "invite_create":
		token, err := postsql.CreateInvitation(user.Username)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Creation impossible : "+err.Error())
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatAdmin, "invitation-creee", token, "")
		writeOK(w, map[string]string{"status": "INVITE_CREATED", "token": token})

	case "invite_delete":
		if err := postsql.DeleteInvitation(p.Token); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatAdmin, "invitation-supprimee", p.Token, "")
		writeOK(w, map[string]string{"status": "INVITE_DELETED"})

	/* Cloud personnel */

	case "personal_list":
		writeOK(w, map[string]interface{}{
			"folder":  postsql.NormalizeFolder(p.Folder),
			"folders": postsql.PersonalSubfolders(user.ID, p.Folder),
			"files":   postsql.PersonalList(user.ID, p.Folder),
		})

	case "personal_delete":
		if err := postsql.PersonalDelete(user.ID, p.FileID); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatPersonal, "suppression", p.FileID, "")
		writeOK(w, map[string]string{"status": "PERSONAL_DELETED"})

	case "personal_rename":
		if err := postsql.PersonalRename(user.ID, p.FileID, p.NewName); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatPersonal, "renommage", p.FileID, p.NewName)
		writeOK(w, map[string]string{"status": "PERSONAL_RENAMED"})

	case "personal_move":
		if err := postsql.PersonalMove(user.ID, p.FileID, p.Folder); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatPersonal, "deplacement", p.FileID, p.Folder)
		writeOK(w, map[string]string{"status": "PERSONAL_MOVED"})

	default:
		writeErr(w, http.StatusBadRequest, "Action inconnue")
	}
}

/* Connexion et inscription */

func adminLogin(w http.ResponseWriter, req *http.Request, p rqBody) {
	identifier := strings.TrimSpace(p.Username)
	if identifier == "" {
		identifier = strings.TrimSpace(p.Email)
	}
	if identifier == "" || p.Password == "" {
		writeErr(w, http.StatusBadRequest, "Identifiant et mot de passe requis")
		return
	}

	user, ok := postsql.AdminGetUser(identifier)
	if !ok || !postsql.AdminVerifyPassword(user.ID, p.Password) {
		auditAnon(req, postsql.LevelWarn, postsql.CatAuth, "connexion-refusee", identifier,
			"identifiant ou mot de passe incorrect")
		writeErr(w, http.StatusUnauthorized, "Identifiant ou mot de passe incorrect")
		return
	}

	// La politique du compte peut exiger une passkey en plus du mot de passe.
	if user.AuthPolicy == postsql.PolicyPasswordAndPasskey {
		if postsql.CountCredentials(user.ID) == 0 {
			writeErr(w, http.StatusInternalServerError, "Compte configure pour exiger une passkey, mais aucune n'est enregistree")
			return
		}
		pending, err := StartPendingPasskey(user.ID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Ouverture de session impossible")
			return
		}
		audit(req, user, postsql.LevelInfo, postsql.CatAuth, "mot-de-passe-valide", user.Username,
			"passkey attendue (second facteur)")
		writeOK(w, map[string]interface{}{"status": "PASSKEY_REQUIRED", "pending": pending})
		return
	}

	token, err := postsql.CreateSession(user.ID, clientIP(req), req.UserAgent())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Ouverture de session impossible")
		return
	}
	setSessionCookie(w, req, token)
	audit(req, user, postsql.LevelInfo, postsql.CatAuth, "connexion", user.Username, "mot de passe")
	writeOK(w, map[string]interface{}{"status": "LOGGED_IN", "user": user})
}

func adminRegister(w http.ResponseWriter, req *http.Request, p rqBody) {
	if p.Email == "" || p.Password == "" || p.Username == "" ||
		p.FirstName == "" || p.LastName == "" || p.InviteCode == "" {
		writeErr(w, http.StatusBadRequest, "Tous les champs sont requis")
		return
	}
	if len(p.Password) < 8 {
		writeErr(w, http.StatusBadRequest, "Le mot de passe doit faire au moins 8 caracteres")
		return
	}
	if postsql.AdminCheckUserExistence(p.Username) {
		writeErr(w, http.StatusConflict, "Ce nom d'utilisateur ou cet e-mail existe deja")
		return
	}

	if _, success := postsql.AdminRegisterUser(
		p.FirstName, p.LastName, p.Username, p.Email, p.Password, p.InviteCode); !success {
		writeErr(w, http.StatusBadRequest, "Creation impossible : code d'invitation invalide ou deja utilise")
		return
	}

	user, ok := postsql.AdminGetUser(p.Username)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "Compte cree mais introuvable")
		return
	}

	// Le compte vient d'etre cree : on ouvre la session directement.
	token, err := postsql.CreateSession(user.ID, clientIP(req), req.UserAgent())
	if err != nil {
		writeOK(w, map[string]string{"status": "USER_CREATED"})
		return
	}
	setSessionCookie(w, req, token)
	fmt.Printf("[ADMIN] compte cree : %s\n", user.Username)
	audit(req, user, postsql.LevelWarn, postsql.CatAdmin, "compte-cree", user.Username,
		"via invitation "+p.InviteCode)
	writeOK(w, map[string]interface{}{"status": "USER_CREATED", "user": user})
}
