package postsql

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx"
)

// WebAuthnUser adapte un compte administrateur a l'interface attendue par la
// bibliotheque WebAuthn.
type WebAuthnUser struct {
	User        *AdminUser
	credentials []webauthn.Credential
}

// Le handle WebAuthn doit etre opaque et stable : l'identifiant numerique du
// compte, sur 8 octets, remplit les deux conditions et se re-resout directement
// lors d'une connexion par passkey decouvrable.
func encodeHandle(id int) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(id))
	return b
}

func decodeHandle(h []byte) (int, bool) {
	if len(h) != 8 {
		return 0, false
	}
	return int(binary.BigEndian.Uint64(h)), true
}

func (w *WebAuthnUser) WebAuthnID() []byte   { return encodeHandle(w.User.ID) }
func (w *WebAuthnUser) WebAuthnName() string { return w.User.Username }
func (w *WebAuthnUser) WebAuthnDisplayName() string {
	name := w.User.FirstName + " " + w.User.LastName
	if len(name) <= 1 {
		return w.User.Username
	}
	return name
}
func (w *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential { return w.credentials }

// NewWebAuthnUser charge un compte et ses passkeys.
func NewWebAuthnUser(u *AdminUser) *WebAuthnUser {
	return &WebAuthnUser{User: u, credentials: loadCredentials(u.ID)}
}

// UserByWebAuthnHandle resout le compte designe par un handle, pour les
// connexions par passkey ou l'utilisateur n'a pas saisi son identifiant.
func UserByWebAuthnHandle(handle []byte) (*AdminUser, bool) {
	id, ok := decodeHandle(handle)
	if !ok {
		return nil, false
	}
	return AdminGetUserByID(id)
}

func loadCredentials(userID int) []webauthn.Credential {
	ReconnectDB()
	out := []webauthn.Credential{}

	rows, err := connPool.Query(
		"SELECT credential_json FROM admin_credentials WHERE user_id = $1 ORDER BY id;", userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Chargement des passkeys impossible : %v\n", err)
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			fmt.Fprintf(os.Stderr, "Lecture d'une passkey impossible : %v\n", err)
			return out
		}
		var c webauthn.Credential
		if err := json.Unmarshal(raw, &c); err != nil {
			fmt.Fprintf(os.Stderr, "Passkey illisible ignoree : %v\n", err)
			continue
		}
		out = append(out, c)
	}
	return out
}

// CredentialInfo est la vue presentee dans le dashboard.
type CredentialInfo struct {
	ID        int        `json:"id"`
	Name      string     `json:"name"`
	SignCount int64      `json:"sign_count"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used"`
}

func CountCredentials(userID int) int {
	ReconnectDB()
	var n int
	if err := connPool.QueryRow(
		"SELECT COUNT(*) FROM admin_credentials WHERE user_id = $1;", userID).Scan(&n); err != nil {
		return 0
	}
	return n
}

func ListCredentials(userID int) []CredentialInfo {
	ReconnectDB()
	out := []CredentialInfo{}
	rows, err := connPool.Query(`
		SELECT id, name, sign_count, created_at, last_used
		FROM admin_credentials WHERE user_id = $1 ORDER BY created_at;`, userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Listing des passkeys impossible : %v\n", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c CredentialInfo
		if err := rows.Scan(&c.ID, &c.Name, &c.SignCount, &c.CreatedAt, &c.LastUsed); err != nil {
			return out
		}
		out = append(out, c)
	}
	return out
}

// AddCredential enregistre une nouvelle passkey pour un administrateur.
func AddCredential(userID int, name string, cred *webauthn.Credential) error {
	ReconnectDB()
	if name == "" {
		name = "Passkey"
	}
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	if _, err = connPool.Exec(`
		INSERT INTO admin_credentials (user_id, name, credential_id, credential_json, sign_count)
		VALUES ($1, $2, $3, $4, $5);`,
		userID, name, cred.ID, raw, int64(cred.Authenticator.SignCount)); err != nil {
		return err
	}

	// Une passkey enregistree doit servir immediatement. Sans cela le compte reste
	// en "mot de passe seul" et le serveur refuse la connexion par passkey, sans
	// que rien ne l'explique a l'utilisateur. On ne touche pas a une politique
	// deja choisie explicitement (deux facteurs).
	if _, err := connPool.Exec(
		"UPDATE users SET auth_policy = $1 WHERE id = $2 AND auth_policy = $3;",
		PolicyPasskeyOrPassword, userID, PolicyPassword); err != nil {
		fmt.Fprintf(os.Stderr, "Activation de la passkey pour la connexion impossible : %v\n", err)
	}
	return nil
}

// RenameCredential change le libelle d'une passkey.
func RenameCredential(userID, credID int, name string) error {
	ReconnectDB()
	if name == "" {
		return fmt.Errorf("nom vide")
	}
	tag, err := connPool.Exec(
		"UPDATE admin_credentials SET name = $1 WHERE id = $2 AND user_id = $3;", name, credID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("passkey introuvable")
	}
	return nil
}

// DeleteCredential retire une passkey. Retirer la derniere alors que la politique
// l'exige enfermerait l'administrateur dehors : on repasse au mot de passe seul.
func DeleteCredential(userID, credID int) error {
	ReconnectDB()
	tag, err := connPool.Exec(
		"DELETE FROM admin_credentials WHERE id = $1 AND user_id = $2;", credID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("passkey introuvable")
	}
	if CountCredentials(userID) == 0 {
		if _, err := connPool.Exec(
			"UPDATE users SET auth_policy = $1 WHERE id = $2;", PolicyPassword, userID); err != nil {
			fmt.Fprintf(os.Stderr, "Retour a la politique mot de passe impossible : %v\n", err)
		}
	}
	return nil
}

// TouchCredential met a jour le compteur anti-rejeu apres une connexion reussie.
func TouchCredential(cred *webauthn.Credential) {
	ReconnectDB()
	raw, err := json.Marshal(cred)
	if err != nil {
		return
	}
	if _, err := connPool.Exec(`
		UPDATE admin_credentials
		SET credential_json = $1, sign_count = $2, last_used = CURRENT_TIMESTAMP
		WHERE credential_id = $3;`,
		raw, int64(cred.Authenticator.SignCount), cred.ID); err != nil {
		fmt.Fprintf(os.Stderr, "Mise a jour de la passkey impossible : %v\n", err)
	}
}

// CredentialOwner retrouve le proprietaire d'une passkey.
func CredentialOwner(credentialID []byte) (*AdminUser, bool) {
	ReconnectDB()
	var userID int
	err := connPool.QueryRow(
		"SELECT user_id FROM admin_credentials WHERE credential_id = $1;", credentialID).Scan(&userID)
	if err == pgx.ErrNoRows {
		return nil, false
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Proprietaire de passkey introuvable : %v\n", err)
		return nil, false
	}
	return AdminGetUserByID(userID)
}
