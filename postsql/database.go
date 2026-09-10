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
	"strings"
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

	// Au premier demarrage, PostgreSQL peut encore etre en cours d'initialisation.
	// On retente pendant ~60s au lieu de faire crasher le conteneur immediatement.
	var err error
	for attempt := 1; attempt <= 30; attempt++ {
		connPool, err = pgx.NewConnPool(poolCfg)
		if err == nil {
			break
		}
		fmt.Printf("PostgreSQL pas encore disponible (tentative %d/30) : %v\n", attempt, err)
		time.Sleep(2 * time.Second)
	}
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

	migrate(conn)

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
		fmt.Println("return false")
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
		// Un code inexistant n'est pas une erreur serveur : on refuse l'invitation
		// sans arreter le processus (manageErr appelle os.Exit).
		if err != pgx.ErrNoRows {
			fmt.Fprintf(os.Stderr, "Verification de l'invitation impossible : %v\n", err)
		}
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

// validFileID n'accepte qu'un identifiant de la meme forme que ceux generes.
// Sans cette verification, l'identifiant fourni se retrouvait tel quel dans un
// chemin de fichier : "../x" faisait sortir le repertoire du dossier de stockage.
func validFileID(id string) bool {
	if len(id) == 0 || len(id) > 6 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func ChangeFileID(id string, new_id string) bool {
	ReconnectDB()
	if Exists(id) == false {
		return false
	}
	if !validFileID(new_id) {
		fmt.Fprintf(os.Stderr, "Identifiant refuse : %q\n", new_id)
		return false
	}

	_, err := connPool.Exec("UPDATE file_transfer SET id=$1 WHERE id=$2;", new_id, id)
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

// likeEscaper neutralise les jokers d'un motif ILIKE.
var likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

// globToLike traduit un motif de style explorateur de fichiers (*.zip,
// photo?.jpg) en motif ILIKE : * devient %, ? devient _. Les jokers SQL deja
// presents dans le terme sont echappes pour rester litteraux.
// Renvoie false si le terme ne contient aucun joker : seule la recherche
// litterale s'applique alors.
func globToLike(term string) (string, bool) {
	if !strings.ContainsAny(term, "*?") {
		return "", false
	}
	var sb strings.Builder
	for _, r := range term {
		switch r {
		case '*':
			sb.WriteByte('%')
		case '?':
			sb.WriteByte('_')
		case '%', '_', '\\':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String(), true
}

// SearchFiles renvoie les transferts correspondant a une recherche, les plus
// recents d'abord, avec le nombre total de resultats pour la pagination.
// Une recherche vide equivaut a lister tous les fichiers.
func SearchFiles(search string, offset, limit int) ([]FileInfo, int64) {
	ReconnectDB()
	files := []FileInfo{}

	where := "1 = 1"
	args := []interface{}{}

	// La recherche porte sur le nom, l'identifiant court et l'IP de depot :
	// ce sont les trois pistes dont on dispose pour retrouver un transfert.
	if term := strings.TrimSpace(search); term != "" {
		// Deux lectures du terme, reunies par un OR.
		//
		// 1. Litterale : % et _ sont des jokers pour ILIKE, on les echappe, sinon
		//    "mon_fichier" remonterait aussi "monXfichier". C'est aussi ce qui
		//    permet de retrouver un fichier dont le nom contient vraiment "*.zip".
		// 2. Glob : *.zip ou photo?.jpg sont traduits en motifs LIKE, pour
		//    chercher par extension comme dans un explorateur de fichiers.
		conds := []string{
			`file_name ILIKE $1 ESCAPE '\'`,
			`id ILIKE $1 ESCAPE '\'`,
			`ip_addr ILIKE $1 ESCAPE '\'`,
		}
		args = append(args, "%"+likeEscaper.Replace(term)+"%")

		if pattern, ok := globToLike(term); ok {
			args = append(args, pattern)
			conds = append(conds,
				`file_name ILIKE $2 ESCAPE '\'`,
				`id ILIKE $2 ESCAPE '\'`,
				`ip_addr ILIKE $2 ESCAPE '\'`)
		}
		where = "(" + strings.Join(conds, " OR ") + ")"
	}

	var total int64
	if err := connPool.QueryRow(
		"SELECT COUNT(*) FROM file_transfer WHERE "+where, args...).Scan(&total); err != nil {
		manageErr(err)
		return files, 0
	}

	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)

	query := fmt.Sprintf(
		"SELECT id, file_name, file_size, ip_addr, date, has_passwd FROM file_transfer "+
			"WHERE %s ORDER BY date DESC LIMIT $%d OFFSET $%d",
		where, len(args)-1, len(args))

	rows, err := connPool.Query(query, args...)
	if err != nil {
		manageErr(err)
		return files, total
	}
	defer rows.Close()

	for rows.Next() {
		var file FileInfo
		if err := rows.Scan(&file.ID, &file.FileName, &file.FileSize,
			&file.IPAddr, &file.Date, &file.HasPasswd); err != nil {
			manageErr(err)
			return files, total
		}
		files = append(files, file)
	}
	return files, total
}

func DeleteFile(id string) bool {
	ReconnectDB()
	if Exists(id) == false {
		return false
	}

	if fmgr.DeleteFile(os.Getenv("FILES_PATH")+"/"+id) != nil {
		return false
	}

	_, err := connPool.Exec("DELETE FROM file_transfer WHERE id=$1;", id)
	if err != nil {
		manageErr(err)
		return false
	}

	return true
}

func RenameFile(id, new_name string) bool {
	ReconnectDB()
	if Exists(id) == false {
		return false
	}

	fmt.Println("[RENAME] (Previous) Full path:", os.Getenv("FILES_PATH")+"/"+id+"/"+GetFileName(id))
	fmt.Println("[RENAME] (New) Full path:", os.Getenv("FILES_PATH")+"/"+id+"/"+new_name)
	old_path := os.Getenv("FILES_PATH") + "/" + id + "/" + GetFileName(id)
	new_path := os.Getenv("FILES_PATH") + "/" + id + "/" + new_name

	if fmgr.RenameFile(old_path, new_path) != nil {
		return false
	}

	_, err := connPool.Exec("UPDATE file_transfer SET file_name=$1 WHERE id=$2;", new_name, id)
	if err != nil {
		manageErr(err)
		return false
	}

	return true
}
