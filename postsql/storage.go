package postsql

import (
	"os"
	"strconv"
	"strings"
)

// Storage decrit l'occupation du stockage, telle qu'affichee dans le dashboard.
type Storage struct {
	Used       int64  `json:"used"`        // somme des tailles connues en base
	PublicUsed int64  `json:"public_used"` // part des transferts publics
	PersonUsed int64  `json:"person_used"` // part des clouds personnels
	Max        int64  `json:"max"`         // 0 = inconnu
	DiskFree   int64  `json:"disk_free"`   // 0 = inconnu
	Source     string `json:"source"`      // "quota" ou "disque"
}

// parseSizeSpec accepte 500G, 2T, 1500M, ou un nombre d'octets brut.
func parseSizeSpec(spec string) int64 {
	spec = strings.TrimSpace(strings.ToUpper(spec))
	if spec == "" {
		return 0
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(spec, "T"):
		mult, spec = 1<<40, strings.TrimSuffix(spec, "T")
	case strings.HasSuffix(spec, "G"):
		mult, spec = 1<<30, strings.TrimSuffix(spec, "G")
	case strings.HasSuffix(spec, "M"):
		mult, spec = 1<<20, strings.TrimSuffix(spec, "M")
	case strings.HasSuffix(spec, "K"):
		mult, spec = 1<<10, strings.TrimSuffix(spec, "K")
	case strings.HasSuffix(spec, "B"):
		spec = strings.TrimSuffix(spec, "B")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(spec), 64)
	if err != nil || n < 0 {
		return 0
	}
	return int64(n * float64(mult))
}

// GetStorage calcule l'occupation. Le plafond vient de STORAGE_MAX si elle est
// definie, sinon de la taille reelle du systeme de fichiers de stockage.
func GetStorage() Storage {
	ReconnectDB()
	var s Storage

	if err := connPool.QueryRow(
		"SELECT COALESCE(SUM(file_size), 0) FROM file_transfer;").Scan(&s.PublicUsed); err != nil {
		s.PublicUsed = 0
	}
	if err := connPool.QueryRow(
		"SELECT COALESCE(SUM(file_size), 0) FROM personal_files;").Scan(&s.PersonUsed); err != nil {
		s.PersonUsed = 0
	}
	s.Used = s.PublicUsed + s.PersonUsed

	if max := parseSizeSpec(os.Getenv("STORAGE_MAX")); max > 0 {
		s.Max, s.Source = max, "quota"
	}

	root := os.Getenv("FILES_PATH")
	if root == "" {
		root = "."
	}
	if total, free, ok := diskCapacity(root); ok {
		s.DiskFree = free
		if s.Max == 0 {
			s.Max, s.Source = total, "disque"
		}
	}
	return s
}
