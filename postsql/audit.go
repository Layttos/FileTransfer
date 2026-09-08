package postsql

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx"
)

// Niveaux et categories du journal.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"

	CatAuth     = "auth"     // connexions, deconnexions, passkeys, mots de passe
	CatUpload   = "upload"   // depots de fichiers
	CatDownload = "download" // telechargements
	CatFile     = "file"     // renommage, suppression, changement d'identifiant
	CatPersonal = "personal" // cloud personnel
	CatAdmin    = "admin"    // comptes et invitations
	CatSystem   = "system"   // demarrage, purges
)

// AuditEntry est une ligne du journal.
type AuditEntry struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Level     string    `json:"level"`
	Category  string    `json:"category"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	ActorID   *int      `json:"actor_id"`
	IPAddr    string    `json:"ip_addr"`
	Target    string    `json:"target"`
	Detail    string    `json:"detail"`
	UserAgent string    `json:"user_agent"`
}

// auditQueue decouple l'ecriture du journal du chemin des requetes : un
// telechargement ne doit jamais ralentir ni echouer a cause du journal.
var auditQueue = make(chan AuditEntry, 512)

// StartAuditWriter lance le consommateur du journal. Appele une fois au demarrage.
func StartAuditWriter() {
	go func() {
		for e := range auditQueue {
			writeAudit(e)
		}
	}()
}

func writeAudit(e AuditEntry) {
	defer func() {
		// Le journal ne doit jamais faire tomber le serveur.
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "Journal : panique ignoree (%v)\n", r)
		}
	}()

	ReconnectDB()
	if len(e.UserAgent) > 512 {
		e.UserAgent = e.UserAgent[:512]
	}
	if len(e.Detail) > 4000 {
		e.Detail = e.Detail[:4000]
	}
	if len(e.Target) > 255 {
		e.Target = e.Target[:255]
	}

	_, err := connPool.Exec(`
		INSERT INTO audit_log (level, category, action, actor, actor_id, ip_addr, target, detail, user_agent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);`,
		e.Level, e.Category, e.Action, e.Actor, e.ActorID, e.IPAddr, e.Target, e.Detail, e.UserAgent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Journal : ecriture impossible (%s/%s) : %v\n", e.Category, e.Action, err)
	}
}

// Audit enregistre un evenement. Non bloquant : si la file est pleine,
// l'evenement part sur la sortie standard plutot que de retenir la requete.
func Audit(e AuditEntry) {
	if e.Level == "" {
		e.Level = LevelInfo
	}
	if e.Actor == "" {
		e.Actor = "anonyme"
	}
	select {
	case auditQueue <- e:
	default:
		fmt.Printf("[JOURNAL SATURE] %s/%s %s %s %s\n", e.Category, e.Action, e.Actor, e.Target, e.Detail)
	}
}

// AuditFilter decrit une recherche dans le journal.
type AuditFilter struct {
	Category string
	Level    string
	Search   string
	Offset   int
	Limit    int
}

// ListAudit renvoie les entrees correspondantes, les plus recentes d'abord,
// ainsi que le nombre total pour la pagination.
func ListAudit(f AuditFilter) ([]AuditEntry, int64) {
	ReconnectDB()
	out := []AuditEntry{}

	where := []string{"1 = 1"}
	args := []interface{}{}
	add := func(clause string, value interface{}) {
		args = append(args, value)
		where = append(where, strings.Replace(clause, "?", "$"+strconv.Itoa(len(args)), 1))
	}

	if f.Category != "" && f.Category != "all" {
		add("category = ?", f.Category)
	}
	if f.Level != "" && f.Level != "all" {
		add("level = ?", f.Level)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		add("(actor ILIKE ? OR target ILIKE ? OR detail ILIKE ? OR action ILIKE ? OR ip_addr ILIKE ?)",
			"%"+s+"%")
		// La meme valeur sert aux cinq colonnes : on duplique le parametre.
		last := "$" + strconv.Itoa(len(args))
		where[len(where)-1] = strings.ReplaceAll(where[len(where)-1], "?", last)
	}
	clause := strings.Join(where, " AND ")

	var total int64
	if err := connPool.QueryRow("SELECT COUNT(*) FROM audit_log WHERE "+clause, args...).Scan(&total); err != nil {
		fmt.Fprintf(os.Stderr, "Journal : comptage impossible : %v\n", err)
		return out, 0
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	args = append(args, limit, f.Offset)
	query := fmt.Sprintf(
		"SELECT id, at, level, category, action, actor, actor_id, ip_addr, target, detail, user_agent "+
			"FROM audit_log WHERE %s ORDER BY at DESC, id DESC LIMIT $%d OFFSET $%d",
		clause, len(args)-1, len(args))

	rows, err := connPool.Query(query, args...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Journal : lecture impossible : %v\n", err)
		return out, total
	}
	defer rows.Close()

	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Level, &e.Category, &e.Action, &e.Actor,
			&e.ActorID, &e.IPAddr, &e.Target, &e.Detail, &e.UserAgent); err != nil {
			fmt.Fprintf(os.Stderr, "Journal : ligne illisible : %v\n", err)
			return out, total
		}
		out = append(out, e)
	}
	return out, total
}

// AuditStats renvoie le nombre d'evenements par categorie sur les 24 dernieres heures.
func AuditStats() map[string]int64 {
	ReconnectDB()
	out := map[string]int64{}
	rows, err := connPool.Query(`
		SELECT category, COUNT(*) FROM audit_log
		WHERE at > CURRENT_TIMESTAMP - INTERVAL '24 hours'
		GROUP BY category;`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		var n int64
		if err := rows.Scan(&c, &n); err != nil {
			return out
		}
		out[c] = n
	}
	return out
}

// PurgeAudit supprime les entrees plus anciennes que AUDIT_RETENTION_DAYS
// (90 jours par defaut, 0 pour ne jamais purger).
func PurgeAudit() {
	ReconnectDB()
	days := 90
	if v := os.Getenv("AUDIT_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			days = n
		}
	}
	if days <= 0 {
		return
	}
	tag, err := connPool.Exec(
		fmt.Sprintf("DELETE FROM audit_log WHERE at < CURRENT_TIMESTAMP - INTERVAL '%d days';", days))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Journal : purge impossible : %v\n", err)
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		fmt.Printf("Journal : %d entrees de plus de %d jours purgees\n", n, days)
	}
}

var _ = pgx.ErrNoRows
