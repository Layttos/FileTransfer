package postsql

import (
	"fmt"
	"os"

	"github.com/jackc/pgx"
)

// migrate cree les tables et colonnes ajoutees apres la premiere version.
// Tout est idempotent : la fonction s'execute a chaque demarrage sans effet de bord.
func migrate(conn *pgx.Conn) {
	statements := []string{
		// Sessions administrateur. Remplace le stockage du mot de passe en clair
		// dans un cookie lisible en JavaScript.
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			token_hash CHAR(64) PRIMARY KEY,
			user_id    INTEGER NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL,
			ip_addr    VARCHAR(45) NOT NULL DEFAULT '',
			user_agent VARCHAR(512) NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX IF NOT EXISTS admin_sessions_user_idx ON admin_sessions (user_id);`,

		// Cles WebAuthn (passkeys) enregistrees par les administrateurs.
		// Le credential complet est conserve en JSON : les champs de la bibliotheque
		// evoluent d'une version a l'autre, un mapping colonne par colonne casserait.
		`CREATE TABLE IF NOT EXISTS admin_credentials (
			id              SERIAL PRIMARY KEY,
			user_id         INTEGER NOT NULL,
			name            VARCHAR(255) NOT NULL DEFAULT 'Passkey',
			credential_id   BYTEA NOT NULL UNIQUE,
			credential_json BYTEA NOT NULL,
			sign_count      BIGINT NOT NULL DEFAULT 0,
			created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_used       TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS admin_credentials_user_idx ON admin_credentials (user_id);`,

		// Politique d'authentification choisie par chaque administrateur :
		//   password          : mot de passe seul (defaut)
		//   passkey_or_password : passkey acceptee, mot de passe conserve en secours
		//   password_and_passkey : les deux exiges
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS auth_policy VARCHAR(32) NOT NULL DEFAULT 'password';`,

		// Espace de stockage personnel de chaque administrateur.
		`CREATE TABLE IF NOT EXISTS personal_files (
			id         VARCHAR(12) PRIMARY KEY,
			user_id    INTEGER NOT NULL,
			file_name  VARCHAR(255) NOT NULL,
			file_size  BIGINT NOT NULL,
			folder     VARCHAR(512) NOT NULL DEFAULT '/',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE INDEX IF NOT EXISTS personal_files_user_idx ON personal_files (user_id);`,

		// Tracabilite des invitations, pour les gerer depuis le dashboard
		// au lieu d'un INSERT SQL a la main.
		`ALTER TABLE admin_invitations ADD COLUMN IF NOT EXISTS created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP;`,
		`ALTER TABLE admin_invitations ADD COLUMN IF NOT EXISTS created_by VARCHAR(255) NOT NULL DEFAULT '';`,
		`ALTER TABLE admin_invitations ADD COLUMN IF NOT EXISTS used_by VARCHAR(255) NOT NULL DEFAULT '';`,
	}

	for _, stmt := range statements {
		if _, err := conn.Exec(stmt); err != nil {
			fmt.Fprintf(os.Stderr, "Migration echouee (%.60s...) : %v\n", stmt, err)
			os.Exit(1)
		}
	}
}
