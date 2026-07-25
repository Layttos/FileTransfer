package main

import (
	routes "filetransfer-backend/Routes"
	"filetransfer-backend/postsql"
	"fmt"
	"net/http"

	"github.com/jackc/pgx"
	"github.com/joho/godotenv"
)

var (
	conn *pgx.Conn
)

func main() {
	err := godotenv.Load("./.env")
	if err != nil {
		fmt.Println("Error loading .env file")
	}
	fmt.Println("——— Starting File Transfer Backend server ———")
	port := "3333"

	fmt.Println("Attemping to connect to the PostgreSQL database")
	postsql.ReconnectDB()
	defer postsql.Close()

	http.HandleFunc("/upload", routes.HandleUpload)
	http.HandleFunc("/index", routes.HandleDefault)
	http.HandleFunc("/admin", routes.HandleAdmin)
	http.HandleFunc(`/{$}`, routes.HandleDefault)
	http.HandleFunc(`/admin/login`, routes.HandleAdminLogin)
	http.HandleFunc(`/admin/register`, routes.HandleAdminRegister)
	http.HandleFunc(`/admin/api`, routes.HandleAdminAPI)
	http.HandleFunc(`/{id}`, routes.HandleFile)

	fmt.Println("Now listening on the port " + port)
	http.ListenAndServe(":"+port, nil)
}
