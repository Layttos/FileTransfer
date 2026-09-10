package postsql

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx"
)

// Reglages du bannissement, modifiables depuis le panneau.
const (
	SettingBanMinutes   = "ban_minutes"
	SettingBanThreshold = "ban_threshold"
	SettingBanWindow    = "ban_window_minutes"
	SettingBanAllowlist = "ban_allowlist"
)

// Valeurs de depart. La liste blanche contient les adresses de l'administrateur :
// une erreur de saisie ne doit pas l'enfermer dehors de son propre service.
var settingDefaults = map[string]string{
	SettingBanMinutes:   "60",
	SettingBanThreshold: "3",
	SettingBanWindow:    "15",
	SettingBanAllowlist: "82.67.50.41,82.64.210.158",
}

/* Reglages */

// Setting lit un reglage, en retombant sur sa valeur par defaut.
func Setting(key string) string {
	ReconnectDB()
	var value string
	err := connPool.QueryRow("SELECT value FROM settings WHERE key = $1;", key).Scan(&value)
	if err != nil || strings.TrimSpace(value) == "" {
		return settingDefaults[key]
	}
	return value
}

// SettingInt lit un reglage numerique. Une valeur absurde retombe sur le defaut,
// pour qu'une saisie fautive ne desactive pas la protection.
func SettingInt(key string, min, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(Setting(key)))
	if err != nil || n < min || n > max {
		n, _ = strconv.Atoi(settingDefaults[key])
	}
	return n
}

// SetSetting enregistre un reglage.
func SetSetting(key, value string) error {
	ReconnectDB()
	_, err := connPool.Exec(`
		INSERT INTO settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;`, key, value)
	return err
}

// BanSettings rassemble les reglages pour l'affichage.
type BanSettings struct {
	Minutes   int      `json:"minutes"`
	Threshold int      `json:"threshold"`
	Window    int      `json:"window"`
	Allowlist []string `json:"allowlist"`
}

func GetBanSettings() BanSettings {
	return BanSettings{
		Minutes:   SettingInt(SettingBanMinutes, 1, 525600), // jusqu'a un an
		Threshold: SettingInt(SettingBanThreshold, 1, 100),
		Window:    SettingInt(SettingBanWindow, 1, 1440),
		Allowlist: Allowlist(),
	}
}

// Allowlist renvoie les adresses jamais bannies.
func Allowlist() []string {
	out := []string{}
	for _, entry := range strings.Split(Setting(SettingBanAllowlist), ",") {
		if e := strings.TrimSpace(entry); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// IsAllowlisted indique si une adresse echappe au bannissement.
func IsAllowlisted(ip string) bool {
	ip = strings.TrimSpace(ip)
	for _, entry := range Allowlist() {
		if strings.EqualFold(entry, ip) {
			return true
		}
	}
	return false
}

/* Bannissements */

type Ban struct {
	IPAddr    string    `json:"ip_addr"`
	BannedAt  time.Time `json:"banned_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Reason    string    `json:"reason"`
	Attempts  int       `json:"attempts"`
	CreatedBy string    `json:"created_by"`
	Active    bool      `json:"active"`

	// Calcule par la base : Go et PostgreSQL ne s'accordent pas sur le fuseau
	// d'un TIMESTAMP sans fuseau, et soustraire des dates de part et d'autre
	// donnait un temps restant faux.
	RemainingMinutes int `json:"remaining_minutes"`
}

// BanStatus renvoie le bannissement en cours pour une adresse, s'il existe.
func BanStatus(ip string) (*Ban, bool) {
	ReconnectDB()
	var b Ban
	err := connPool.QueryRow(`
		SELECT ip_addr, banned_at, expires_at, reason, attempts, created_by,
		       CEIL(EXTRACT(EPOCH FROM (expires_at - CURRENT_TIMESTAMP)) / 60)::int
		FROM ip_bans WHERE ip_addr = $1 AND expires_at > CURRENT_TIMESTAMP;`, ip,
	).Scan(&b.IPAddr, &b.BannedAt, &b.ExpiresAt, &b.Reason, &b.Attempts, &b.CreatedBy,
		&b.RemainingMinutes)
	if err == pgx.ErrNoRows {
		return nil, false
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Lecture du bannissement impossible : %v\n", err)
		return nil, false
	}
	b.Active = true
	return &b, true
}

// BanIP bannit une adresse. Un bannissement existant est prolonge plutot que
// duplique. La liste blanche est respectee ici aussi, pas seulement a l'appel.
func BanIP(ip string, minutes int, reason, createdBy string, attempts int) error {
	ReconnectDB()
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return fmt.Errorf("adresse vide")
	}
	if IsAllowlisted(ip) {
		return fmt.Errorf("cette adresse est sur la liste blanche")
	}
	if minutes < 1 {
		minutes = SettingInt(SettingBanMinutes, 1, 525600)
	}

	// L'echeance est calculee par la base, avec sa propre horloge : c'est la
	// meme qui servira a la comparer, donc aucun decalage possible.
	_, err := connPool.Exec(fmt.Sprintf(`
		INSERT INTO ip_bans (ip_addr, banned_at, expires_at, reason, attempts, created_by)
		VALUES ($1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP + INTERVAL '%d minutes', $2, $3, $4)
		ON CONFLICT (ip_addr) DO UPDATE SET
			banned_at = CURRENT_TIMESTAMP, expires_at = EXCLUDED.expires_at,
			reason = EXCLUDED.reason, attempts = EXCLUDED.attempts,
			created_by = EXCLUDED.created_by;`, minutes),
		ip, reason, attempts, createdBy)
	if err == nil {
		RefreshBanCache()
	}
	return err
}

// UnbanIP leve un bannissement et efface les tentatives associees, pour que
// l'adresse reparte avec un compteur remis a zero.
func UnbanIP(ip string) error {
	ReconnectDB()
	tag, err := connPool.Exec("DELETE FROM ip_bans WHERE ip_addr = $1;", ip)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("aucun bannissement pour cette adresse")
	}
	ClearAttempts(ip)
	RefreshBanCache()
	return nil
}

// ListBans renvoie les bannissements, les actifs d'abord.
func ListBans() []Ban {
	ReconnectDB()
	out := []Ban{}
	rows, err := connPool.Query(`
		SELECT ip_addr, banned_at, expires_at, reason, attempts, created_by,
		       expires_at > CURRENT_TIMESTAMP AS active,
		       GREATEST(CEIL(EXTRACT(EPOCH FROM (expires_at - CURRENT_TIMESTAMP)) / 60), 0)::int
		FROM ip_bans ORDER BY active DESC, expires_at DESC LIMIT 200;`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Listing des bannissements impossible : %v\n", err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b Ban
		if err := rows.Scan(&b.IPAddr, &b.BannedAt, &b.ExpiresAt, &b.Reason,
			&b.Attempts, &b.CreatedBy, &b.Active, &b.RemainingMinutes); err != nil {
			return out
		}
		out = append(out, b)
	}
	return out
}

// PurgeExpiredBans nettoie les bannissements termines depuis plus d'une semaine.
func PurgeExpiredBans() {
	ReconnectDB()
	if _, err := connPool.Exec(
		"DELETE FROM ip_bans WHERE expires_at < CURRENT_TIMESTAMP - INTERVAL '7 days';"); err != nil {
		fmt.Fprintf(os.Stderr, "Purge des bannissements impossible : %v\n", err)
	}
	if _, err := connPool.Exec(
		"DELETE FROM login_attempts WHERE at < CURRENT_TIMESTAMP - INTERVAL '1 day';"); err != nil {
		fmt.Fprintf(os.Stderr, "Purge des tentatives impossible : %v\n", err)
	}
}

/* Tentatives */

// RecordFailure enregistre un echec de connexion et bannit l'adresse si le seuil
// est atteint dans la fenetre de temps. Renvoie le bannissement s'il vient
// d'etre prononce.
func RecordFailure(ip, identifier string) *Ban {
	ReconnectDB()
	if ip == "" || IsAllowlisted(ip) {
		return nil
	}

	if _, err := connPool.Exec(
		"INSERT INTO login_attempts (ip_addr, identifier) VALUES ($1, $2);", ip, identifier); err != nil {
		fmt.Fprintf(os.Stderr, "Enregistrement de la tentative impossible : %v\n", err)
		return nil
	}

	window := SettingInt(SettingBanWindow, 1, 1440)
	threshold := SettingInt(SettingBanThreshold, 1, 100)

	var count int
	if err := connPool.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM login_attempts
		WHERE ip_addr = $1 AND at > CURRENT_TIMESTAMP - INTERVAL '%d minutes';`, window),
		ip).Scan(&count); err != nil {
		fmt.Fprintf(os.Stderr, "Comptage des tentatives impossible : %v\n", err)
		return nil
	}

	if count < threshold {
		return nil
	}

	minutes := SettingInt(SettingBanMinutes, 1, 525600)
	reason := fmt.Sprintf("%d tentatives de connexion echouees", count)
	if err := BanIP(ip, minutes, reason, "", count); err != nil {
		fmt.Fprintf(os.Stderr, "Bannissement impossible : %v\n", err)
		return nil
	}
	ban, _ := BanStatus(ip)
	return ban
}

// ClearAttempts efface le compteur d'une adresse, apres une connexion reussie.
func ClearAttempts(ip string) {
	ReconnectDB()
	if _, err := connPool.Exec("DELETE FROM login_attempts WHERE ip_addr = $1;", ip); err != nil {
		fmt.Fprintf(os.Stderr, "Remise a zero des tentatives impossible : %v\n", err)
	}
}

// RecentAttempts renvoie le nombre d'echecs recents d'une adresse.
func RecentAttempts(ip string) int {
	ReconnectDB()
	window := SettingInt(SettingBanWindow, 1, 1440)
	var n int
	if err := connPool.QueryRow(fmt.Sprintf(`
		SELECT COUNT(*) FROM login_attempts
		WHERE ip_addr = $1 AND at > CURRENT_TIMESTAMP - INTERVAL '%d minutes';`, window),
		ip).Scan(&n); err != nil {
		return 0
	}
	return n
}

/* Cache des bannissements */

// Le filtre s'applique a chaque requete, y compris au telechargement d'un
// fichier de plusieurs gigaoctets. Interroger la base a chaque fois serait
// absurde : les bannissements actifs tiennent en memoire, et sont rafraichis
// a chaque modification ainsi que periodiquement.
var banCache = struct {
	sync.RWMutex
	until map[string]time.Time
}{until: make(map[string]time.Time)}

// RefreshBanCache recharge les bannissements actifs depuis la base.
func RefreshBanCache() {
	ReconnectDB()
	rows, err := connPool.Query(`
		SELECT ip_addr, EXTRACT(EPOCH FROM (expires_at - CURRENT_TIMESTAMP))::bigint
		FROM ip_bans WHERE expires_at > CURRENT_TIMESTAMP;`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Rechargement des bannissements impossible : %v\n", err)
		return
	}
	defer rows.Close()

	fresh := make(map[string]time.Time)
	for rows.Next() {
		var ip string
		var seconds int64
		if err := rows.Scan(&ip, &seconds); err != nil {
			return
		}
		// Converti en instant local a partir d'une duree : les deux horloges
		// n'ont jamais a s'accorder sur un fuseau.
		fresh[ip] = time.Now().Add(time.Duration(seconds) * time.Second)
	}

	banCache.Lock()
	banCache.until = fresh
	banCache.Unlock()
}

// StartBanCache charge le cache et le tient a jour. Le rafraichissement
// periodique couvre le cas de plusieurs instances partageant la meme base.
func StartBanCache() {
	RefreshBanCache()
	go func() {
		for range time.Tick(30 * time.Second) {
			RefreshBanCache()
		}
	}()
}

// IsBanned indique si une adresse est bannie, et jusqu'a quand. Lecture en
// memoire : aucun acces a la base sur le chemin des requetes.
func IsBanned(ip string) (time.Time, bool) {
	banCache.RLock()
	until, found := banCache.until[ip]
	banCache.RUnlock()

	if !found || time.Now().After(until) {
		return time.Time{}, false
	}
	return until, true
}
