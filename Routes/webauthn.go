package routes

import (
	"crypto/rand"
	"encoding/hex"
	"filetransfer-backend/postsql"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Une ceremonie WebAuthn se deroule en deux requetes (begin puis finish) et il
// faut conserver le defi entre les deux. Un stockage memoire suffit : ces etats
// vivent quelques minutes et un redemarrage n'a pour effet que d'obliger a
// recommencer la ceremonie.
type ceremony struct {
	data    *webauthn.SessionData
	userID  int
	purpose string // "register", "login", "2fa"
	expires time.Time
}

var ceremonies = struct {
	sync.Mutex
	m map[string]*ceremony
}{m: make(map[string]*ceremony)}

const ceremonyTTL = 5 * time.Minute

func putCeremony(c *ceremony) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	key := hex.EncodeToString(raw)
	c.expires = time.Now().Add(ceremonyTTL)

	ceremonies.Lock()
	defer ceremonies.Unlock()
	for k, v := range ceremonies.m { // purge opportuniste
		if time.Now().After(v.expires) {
			delete(ceremonies.m, k)
		}
	}
	ceremonies.m[key] = c
	return key, nil
}

func takeCeremony(key, purpose string) (*ceremony, bool) {
	ceremonies.Lock()
	defer ceremonies.Unlock()
	c, ok := ceremonies.m[key]
	if !ok || time.Now().After(c.expires) || c.purpose != purpose {
		delete(ceremonies.m, key)
		return nil, false
	}
	delete(ceremonies.m, key) // usage unique
	return c, true
}

// peekCeremony consulte une ceremonie sans la consommer (etape intermediaire
// d'une connexion a deux facteurs).
func peekCeremony(key, purpose string) (*ceremony, bool) {
	ceremonies.Lock()
	defer ceremonies.Unlock()
	c, ok := ceremonies.m[key]
	if !ok || time.Now().After(c.expires) || c.purpose != purpose {
		return nil, false
	}
	return c, true
}

// StartPendingPasskey ouvre une attente de second facteur apres un mot de passe
// valide, quand la politique du compte exige les deux.
func StartPendingPasskey(userID int) (string, error) {
	return putCeremony(&ceremony{userID: userID, purpose: "2fa"})
}

// webAuthnFor construit la configuration WebAuthn a partir de l'origine publique.
// Le RPID doit correspondre exactement au domaine visite, sinon le navigateur
// refuse la ceremonie.
func webAuthnFor(req *http.Request) (*webauthn.WebAuthn, error) {
	origin := publicOrigin(req)
	u, err := url.Parse(origin)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("origine publique invalide (%s) : definissez PUBLIC_URL", origin)
	}
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "FileTransfer",
		RPID:          u.Hostname(),
		RPOrigins:     []string{origin},

		// Cle residente exigee : sans elle, la cle n'est pas une "passkey" au sens
		// des gestionnaires (Bitwarden, iCloud, 1Password) qui refusent alors de la
		// stocker, et la connexion sans identifiant ne peut rien retrouver.
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationPreferred,
		},
		AttestationPreference: protocol.PreferNoAttestation,
	})
}

/* Enregistrement d'une passkey (administrateur deja connecte) */

func HandleWebAuthnRegisterBegin(w http.ResponseWriter, req *http.Request) {
	user, _, ok := requireAdmin(w, req)
	if !ok {
		return
	}
	wa, err := webAuthnFor(req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	waUser := postsql.NewWebAuthnUser(user)
	options, sessionData, err := wa.BeginRegistration(waUser,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationPreferred,
		}),
		// credProps nous dit si l'authenticateur a bien cree une cle residente.
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
	)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Ceremonie impossible : "+err.Error())
		return
	}

	key, err := putCeremony(&ceremony{data: sessionData, userID: user.ID, purpose: "register"})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Ceremonie impossible")
		return
	}

	writeOK(w, map[string]interface{}{"ceremony": key, "options": options.Response})
}

func HandleWebAuthnRegisterFinish(w http.ResponseWriter, req *http.Request) {
	user, _, ok := requireAdmin(w, req)
	if !ok {
		return
	}

	key := req.URL.Query().Get("ceremony")
	name := strings.TrimSpace(req.URL.Query().Get("name"))
	if name == "" {
		name = "Passkey"
	}
	if len(name) > 64 {
		name = name[:64]
	}

	c, found := takeCeremony(key, "register")
	if !found || c.userID != user.ID {
		writeErr(w, http.StatusBadRequest, "Ceremonie expiree, recommencez")
		return
	}

	wa, err := webAuthnFor(req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	waUser := postsql.NewWebAuthnUser(user)
	cred, err := wa.FinishRegistration(waUser, *c.data, req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "Passkey refusee : "+err.Error())
		return
	}

	if err := postsql.AddCredential(user.ID, name, cred); err != nil {
		writeErr(w, http.StatusInternalServerError, "Enregistrement impossible : "+err.Error())
		return
	}
	audit(req, user, postsql.LevelWarn, postsql.CatAuth, "passkey-enregistree", name, "")
	writeOK(w, map[string]string{"status": "PASSKEY_REGISTERED", "name": name})
}

/* Connexion par passkey */

func HandleWebAuthnLoginBegin(w http.ResponseWriter, req *http.Request) {
	wa, err := webAuthnFor(req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Second facteur : le mot de passe a deja ete valide, on cible ce compte.
	if pending := req.URL.Query().Get("pending"); pending != "" {
		c, found := peekCeremony(pending, "2fa")
		if !found {
			writeErr(w, http.StatusBadRequest, "Session de connexion expiree")
			return
		}
		user, ok := postsql.AdminGetUserByID(c.userID)
		if !ok {
			writeErr(w, http.StatusBadRequest, "Compte introuvable")
			return
		}
		options, sessionData, err := wa.BeginLogin(postsql.NewWebAuthnUser(user))
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Ceremonie impossible : "+err.Error())
			return
		}
		key, err := putCeremony(&ceremony{data: sessionData, userID: user.ID, purpose: "login"})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Ceremonie impossible")
			return
		}
		// La ceremonie 2fa reste ouverte jusqu'a la validation de la passkey.
		writeOK(w, map[string]interface{}{"ceremony": key, "pending": pending, "options": options.Response})
		return
	}

	// Connexion sans identifiant : la passkey elle-meme designe le compte.
	options, sessionData, err := wa.BeginDiscoverableLogin()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Ceremonie impossible : "+err.Error())
		return
	}
	key, err := putCeremony(&ceremony{data: sessionData, purpose: "login"})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Ceremonie impossible")
		return
	}
	writeOK(w, map[string]interface{}{"ceremony": key, "options": options.Response})
}

func HandleWebAuthnLoginFinish(w http.ResponseWriter, req *http.Request) {
	key := req.URL.Query().Get("ceremony")
	pending := req.URL.Query().Get("pending")

	c, found := takeCeremony(key, "login")
	if !found {
		writeErr(w, http.StatusBadRequest, "Ceremonie expiree, recommencez")
		return
	}

	wa, err := webAuthnFor(req)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var (
		user *postsql.AdminUser
		cred *webauthn.Credential
	)

	if c.userID != 0 {
		// Second facteur : le compte est deja connu.
		u, ok := postsql.AdminGetUserByID(c.userID)
		if !ok {
			writeErr(w, http.StatusBadRequest, "Compte introuvable")
			return
		}
		cred, err = wa.FinishLogin(postsql.NewWebAuthnUser(u), *c.data, req)
		user = u
	} else {
		handler := func(rawID, userHandle []byte) (webauthn.User, error) {
			u, ok := postsql.UserByWebAuthnHandle(userHandle)
			if !ok {
				return nil, fmt.Errorf("compte inconnu")
			}
			user = u
			return postsql.NewWebAuthnUser(u), nil
		}
		cred, err = wa.FinishDiscoverableLogin(handler, *c.data, req)
	}

	if err != nil || user == nil {
		auditAnon(req, postsql.LevelWarn, postsql.CatAuth, "passkey-refusee", "", "")
		writeErr(w, http.StatusUnauthorized, "Passkey refusee")
		return
	}

	// La politique du compte doit autoriser la passkey seule.
	if pending == "" && user.AuthPolicy == postsql.PolicyPassword {
		writeErr(w, http.StatusForbidden, "Ce compte exige le mot de passe")
		return
	}
	if pending != "" {
		if _, ok := takeCeremony(pending, "2fa"); !ok {
			writeErr(w, http.StatusBadRequest, "Session de connexion expiree")
			return
		}
	}

	postsql.TouchCredential(cred)

	token, err := postsql.CreateSession(user.ID, clientIP(req), req.UserAgent())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Ouverture de session impossible")
		return
	}
	setSessionCookie(w, req, token)
	methode := "passkey"
	if pending != "" {
		methode = "mot de passe + passkey"
	}
	audit(req, user, postsql.LevelInfo, postsql.CatAuth, "connexion", user.Username, methode)
	writeOK(w, map[string]interface{}{"status": "LOGGED_IN", "user": user})
}
