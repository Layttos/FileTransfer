package routes

import (
	"filetransfer-backend/postsql"
	"net/http"
)

// audit enregistre un evenement en reprenant automatiquement l'IP reelle du
// visiteur, son navigateur, et l'administrateur connecte s'il y en a un.
func audit(req *http.Request, user *postsql.AdminUser, level, category, action, target, detail string) {
	e := postsql.AuditEntry{
		Level:     level,
		Category:  category,
		Action:    action,
		IPAddr:    clientIP(req),
		Target:    target,
		Detail:    detail,
		UserAgent: req.UserAgent(),
	}
	if user != nil {
		e.Actor = user.Username
		id := user.ID
		e.ActorID = &id
	}
	postsql.Audit(e)
}

// auditAnon journalise une action de visiteur non authentifie.
func auditAnon(req *http.Request, level, category, action, target, detail string) {
	audit(req, nil, level, category, action, target, detail)
}
