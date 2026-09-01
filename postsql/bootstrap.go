package postsql

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx"
)

// Alphabet sans caracteres ambigus (ni 0/O, ni 1/I) : le code est relu dans les logs et recopie a la main.
const inviteAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

func generateInviteCode() (string, error) {
	const groups, groupLen = 4, 5
	max := big.NewInt(int64(len(inviteAlphabet)))

	var sb strings.Builder
	for g := 0; g < groups; g++ {
		if g > 0 {
			sb.WriteByte('-')
		}
		for i := 0; i < groupLen; i++ {
			n, err := rand.Int(rand.Reader, max)
			if err != nil {
				return "", err
			}
			sb.WriteByte(inviteAlphabet[n.Int64()])
		}
	}
	return sb.String(), nil
}

// BootstrapAdminInvitation permet de creer le tout premier administrateur sans toucher a la base.
// Tant qu'aucun compte n'existe, un code d'invitation est cree (ou reutilise s'il n'a pas servi)
// puis affiche dans les logs. Des qu'un administrateur est inscrit, la fonction ne fait plus rien.
func BootstrapAdminInvitation() {
	ReconnectDB()

	var users int
	if err := connPool.QueryRow("SELECT COUNT(*) FROM users;").Scan(&users); err != nil {
		fmt.Fprintf(os.Stderr, "Impossible de verifier les comptes existants : %v\n", err)
		return
	}
	if users > 0 {
		return
	}

	// Un code non utilise existe deja (redemarrage avant inscription) : on le reaffiche plutot
	// que d'en empiler un nouveau a chaque `docker compose up`.
	token := ""
	err := connPool.QueryRow("SELECT token FROM admin_invitations WHERE used = FALSE LIMIT 1;").Scan(&token)

	switch {
	case err == pgx.ErrNoRows || (err == nil && token == ""):
		token, err = generateInviteCode()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Impossible de generer un code d'invitation : %v\n", err)
			return
		}
		if _, err := connPool.Exec("INSERT INTO admin_invitations (token, used) VALUES ($1, FALSE);", token); err != nil {
			fmt.Fprintf(os.Stderr, "Impossible d'enregistrer le code d'invitation : %v\n", err)
			return
		}
	case err != nil:
		fmt.Fprintf(os.Stderr, "Impossible de lire les invitations : %v\n", err)
		return
	}

	printInviteBanner(token)
}

func printInviteBanner(token string) {
	url := strings.TrimSuffix(os.Getenv("PUBLIC_URL"), "/")
	if url == "" {
		url = "http://localhost:3333"
	}

	const width = 64
	border := strings.Repeat("=", width)

	line := func(s string) string {
		padding := width - 4 - utf8.RuneCountInString(s)
		if padding < 0 {
			padding = 0
		}
		return "| " + s + strings.Repeat(" ", padding) + " |"
	}

	fmt.Println()
	fmt.Println(border)
	fmt.Println(line("PREMIER DEMARRAGE - aucun compte administrateur"))
	fmt.Println(line(""))
	fmt.Println(line("  Code d'invitation :  " + token))
	fmt.Println(line(""))
	fmt.Println(line("  Inscription :  " + url + "/admin/register"))
	fmt.Println(line(""))
	fmt.Println(line("Ce code est a usage unique. Il reste affiche a chaque"))
	fmt.Println(line("demarrage tant qu'aucun compte n'a ete cree."))
	fmt.Println(border)
	fmt.Println()
}
