package main

import (
	routes "filetransfer-backend/Routes"
	"filetransfer-backend/postsql"
	"fmt"
	"net/http"
	"os"

	"github.com/jackc/pgx"
	"github.com/joho/godotenv"
)

var (
	conn *pgx.Conn
)

func main() {
	// Le .env est optionnel : sous Docker la configuration arrive par l'environnement.
	// godotenv n'ecrase jamais une variable deja definie, les deux modes cohabitent.
	if err := godotenv.Load("./.env"); err != nil {
		fmt.Println("Pas de fichier .env, utilisation des variables d'environnement")
	}

	fmt.Println("——— Starting File Transfer Backend server ———")

	port := os.Getenv("PORT")
	if port == "" {
		port = "3333"
	}

	fmt.Println("Attemping to connect to the PostgreSQL database")
	postsql.ReconnectDB()
	defer postsql.Close()

	postsql.StartAuditWriter()
	postsql.BootstrapAdminInvitation()
	postsql.PurgeExpiredSessions()
	postsql.PurgeAudit()
	postsql.Audit(postsql.AuditEntry{
		Category: postsql.CatSystem, Action: "demarrage", Actor: "serveur",
		Detail: "port " + port,
	})

	http.HandleFunc("/upload", routes.HandleUpload)
	http.HandleFunc("/index", routes.HandleDefault)
	http.HandleFunc("/admin", routes.HandleAdmin)
	http.HandleFunc(`/{$}`, routes.HandleDefault)
	http.HandleFunc(`/admin/login`, routes.HandleAdminLogin)
	http.HandleFunc(`/admin/register`, routes.HandleAdminRegister)
	http.HandleFunc(`/admin/dashboard`, routes.HandleAdminDashboard)
	http.HandleFunc(`/admin/api`, routes.HandleAdminAPI)
	http.HandleFunc(`/admin/download`, routes.HandleAdminDownload)
	http.HandleFunc(`/admin/personal/upload`, routes.HandlePersonalUpload)
	http.HandleFunc(`/admin/personal/download`, routes.HandlePersonalDownload)
	http.HandleFunc(`/admin/personal/zip`, routes.HandlePersonalZip)
	http.HandleFunc(`/admin/webauthn/register/begin`, routes.HandleWebAuthnRegisterBegin)
	http.HandleFunc(`/admin/webauthn/register/finish`, routes.HandleWebAuthnRegisterFinish)
	http.HandleFunc(`/admin/webauthn/login/begin`, routes.HandleWebAuthnLoginBegin)
	http.HandleFunc(`/admin/webauthn/login/finish`, routes.HandleWebAuthnLoginFinish)
	http.HandleFunc(`/{id}`, routes.HandleFile)

	fmt.Println("Now listening on the port " + port)
	http.ListenAndServe(":"+port, nil)
}
