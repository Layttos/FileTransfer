package routes

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Les ressources partagees sont referencees avec une empreinte de leur contenu :
// /assets/i18n.js devient /assets/i18n.js?v=a1b2c3d4.
//
// C'est indispensable ici : Nginx Proxy Manager met en cache tous les .js et .css
// pendant trente minutes et masque l'en-tete Cache-Control du serveur. Un
// dictionnaire mis a jour continuait donc d'etre servi dans son ancienne version,
// avec un HTML deja porteur des nouvelles cles — qui s'affichaient telles quelles.
// Changer l'URL est le seul moyen sur quand le proxy ignore les en-tetes.

var assetRef = regexp.MustCompile(`/assets/([A-Za-z0-9_.\-]+\.(?:js|css))`)

var assetVersions = struct {
	sync.RWMutex
	hash    map[string]string
	checked time.Time
}{hash: make(map[string]string)}

// refreshAssetVersions recalcule les empreintes. Appelee au demarrage puis au
// plus une fois par minute, pour qu'une modification en place soit prise en
// compte sans redemarrer.
func refreshAssetVersions() {
	entries, err := os.ReadDir("public/assets")
	if err != nil {
		return
	}

	fresh := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join("public/assets", entry.Name()))
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		fresh[entry.Name()] = hex.EncodeToString(sum[:])[:8]
	}

	assetVersions.Lock()
	assetVersions.hash = fresh
	assetVersions.checked = time.Now()
	assetVersions.Unlock()
}

func assetVersion(name string) string {
	assetVersions.RLock()
	stale := time.Since(assetVersions.checked) > time.Minute
	version := assetVersions.hash[name]
	assetVersions.RUnlock()

	if stale {
		refreshAssetVersions()
		assetVersions.RLock()
		version = assetVersions.hash[name]
		assetVersions.RUnlock()
	}
	return version
}

// versionAssets reecrit les references aux ressources d'une page.
func versionAssets(html string) string {
	return assetRef.ReplaceAllStringFunc(html, func(match string) string {
		name := strings.TrimPrefix(match, "/assets/")
		version := assetVersion(name)
		if version == "" {
			return match
		}
		return fmt.Sprintf("%s?v=%s", match, version)
	})
}

// InitAssets prepare les empreintes au demarrage.
func InitAssets() { refreshAssetVersions() }
