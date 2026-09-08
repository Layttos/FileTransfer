package postsql

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx"
	"golang.org/x/crypto/bcrypt"
)

// Politiques d'authentification acceptees.
const (
	PolicyPassword           = "password"             // mot de passe seul
	PolicyPasskeyOrPassword  = "passkey_or_password"  // passkey acceptee, mot de passe en secours
	PolicyPasswordAndPasskey = "password_and_passkey" // les deux exiges
)

func ValidAuthPolicy(p string) bool {
	switch p {
	case PolicyPassword, PolicyPasskeyOrPassword, PolicyPasswordAndPasskey:
		return true
	}
	return false
}

const userColumns = `u.id, u.username, u.email_address, u.first_name, u.last_name,
	COALESCE(u.auth_policy, 'password')`

func scanUser(row *pgx.Row) (*AdminUser, bool) {
	var u AdminUser
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.AuthPolicy)
	if err == pgx.ErrNoRows {
		return nil, false
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Lecture d'un compte impossible : %v\n", err)
		return nil, false
	}
	return &u, true
}

// AdminGetUser resout un compte par nom d'utilisateur ou par adresse e-mail.
func AdminGetUser(identifier string) (*AdminUser, bool) {
	ReconnectDB()
	row := connPool.QueryRow(
		"SELECT "+userColumns+" FROM users u WHERE u.username = $1 OR u.email_address = $1 LIMIT 1;",
		identifier)
	return scanUser((*pgx.Row)(row))
}

// AdminGetUserByID resout un compte par identifiant numerique.
func AdminGetUserByID(id int) (*AdminUser, bool) {
	ReconnectDB()
	row := connPool.QueryRow("SELECT "+userColumns+" FROM users u WHERE u.id = $1;", id)
	return scanUser((*pgx.Row)(row))
}

// AdminVerifyPassword compare un mot de passe au hash bcrypt stocke.
func AdminVerifyPassword(userID int, password string) bool {
	ReconnectDB()
	var stored string
	err := connPool.QueryRow("SELECT password FROM users WHERE id = $1;", userID).Scan(&stored)
	if err != nil {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)) == nil
}

// AdminChangePassword remplace le mot de passe apres verification de l'actuel.
func AdminChangePassword(userID int, current, next string) error {
	ReconnectDB()
	if !AdminVerifyPassword(userID, current) {
		return fmt.Errorf("mot de passe actuel incorrect")
	}
	if len(next) < 8 {
		return fmt.Errorf("le nouveau mot de passe doit faire au moins 8 caracteres")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := connPool.Exec("UPDATE users SET password = $1 WHERE id = $2;", string(hashed), userID); err != nil {
		return err
	}
	return nil
}

// AdminSetAuthPolicy change la politique de connexion d'un compte. Exiger une
// passkey sans en avoir enregistre une enfermerait l'administrateur dehors.
func AdminSetAuthPolicy(userID int, policy string) error {
	ReconnectDB()
	if !ValidAuthPolicy(policy) {
		return fmt.Errorf("politique inconnue")
	}
	if policy != PolicyPassword && CountCredentials(userID) == 0 {
		return fmt.Errorf("enregistrez d'abord une passkey")
	}
	_, err := connPool.Exec("UPDATE users SET auth_policy = $1 WHERE id = $2;", policy, userID)
	return err
}

/* Gestion des administrateurs */

type AdminSummary struct {
	AdminUser
	Confirmed   bool      `json:"confirmed"`
	Passkeys    int       `json:"passkeys"`
	Sessions    int       `json:"sessions"`
	InvitedWith string    `json:"invited_with"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListAdmins renvoie tous les comptes administrateur avec leur etat.
func ListAdmins() []AdminSummary {
	ReconnectDB()
	out := []AdminSummary{}

	rows, err := connPool.Query(`
		SELECT u.id, u.username, u.email_address, u.first_name, u.last_name,
		       COALESCE(u.auth_policy, 'password'), u.confirmed, u.invitation_used,
		       (SELECT COUNT(*) FROM admin_credentials c WHERE c.user_id = u.id),
		       (SELECT COUNT(*) FROM admin_sessions s WHERE s.user_id = u.id AND s.expires_at > CURRENT_TIMESTAMP)
		FROM users u ORDER BY u.id;`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Listing des administrateurs impossible : %v\n", err)
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var a AdminSummary
		if err := rows.Scan(&a.ID, &a.Username, &a.Email, &a.FirstName, &a.LastName,
			&a.AuthPolicy, &a.Confirmed, &a.InvitedWith, &a.Passkeys, &a.Sessions); err != nil {
			fmt.Fprintf(os.Stderr, "Lecture d'un administrateur impossible : %v\n", err)
			return out
		}
		out = append(out, a)
	}
	return out
}

// DeleteAdmin supprime un compte et tout ce qui s'y rattache. Le dernier
// administrateur ne peut pas etre supprime : plus personne ne pourrait entrer.
func DeleteAdmin(targetID, actorID int) error {
	ReconnectDB()
	if targetID == actorID {
		return fmt.Errorf("impossible de supprimer son propre compte")
	}

	var total int
	if err := connPool.QueryRow("SELECT COUNT(*) FROM users;").Scan(&total); err != nil {
		return err
	}
	if total <= 1 {
		return fmt.Errorf("c'est le dernier administrateur")
	}

	if _, err := connPool.Exec("DELETE FROM admin_sessions WHERE user_id = $1;", targetID); err != nil {
		return err
	}
	if _, err := connPool.Exec("DELETE FROM admin_credentials WHERE user_id = $1;", targetID); err != nil {
		return err
	}
	tag, err := connPool.Exec("DELETE FROM users WHERE id = $1;", targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("compte introuvable")
	}
	return nil
}

/* Invitations */

type Invitation struct {
	Token     string    `json:"token"`
	Used      bool      `json:"used"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
	UsedBy    string    `json:"used_by"`
}

// CreateInvitation genere un code d'invitation, dans le meme format lisible que
// celui affiche au premier demarrage.
func CreateInvitation(createdBy string) (string, error) {
	ReconnectDB()
	token, err := generateInviteCode()
	if err != nil {
		return "", err
	}
	_, err = connPool.Exec(
		"INSERT INTO admin_invitations (token, used, created_by) VALUES ($1, FALSE, $2);",
		token, createdBy)
	if err != nil {
		return "", err
	}
	return token, nil
}

func ListInvitations() []Invitation {
	ReconnectDB()
	out := []Invitation{}
	rows, err := connPool.Query(`
		SELECT token, used, COALESCE(created_at, CURRENT_TIMESTAMP),
		       COALESCE(created_by, ''), COALESCE(used_by, '')
		FROM admin_invitations ORDER BY created_at DESC;`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Listing des invitations impossible : %v\n", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var i Invitation
		if err := rows.Scan(&i.Token, &i.Used, &i.CreatedAt, &i.CreatedBy, &i.UsedBy); err != nil {
			return out
		}
		out = append(out, i)
	}
	return out
}

// DeleteInvitation retire un code non utilise.
func DeleteInvitation(token string) error {
	ReconnectDB()
	tag, err := connPool.Exec("DELETE FROM admin_invitations WHERE token = $1 AND used = FALSE;", token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("invitation introuvable ou deja utilisee")
	}
	return nil
}

/* Vue d'ensemble */

type Overview struct {
	Files          int64 `json:"files"`
	TotalSize      int64 `json:"total_size"`
	ProtectedFiles int64 `json:"protected_files"`
	Admins         int64 `json:"admins"`
	ActiveSessions int64 `json:"active_sessions"`
	Passkeys       int64 `json:"passkeys"`
	PersonalFiles  int64 `json:"personal_files"`
	PersonalSize   int64 `json:"personal_size"`
	OpenInvites    int64 `json:"open_invites"`
}

func GetOverview() Overview {
	ReconnectDB()
	var o Overview
	q := func(dest *int64, query string, args ...interface{}) {
		if err := connPool.QueryRow(query, args...).Scan(dest); err != nil {
			fmt.Fprintf(os.Stderr, "Statistique indisponible : %v\n", err)
		}
	}
	q(&o.Files, "SELECT COUNT(*) FROM file_transfer;")
	q(&o.TotalSize, "SELECT COALESCE(SUM(file_size), 0) FROM file_transfer;")
	q(&o.ProtectedFiles, "SELECT COUNT(*) FROM file_transfer WHERE has_passwd = TRUE;")
	q(&o.Admins, "SELECT COUNT(*) FROM users;")
	q(&o.ActiveSessions, "SELECT COUNT(*) FROM admin_sessions WHERE expires_at > CURRENT_TIMESTAMP;")
	q(&o.Passkeys, "SELECT COUNT(*) FROM admin_credentials;")
	q(&o.PersonalFiles, "SELECT COUNT(*) FROM personal_files;")
	q(&o.PersonalSize, "SELECT COALESCE(SUM(file_size), 0) FROM personal_files;")
	q(&o.OpenInvites, "SELECT COUNT(*) FROM admin_invitations WHERE used = FALSE;")
	return o
}

/* Utilitaires partages */

// randomID produit un identifiant alphanumerique pour les fichiers personnels.
func randomID(n int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	max := big.NewInt(int64(len(alphabet)))
	var sb strings.Builder
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		sb.WriteByte(alphabet[v.Int64()])
	}
	return sb.String(), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
