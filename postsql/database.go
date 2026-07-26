package postsql

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"filetransfer-backend/fmgr"
	"fmt"
	rnd "math/rand"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx"
	"golang.org/x/crypto/bcrypt"
)

type Database struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

type FileInfo struct {
	ID        string    `json:"id"`
	FileName  string    `json:"file_name"`
	FileSize  int64     `json:"file_size"`
	IPAddr    string    `json:"ip_addr"`
	Date      time.Time `json:"date"`
	HasPasswd bool      `json:"has_passwd"`
}

type APIResponse struct {
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
	Data  []FileInfo `json:"data"`
}

var (
	connPool *pgx.ConnPool
	char     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

func GenerateSalt(n int) ([]byte, error) {
	salt := make([]byte, n)
	_, err := rand.Read(salt)
	if err != nil {
		return nil, err
	}
	return salt, nil
}

func ReconnectDB() {
	if connPool != nil {
		return
	}
	port, _ := strconv.Atoi(os.Getenv("POSTGRESQL_PORT"))
	credentials := Database{
		Host:     os.Getenv("POSTGRESQL_HOST"),
		Port:     port,
		Database: os.Getenv("POSTGRESQL_DATABASE"),
		User:     os.Getenv("POSTGRESQL_USER"),
		Password: os.Getenv("POSTGRESQL_PASSWORD"),
	}
	ConnectPool(&credentials)
}

func ConnectPool(db *Database) {
	if connPool != nil {
		return
	}

	connCfg := pgx.ConnConfig{
		Host:     db.Host,
		Port:     uint16(db.Port),
		Database: db.Database,
		User:     db.User,
		Password: db.Password,
	}

	poolCfg := pgx.ConnPoolConfig{
		ConnConfig:     connCfg,
		MaxConnections: 25,
		AcquireTimeout: 30 * time.Second,
	}

	var err error
	connPool, err = pgx.NewConnPool(poolCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erreur de connexion à PostgreSQL : %v\n", err)
		os.Exit(1)
	}

	conn, err := connPool.Acquire()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erreur lors de l'acquisition d'une connexion : %v\n", err)
		os.Exit(1)
	}
	defer connPool.Release(conn)

	_, err = conn.Exec(`CREATE TABLE IF NOT EXISTS file_transfer (
		id VARCHAR(6) PRIMARY KEY, 
		file_name VARCHAR(255) NOT NULL, 
		file_size BIGINT NOT NULL, 
		ip_addr VARCHAR(45) NOT NULL, 
		date TIMESTAMP DEFAULT CURRENT_TIMESTAMP, 
		has_passwd BOOLEAN DEFAULT FALSE, 
		xpasswd CHAR(64), 
		salt_passwd BYTEA
	);`)
	manageErr(err)

	_, err = conn.Exec(`CREATE TABLE IF NOT EXISTS users (
		id SERIAL PRIMARY KEY, 
		email_address VARCHAR(255) NOT NULL, 
		last_name VARCHAR(255) NOT NULL, 
		first_name VARCHAR(255) NOT NULL, 
		username VARCHAR(255) NOT NULL, 
		password VARCHAR(255) NOT NULL,
		token VARCHAR(255) NOT NULL,
		confirmed BOOLEAN DEFAULT FALSE,
		invitation_used VARCHAR(255) NOT NULL,
		confirmation_code VARCHAR(255) NOT NULL
	);`)
	manageErr(err)

	_, err = conn.Exec(`CREATE TABLE IF NOT EXISTS admin_invitations (
		token VARCHAR(255) NOT NULL,
		used BOOLEAN DEFAULT TRUE
	);`)
	manageErr(err)
	fmt.Println("Connecté avec succès à la base de données")
}

func manageErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Query failed: %v\n", err)
		os.Exit(1)
	}
}

/* Gestion des fichiers */

func genFileID() string {
	b := make([]byte, 6)
	for i := range b {
		b[i] = char[rnd.Intn(len(char))]
	}
	return string(b)
}

func GenFileID() string {
	ReconnectDB()
	for {
		id := genFileID()
		var exists bool

		err := connPool.QueryRow("SELECT EXISTS(SELECT 1 FROM file_transfer WHERE id=$1);", id).Scan(&exists)
		if err != nil {
			manageErr(err)
		}

		if !exists {
			return id
		}
	}
}

func PushFile(id string, fn string, fsize int64, ip_addr string, password string) {
	ReconnectDB()
	var salt []byte
	has_password := false
	hashed_password := ""

	if password != "" {
		hash := sha256.New()
		salt, _ = GenerateSalt(16)
		hash.Write(salt)
		hash.Write([]byte(password))
		hashed_password = hex.EncodeToString(hash.Sum(nil))
		has_password = true
	}

	_, err := connPool.Exec(`
		INSERT INTO file_transfer (id, file_name, file_size, ip_addr, has_passwd, xpasswd, salt_passwd) 
		VALUES ($1, $2, $3, $4, $5, $6, $7);`,
		id, fn, fsize, ip_addr, has_password, hashed_password, salt,
	)
	manageErr(err)
}

func Exists(id string) bool {
	ReconnectDB()
	var exists int

	err := connPool.QueryRow("SELECT 1 FROM file_transfer WHERE id=$1;", id).Scan(&exists)
	if err != nil {
		return false
	}
	return true
}

func HasPassword(id string) bool {
	ReconnectDB()
	if Exists(id) == false {
		return false
	}

	has_passwd := false

	err := connPool.QueryRow("SELECT has_passwd FROM file_transfer WHERE id=$1;", id).Scan(&has_passwd)
	manageErr(err)
	return has_passwd
}

func IsPassword(id string, password string) bool {
	ReconnectDB()
	if Exists(id) == false {
		return false
	}
	if HasPassword(id) == false {
		return true
	}

	salt := []byte{}
	real_hashed_password := ""

	err := connPool.QueryRow("SELECT salt_passwd FROM file_transfer WHERE id=$1;", id).Scan(&salt)
	manageErr(err)
	err = connPool.QueryRow("SELECT xpasswd FROM file_transfer WHERE id=$1;", id).Scan(&real_hashed_password)
	manageErr(err)

	hash := sha256.New()
	hash.Write(salt)
	hash.Write([]byte(password))
	hashed_password := hex.EncodeToString(hash.Sum(nil))
	return hashed_password == real_hashed_password
}

func GetFileName(id string) string {
	ReconnectDB()
	if Exists(id) == false {
		return id
	}
	name := "Undefined"
	err := connPool.QueryRow("SELECT file_name FROM file_transfer WHERE id=$1;", id).Scan(&name)
	manageErr(err)
	return name

}

func GetFileSize(id string) int64 {
	ReconnectDB()
	if Exists(id) == false {
		return int64(-1)
	}
	size := int64(-1)
	err := connPool.QueryRow("SELECT file_size FROM file_transfer WHERE id=$1;", id).Scan(&size)
	manageErr(err)
	return size

}

func Close() {
	if connPool != nil {
		connPool.Close()
	}
}

/* Administration */

func AdminCheckUserExistence(username string) bool {
	ReconnectDB()
	var exists bool

	err := connPool.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE username=$1);", username).Scan(&exists)
	if err != nil {
		manageErr(err)
	}

	if !exists {
		err = connPool.QueryRow("SELECT EXISTS(SELECT 1 FROM users WHERE email_address=$1);", username).Scan(&exists)
		if err != nil {
			manageErr(err)
		}
	}

	return exists
}

func AdminRegisterUser(firstname string, lastname string, username string, email string, password string, invitation string) (string, bool) {
	if password == "" || invitation == "" || firstname == "" || lastname == "" || username == "" {
		return "", false
	}
	ReconnectDB()
	if AdminCheckUserExistence(username) == true {
		return "", false
	}

	if AdminCheckInvitation(invitation) == false {
		return "", false
	}

	// le truc en sha256 tout caca
	/*salt, err := GenerateSalt(16)
	manageErr(err)

	hash := sha256.New()
	hash.Write(salt)
	hash.Write([]byte(password))
	hashed_password := hex.EncodeToString(hash.Sum(nil))*/

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	manageErr(err)

	confirmation_code, err := GenerateSalt(32)
	manageErr(err)

	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		manageErr(err)
		return "", false
	}

	if !AdminUseInvitation(invitation) {
		fmt.Println("An error occurred while trying to use the invitation")
		return "", false
	}

	_, err = connPool.Exec(`INSERT INTO users (first_name, last_name, username, email_address, invitation_used, password, confirmation_code, token) VALUES($1, $2, $3, $4, $5, $6, $7, $8);`, firstname, lastname, username, email, invitation, string(hashedPassword), hex.EncodeToString(confirmation_code), hex.EncodeToString(token))
	if err != nil {
		fmt.Println("An error occurred while attemping to create the account")
		manageErr(err)
		return "", false
	}

	return hex.EncodeToString(token), true

}

func CheckUserToken(username string, token string) bool {
	ReconnectDB()
	if AdminCheckUserExistence(username) == false {
		return false
	}

	real_token := ""
	err := connPool.QueryRow("SELECT token FROM users WHERE username=$1;", username).Scan(&real_token)
	if err != nil {
		manageErr(err)
		return false
	}

	if real_token == "" {
		return false
	}

	return real_token == token
}

func AdminConfirmUser(username string, user_token string, confirm_code string) bool {
	ReconnectDB()

	if AdminCheckUserExistence(username) == false || CheckUserToken(username, user_token) == false {
		return false
	}

	true_confirm_code := ""
	err := connPool.QueryRow(`SELECT confirmation_code FROM users WHERE token=$1;`, user_token).Scan(&true_confirm_code)
	if err != nil {
		manageErr(err)
		return false
	}

	can_confirm := true_confirm_code == confirm_code

	if can_confirm {
		err = connPool.QueryRow(`UPDATE users SET confirmed=TRUE WHERE token=$1;`, user_token).Scan()
		if err != nil {
			manageErr(err)
			return false
		}
	}

	return can_confirm
}

func AdminCheckInvitation(invitation string) bool {
	ReconnectDB()
	var token string
	var used bool

	err := connPool.QueryRow("SELECT token, used FROM admin_invitations WHERE token=$1;", invitation).Scan(&token, &used)
	if err != nil {
		manageErr(err)
		return false
	}

	if token == "" || used == true {
		return false
	}
	return true

}

func AdminUseInvitation(invitation string) bool {
	ReconnectDB()
	if AdminCheckInvitation(invitation) == false {
		return false
	}
	_, err := connPool.Exec("UPDATE admin_invitations SET used=true WHERE token=$1;", invitation)
	if err != nil {
		manageErr(err)
		return false
	}
	return true

}

func AdminCheckCredentials(username string, password string) bool {
	ReconnectDB()
	if AdminCheckUserExistence(username) == false {
		return false
	}

	var storedPassword string
	err := connPool.QueryRow("SELECT password FROM users WHERE username=$1;", username).Scan(&storedPassword)
	if err != nil {
		manageErr(err)
		return false
	}

	err = bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(password))
	return err == nil

}

func AdminGetUserToken(username string) string {
	ReconnectDB()
	token := ""
	err := connPool.QueryRow("SELECT token FROM users WHERE username=$1;", username).Scan(&token)
	if err != nil {
		err = connPool.QueryRow("SELECT token FROM users WHERE email_address=$1;", username).Scan(&token)
		if err != nil {
			manageErr(err)
			return ""
		}
	}
	return token
}

func ChangeFileID(id string, new_id string) bool {
	ReconnectDB()
	if Exists(id) == false {
		return false
	}

	err := connPool.QueryRow("UPDATE file_transfer SET id=$1 WHERE id=$2;", new_id, id).Scan()
	if err != nil {
		manageErr(err)
		return false
	}

	if fmgr.ChangeFileID(id, new_id) != nil {
		return false
	}

	return true
}

func ListFiles(index, max int) []FileInfo {
	ReconnectDB()
	files := []FileInfo{}

	rows, err := connPool.Query("SELECT id, file_name, file_size, ip_addr, date, has_passwd FROM file_transfer ORDER BY date DESC LIMIT $1 OFFSET $2;", max, index)
	if err != nil {
		manageErr(err)
		return files
	}
	defer rows.Close()

	for rows.Next() {
		var file FileInfo
		err := rows.Scan(&file.ID, &file.FileName, &file.FileSize, &file.IPAddr, &file.Date, &file.HasPasswd)
		if err != nil {
			manageErr(err)
			return files
		}
		files = append(files, file)
	}

	if err := rows.Err(); err != nil {
		manageErr(err)
		return files
	}

	return files
}

func DeleteFile(id string) bool {
	ReconnectDB()
	if Exists(id) == false {
		return false
	}

	err := connPool.QueryRow("DELETE FROM file_transfer WHERE id=$1;", id).Scan()
	if err != nil {
		manageErr(err)
		return false
	}

	if fmgr.DeleteFile("./files/"+id) != nil {
		return false
	}

	return true
}

func RenameFile(id, new_name string) bool {
	ReconnectDB()
	if Exists(id) == false {
		return false
	}

	_, err := connPool.Exec("UPDATE file_transfer SET file_name=$1 WHERE id=$2;", new_name, id)
	if err != nil {
		manageErr(err)
		return false
	}

	old_path := "./files/" + id + "/" + GetFileName(id)
	new_path := "./files/" + id + "/" + new_name

	if fmgr.RenameFile(old_path, new_path) != nil {
		return false
	}

	return true
}
