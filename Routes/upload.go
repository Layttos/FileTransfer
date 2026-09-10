package routes

import (
	"encoding/json"
	"filetransfer-backend/postsql"
	"filetransfer-backend/vault"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 1024*1024)
	},
}

// Reseaux depuis lesquels les en-tetes de proxy sont crus. Par defaut les plages
// privees, ou vit le reverse proxy. TRUSTED_PROXIES permet de restreindre a
// l'adresse exacte du proxy, ce qui est preferable quand d'autres conteneurs
// partagent le meme reseau.
var trustedProxies = func() []*net.IPNet {
	spec := os.Getenv("TRUSTED_PROXIES")
	if strings.TrimSpace(spec) == "" {
		spec = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"
	}
	var nets []*net.IPNet
	for _, entry := range strings.Split(spec, ",") {
		if _, block, err := net.ParseCIDR(strings.TrimSpace(entry)); err == nil {
			nets = append(nets, block)
		}
	}
	return nets
}()

func fromTrustedProxy(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, block := range trustedProxies {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP resout l'adresse du visiteur.
//
// Derriere un reverse proxy, RemoteAddr vaut l'IP du proxy : il faut lire les
// en-tetes qu'il pose. Mais ces en-tetes sont fournis par le client tant qu'un
// proxy ne les remplace pas, et cette adresse decide desormais des
// bannissements. Les croire sans condition permettrait de contourner un
// bannissement, d'en faire prononcer un contre l'adresse de quelqu'un d'autre,
// ou de se faire passer pour une adresse de la liste blanche.
//
// Ils ne sont donc lus que si la connexion vient d'un proxy de confiance.
func clientIP(rq *http.Request) string {
	direct := rq.RemoteAddr
	if host, _, err := net.SplitHostPort(direct); err == nil {
		direct = host
	}

	if !fromTrustedProxy(rq.RemoteAddr) {
		return direct
	}

	// X-Real-IP est pose par le proxy, qui ecrase toute valeur du client.
	if ip := strings.TrimSpace(rq.Header.Get("X-Real-IP")); net.ParseIP(ip) != nil {
		return ip
	}

	// X-Forwarded-For est une liste ou le proxy ajoute a la suite : on remonte
	// depuis la fin en ignorant les proxies connus, la premiere adresse
	// restante est celle que le client n'a pas pu choisir.
	if fwd := rq.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate := strings.TrimSpace(parts[i])
			ip := net.ParseIP(candidate)
			if ip == nil {
				continue
			}
			if !fromTrustedProxy(candidate) {
				return candidate
			}
		}
	}
	return direct
}

func HandleUpload(w http.ResponseWriter, rq *http.Request) {
	if rq.Method != "POST" {
		fmt.Fprintf(w, "<h1>Can't identify the request</h1>")
		return
	}

	reader, err := rq.MultipartReader()
	if err != nil {
		// Sans ce return, reader est nil et la boucle ci-dessous dereference nil.
		writeUploadError(w, http.StatusBadRequest, "Requete multipart invalide")
		return
	}

	for {
		part, err := reader.NextPart()
		if err == io.EOF { // zé fini de liiiire :3
			break
		}
		if err != nil {
			writeUploadError(w, http.StatusInternalServerError, "Erreur pendant la lecture du flux envoye")
			return
		}

		if part.FormName() == "file" {
			fn := filepath.Base(part.FileName())
			if fn == "" || strings.HasPrefix(fn, ".") || strings.HasPrefix(fn, "_") { // on va éviter les conneries ici
				continue
			}

			id := postsql.GenFileID()

			dirPath := filepath.Join(os.Getenv("FILES_PATH"), id)
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				writeUploadError(w, http.StatusInternalServerError, "Impossible de creer le repertoire de destination")
				return
			}

			dest, err := os.Create(filepath.Join(dirPath, fn))
			if err != nil {
				os.RemoveAll(dirPath)
				writeUploadError(w, http.StatusInternalServerError, "Impossible de creer le fichier sur le serveur")
				return
			}

			// Le contenu traverse le coffre : chiffre, et compresse s'il s'y prete.
			enc, encErr := vault.NewWriter(dest)
			if encErr != nil {
				dest.Close()
				os.RemoveAll(dirPath)
				writeUploadError(w, http.StatusInternalServerError, "Chiffrement indisponible")
				return
			}

			buffer := bufferPool.Get().([]byte)
			size, err := io.CopyBuffer(enc, part, buffer)
			bufferPool.Put(buffer)
			if err == nil {
				err = enc.Close()
			}
			closeErr := dest.Close()

			// Sur un transfert de plusieurs Go, une coupure client ou un disque plein
			// laisse un fichier tronque. Aucune ligne n'est encore ecrite en base :
			// on efface le repertoire pour ne pas accumuler d'orphelins sur le disque.
			if err != nil || closeErr != nil {
				os.RemoveAll(dirPath)
				if err == nil {
					err = closeErr
				}
				fmt.Fprintf(os.Stderr, "Upload interrompu (%s, %d octets ecrits) : %v\n", fn, size, err)
				auditAnon(rq, postsql.LevelError, postsql.CatUpload, "depot-interrompu", fn,
					fmt.Sprintf("%d octets ecrits : %v", size, err))
				writeUploadError(w, http.StatusInternalServerError, "Transfert interrompu avant la fin")
				return
			}

			ip := clientIP(rq)

			postsql.PushFile(id, fn, size, ip, rq.URL.Query().Get("password"))

			protege := ""
			if rq.URL.Query().Get("password") != "" {
				protege = ", protege par mot de passe"
			}
			auditAnon(rq, postsql.LevelInfo, postsql.CatUpload, "depot", id,
				fmt.Sprintf("%s (%d octets%s)", fn, size, protege))
			response := map[string]interface{}{
				"id":   id,
				"size": size,
				"ip":   ip,
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
			return
		}
	}
	writeUploadError(w, http.StatusBadRequest, "Aucun fichier trouve dans la requete")
}

// writeUploadError renvoie une erreur en JSON, avec le bon code HTTP, pour que le
// frontend puisse afficher autre chose qu'un generique "Erreur serveur".
func writeUploadError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
