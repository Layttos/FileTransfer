package postsql

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx"
)

// Duree de validite d'une session admin. Chaque requete authentifiee la reconduit.
const sessionLifetime = 7 * 24 * time.Hour

// AdminUser est l'identite resolue a partir d'un cookie de session.
type AdminUser struct {
	ID         int    `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	AuthPolicy string `json:"auth_policy"`
}

// SessionInfo decrit une session active, pour l'affichage dans le dashboard.
type SessionInfo struct {
	ID        string    `json:"id"` // prefixe du hash, suffisant pour cibler une revocation
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	ExpiresAt time.Time `json:"expires_at"`
	IPAddr    string    `json:"ip_addr"`
	UserAgent string    `json:"user_agent"`
	Current   bool      `json:"current"`
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession emet un jeton de session pour un utilisateur deja authentifie.
// Le jeton n'est stocke qu'en clair dans le cookie : la base ne garde que son
// empreinte, pour qu'une fuite de la base ne permette pas d'usurper une session.
func CreateSession(userID int, ip, userAgent string) (string, error) {
	ReconnectDB()

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)

	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}

	_, err := connPool.Exec(`
		INSERT INTO admin_sessions (token_hash, user_id, expires_at, ip_addr, user_agent)
		VALUES ($1, $2, $3, $4, $5);`,
		hashToken(token), userID, time.Now().Add(sessionLifetime), ip, userAgent,
	)
	if err != nil {
		return "", err
	}
	return token, nil
}

// LookupSession valide un jeton et renvoie l'utilisateur correspondant.
// Une session expiree est refusee et supprimee au passage.
func LookupSession(token string) (*AdminUser, bool) {
	if token == "" {
		return nil, false
	}
	ReconnectDB()

	h := hashToken(token)
	var (
		u         AdminUser
		expiresAt time.Time
	)

	err := connPool.QueryRow(`
		SELECT u.id, u.username, u.email_address, u.first_name, u.last_name,
		       COALESCE(u.auth_policy, 'password'), s.expires_at
		FROM admin_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1;`, h,
	).Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.AuthPolicy, &expiresAt)

	if err == pgx.ErrNoRows {
		return nil, false
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Lecture de session impossible : %v\n", err)
		return nil, false
	}

	if time.Now().After(expiresAt) {
		_, _ = connPool.Exec("DELETE FROM admin_sessions WHERE token_hash = $1;", h)
		return nil, false
	}

	// Session glissante : tant qu'elle sert, elle se prolonge.
	_, _ = connPool.Exec(
		"UPDATE admin_sessions SET last_seen = CURRENT_TIMESTAMP, expires_at = $2 WHERE token_hash = $1;",
		h, time.Now().Add(sessionLifetime),
	)

	return &u, true
}

// RevokeSession supprime la session portee par ce jeton (deconnexion).
func RevokeSession(token string) {
	if token == "" {
		return
	}
	ReconnectDB()
	_, _ = connPool.Exec("DELETE FROM admin_sessions WHERE token_hash = $1;", hashToken(token))
}

// RevokeSessionByPrefix revoque une session listee dans le dashboard, en s'assurant
// qu'elle appartient bien a l'utilisateur qui la revoque.
func RevokeSessionByPrefix(userID int, prefix string) bool {
	ReconnectDB()
	if len(prefix) < 8 {
		return false
	}
	tag, err := connPool.Exec(
		"DELETE FROM admin_sessions WHERE user_id = $1 AND token_hash LIKE $2;",
		userID, prefix+"%",
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Revocation de session impossible : %v\n", err)
		return false
	}
	return tag.RowsAffected() > 0
}

// RevokeAllSessions deconnecte toutes les sessions d'un utilisateur, sauf
// eventuellement celle en cours.
func RevokeAllSessions(userID int, exceptToken string) int64 {
	ReconnectDB()
	var (
		tag pgx.CommandTag
		err error
	)
	if exceptToken == "" {
		tag, err = connPool.Exec("DELETE FROM admin_sessions WHERE user_id = $1;", userID)
	} else {
		tag, err = connPool.Exec(
			"DELETE FROM admin_sessions WHERE user_id = $1 AND token_hash != $2;",
			userID, hashToken(exceptToken))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Revocation des sessions impossible : %v\n", err)
		return 0
	}
	return tag.RowsAffected()
}

// ListSessions renvoie les sessions actives d'un utilisateur, la courante en premier.
func ListSessions(userID int, currentToken string) []SessionInfo {
	ReconnectDB()
	out := []SessionInfo{}

	rows, err := connPool.Query(`
		SELECT token_hash, created_at, last_seen, expires_at, ip_addr, user_agent
		FROM admin_sessions
		WHERE user_id = $1 AND expires_at > CURRENT_TIMESTAMP
		ORDER BY last_seen DESC;`, userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Listing des sessions impossible : %v\n", err)
		return out
	}
	defer rows.Close()

	currentHash := ""
	if currentToken != "" {
		currentHash = hashToken(currentToken)
	}

	for rows.Next() {
		var (
			h string
			s SessionInfo
		)
		if err := rows.Scan(&h, &s.CreatedAt, &s.LastSeen, &s.ExpiresAt, &s.IPAddr, &s.UserAgent); err != nil {
			fmt.Fprintf(os.Stderr, "Lecture d'une session impossible : %v\n", err)
			return out
		}
		s.ID = h[:16]
		s.Current = h == currentHash
		out = append(out, s)
	}
	return out
}

// PurgeExpiredSessions nettoie les sessions perimees (appele au demarrage).
func PurgeExpiredSessions() {
	ReconnectDB()
	if _, err := connPool.Exec("DELETE FROM admin_sessions WHERE expires_at < CURRENT_TIMESTAMP;"); err != nil {
		fmt.Fprintf(os.Stderr, "Purge des sessions impossible : %v\n", err)
	}
}
