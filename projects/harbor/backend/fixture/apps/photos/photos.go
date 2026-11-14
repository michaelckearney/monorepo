// Package photos is Shoebox, a photo store.
//
// Scenarios: downloads (D18) through pre-signed URLs on another host (V12),
// and uploads (D19) and share links that must be signed with a key from
// the session (V11); a JWT in a cookie (S3); passkeys (A8), added after a
// first sign-in with the password.
package photos

import (
	"bytes"
	"embed"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data static
var files embed.FS

const (
	sessionFor = time.Hour
	urlFor     = 5 * time.Minute
	skew       = 5 * time.Minute
	maxUpload  = 10 << 20
)

type Album struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Photo struct {
	ID     string `json:"id"`
	Album  string `json:"album"`
	Name   string `json:"name"`
	Taken  string `json:"taken"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Color  string `json:"color,omitempty"`
	Type   string `json:"contentType"`
	Size   int    `json:"size"`
	bytes  []byte
}

type data struct {
	User struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
	Albums []Album  `json:"albums"`
	Photos []*Photo `json:"photos"`
}

// user is the one account, as WebAuthn sees it.
type user struct {
	email, name string
	creds       []webauthn.Credential
}

func (u *user) WebAuthnID() []byte                         { return []byte("shoebox-user-1") }
func (u *user) WebAuthnName() string                       { return u.email }
func (u *user) WebAuthnDisplayName() string                { return u.name }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

type claims struct {
	site.Claims
	Email string `json:"email"`
}

type photos struct {
	key      []byte // signs session JWTs, and makes each session's signing key
	fileKey  []byte // signs download URLs
	webauthn *webauthn.WebAuthn

	// passkeys are user's, by ID, as the store keeps them: a passkey that
	// Harbor saved works after a restart.
	passkeys *site.Table[webauthn.Credential]

	mu         sync.Mutex
	data       data
	user       *user
	ceremonies map[string]*webauthn.SessionData // WebAuthn challenges in progress
	shares     map[string]string                // share token: photo id
}

func New(store *site.Store) (app, files_ http.Handler) {
	p := &photos{key: store.Key("shoebox sessions"), fileKey: store.Key("shoebox download links"),
		passkeys:   site.NewTable[webauthn.Credential](store, "shoebox passkeys"),
		ceremonies: map[string]*webauthn.SessionData{}, shares: map[string]string{}}
	site.Load(files, "data/photos.json", &p.data)
	p.user = &user{email: p.data.User.Email, name: p.data.User.Name}
	p.passkeys.Each(func(_ string, c webauthn.Credential) { p.user.creds = append(p.user.creds, c) })
	for _, ph := range p.data.Photos {
		ph.bytes = picture(ph)
		ph.Type, ph.Size = "image/svg+xml", len(ph.bytes)
	}
	var err error
	p.webauthn, err = webauthn.New(&webauthn.Config{
		RPDisplayName: "Shoebox", RPID: hosts.Photos, RPOrigins: []string{hosts.URL(hosts.Photos)},
	})
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := files.ReadFile("static/" + name)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
		}
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if p.session(r) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		page("index.html")(w, r)
	})
	mux.HandleFunc("GET /login", page("login.html"))
	mux.Handle("GET /static/", http.FileServerFS(files))
	mux.HandleFunc("GET /s/{token}", p.shared)

	mux.HandleFunc("POST /api/login/password", p.passwordLogin)
	mux.HandleFunc("POST /api/passkeys/login/begin", p.passkeyLoginBegin)
	mux.HandleFunc("POST /api/passkeys/login/finish", p.passkeyLoginFinish)
	mux.HandleFunc("POST /api/passkeys/register/begin", p.auth(p.registerBegin))
	mux.HandleFunc("POST /api/passkeys/register/finish", p.auth(p.registerFinish))
	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sb_token", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true})
		site.JSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/session", p.auth(p.sessionInfo))
	mux.HandleFunc("GET /api/albums", p.auth(p.albums))
	mux.HandleFunc("GET /api/albums/{id}", p.auth(p.album))
	mux.HandleFunc("GET /api/photos/{id}/download", p.auth(p.download))
	mux.HandleFunc("POST /api/albums/{id}/photos", p.auth(p.signed(p.upload)))
	mux.HandleFunc("POST /api/photos/{id}/share", p.auth(p.signed(p.share)))

	f := http.NewServeMux()
	f.HandleFunc("GET /o/{id}", p.object)
	return mux, f
}

// ---- Sessions: a JWT in a cookie

func (p *photos) startSession(w http.ResponseWriter) {
	now := time.Now()
	token := site.SignJWT(p.key, claims{Claims: site.Claims{Subject: "shoebox-user-1", ID: site.Token(), Issued: now.Unix(), Expires: now.Add(sessionFor).Unix()}, Email: p.user.email})
	http.SetCookie(w, &http.Cookie{Name: "sb_token", Value: token, Path: "/", MaxAge: int(sessionFor.Seconds()), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (p *photos) session(r *http.Request) *claims {
	c, err := r.Cookie("sb_token")
	if err != nil {
		return nil
	}
	var cl claims
	if site.ParseJWT(p.key, c.Value, &cl) != nil {
		return nil
	}
	return &cl
}

// signingKey is a session's key for signing requests: the page gets it from
// /api/session.
func (p *photos) signingKey(c *claims) []byte { return []byte(site.HMAC(p.key, "signing:"+c.ID)) }

type handler func(w http.ResponseWriter, r *http.Request, c *claims)

func (p *photos) auth(next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := p.session(r)
		if c == nil {
			site.Fail(w, http.StatusUnauthorized, "unauthenticated", "Sign in at "+hosts.URL(hosts.Photos)+"/login.")
			return
		}
		next(w, r, c)
	}
}

// signed checks a request's signature: X-Shoebox-Signature is
// hex(HMAC-SHA256(signing key, METHOD + "\n" + path + "\n" + X-Shoebox-Date)),
// and the date is within 5 minutes of now.
func (p *photos) signed(next handler) handler {
	return func(w http.ResponseWriter, r *http.Request, c *claims) {
		date := r.Header.Get("X-Shoebox-Date")
		t, err := time.Parse(time.RFC3339, date)
		if err != nil {
			site.Fail(w, http.StatusUnauthorized, "missing_signature", "Sign this request: send X-Shoebox-Date (RFC 3339) and X-Shoebox-Signature.")
			return
		}
		if d := time.Since(t); d > skew || d < -skew {
			site.Fail(w, http.StatusUnauthorized, "signature_expired", "X-Shoebox-Date is more than 5 minutes from now.")
			return
		}
		want := site.HMAC(p.signingKey(c), r.Method+"\n"+r.URL.Path+"\n"+date)
		if r.Header.Get("X-Shoebox-Signature") != want {
			site.Fail(w, http.StatusUnauthorized, "bad_signature", "X-Shoebox-Signature is not right for this request.")
			return
		}
		next(w, r, c)
	}
}

func (p *photos) sessionInfo(w http.ResponseWriter, r *http.Request, c *claims) {
	p.mu.Lock()
	n := len(p.user.creds)
	p.mu.Unlock()
	site.JSON(w, 200, map[string]any{"email": p.user.email, "name": p.user.name, "passkeys": n,
		"signingKey": string(p.signingKey(c)), "expires": time.Unix(c.Expires, 0).UTC().Format(time.RFC3339)})
}

// ---- Signing in

func (p *photos) passwordLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(in.Email), p.user.email) || in.Password != site.Password {
		site.Fail(w, http.StatusUnauthorized, "invalid_credentials", "That email and password don't match.")
		return
	}
	p.startSession(w)
	p.mu.Lock()
	n := len(p.user.creds)
	p.mu.Unlock()
	site.JSON(w, 200, map[string]any{"ok": true, "passkeys": n})
}

// ceremony keeps a WebAuthn challenge until it is answered, by a cookie.
func (p *photos) ceremony(w http.ResponseWriter, sd *webauthn.SessionData) {
	id := site.Token()
	p.mu.Lock()
	p.ceremonies[id] = sd
	p.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "sb_webauthn", Value: id, Path: "/api/passkeys", MaxAge: 300, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

func (p *photos) answered(r *http.Request) *webauthn.SessionData {
	c, err := r.Cookie("sb_webauthn")
	if err != nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	sd := p.ceremonies[c.Value]
	delete(p.ceremonies, c.Value)
	return sd
}

func (p *photos) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	options, sd, err := p.webauthn.BeginDiscoverableLogin()
	if err != nil {
		site.Fail(w, 500, "webauthn", err.Error())
		return
	}
	p.ceremony(w, sd)
	site.JSON(w, 200, options)
}

func (p *photos) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	sd := p.answered(r)
	if sd == nil {
		site.Fail(w, 400, "no_challenge", "Start again: the passkey challenge expired.")
		return
	}
	p.mu.Lock()
	_, cred, err := p.webauthn.FinishPasskeyLogin(func(_, handle []byte) (webauthn.User, error) {
		if !bytes.Equal(handle, p.user.WebAuthnID()) {
			return nil, fmt.Errorf("unknown user")
		}
		return p.user, nil
	}, *sd, r)
	if err == nil {
		for i := range p.user.creds {
			if bytes.Equal(p.user.creds[i].ID, cred.ID) {
				p.user.creds[i].Authenticator.SignCount = cred.Authenticator.SignCount
				p.passkeys.Put(base64.RawURLEncoding.EncodeToString(cred.ID), p.user.creds[i])
			}
		}
	}
	p.mu.Unlock()
	if err != nil {
		site.Fail(w, http.StatusUnauthorized, "passkey_failed", "That passkey did not work: "+err.Error())
		return
	}
	p.startSession(w)
	site.JSON(w, 200, map[string]bool{"ok": true})
}

func (p *photos) registerBegin(w http.ResponseWriter, r *http.Request, c *claims) {
	p.mu.Lock()
	options, sd, err := p.webauthn.BeginRegistration(p.user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(webauthn.Credentials(p.user.creds).CredentialDescriptors()))
	p.mu.Unlock()
	if err != nil {
		site.Fail(w, 500, "webauthn", err.Error())
		return
	}
	p.ceremony(w, sd)
	site.JSON(w, 200, options)
}

func (p *photos) registerFinish(w http.ResponseWriter, r *http.Request, c *claims) {
	sd := p.answered(r)
	if sd == nil {
		site.Fail(w, 400, "no_challenge", "Start again: the passkey challenge expired.")
		return
	}
	p.mu.Lock()
	cred, err := p.webauthn.FinishRegistration(p.user, *sd, r)
	if err == nil {
		p.user.creds = append(p.user.creds, *cred)
		p.passkeys.Put(base64.RawURLEncoding.EncodeToString(cred.ID), *cred)
	}
	p.mu.Unlock()
	if err != nil {
		site.Fail(w, 400, "passkey_failed", "The passkey was not saved: "+err.Error())
		return
	}
	site.JSON(w, 200, map[string]bool{"ok": true})
}

// ---- Photos

func (p *photos) albums(w http.ResponseWriter, r *http.Request, _ *claims) {
	p.mu.Lock()
	defer p.mu.Unlock()
	type withCount struct {
		Album
		Count int    `json:"count"`
		Cover string `json:"coverPhoto"`
	}
	out := []withCount{}
	for _, a := range p.data.Albums {
		wc := withCount{Album: a}
		for _, ph := range p.data.Photos {
			if ph.Album == a.ID {
				wc.Count++
				if wc.Cover == "" {
					wc.Cover = ph.ID
				}
			}
		}
		out = append(out, wc)
	}
	site.JSON(w, 200, map[string]any{"albums": out})
}

func (p *photos) album(w http.ResponseWriter, r *http.Request, _ *claims) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i := slices.IndexFunc(p.data.Albums, func(a Album) bool { return a.ID == r.PathValue("id") })
	if i < 0 {
		site.Fail(w, 404, "not_found", "There is no album "+r.PathValue("id")+".")
		return
	}
	list := []Photo{}
	for _, ph := range p.data.Photos {
		if ph.Album == r.PathValue("id") {
			list = append(list, *ph)
		}
	}
	site.JSON(w, 200, map[string]any{"album": p.data.Albums[i], "photos": list})
}

func (p *photos) photo(id string) *Photo {
	for _, ph := range p.data.Photos {
		if ph.ID == id {
			return ph
		}
	}
	return nil
}

// download returns a URL for a photo's bytes on files.shoebox.localhost,
// signed and good for 5 minutes. That host needs no cookie.
func (p *photos) download(w http.ResponseWriter, r *http.Request, _ *claims) {
	p.mu.Lock()
	ph := p.photo(r.PathValue("id"))
	p.mu.Unlock()
	if ph == nil {
		site.Fail(w, 404, "not_found", "There is no photo "+r.PathValue("id")+".")
		return
	}
	expires := time.Now().Add(urlFor).Unix()
	sig := site.HMAC(p.fileKey, ph.ID+"\n"+strconv.FormatInt(expires, 10))
	site.JSON(w, 200, map[string]any{"url": fmt.Sprintf("%s/o/%s?expires=%d&signature=%s", hosts.URL(hosts.PhotoFiles), ph.ID, expires, sig),
		"expires": time.Unix(expires, 0).UTC().Format(time.RFC3339), "name": ph.Name, "contentType": ph.Type})
}

func (p *photos) object(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	id, expires := r.PathValue("id"), r.URL.Query().Get("expires")
	n, err := strconv.ParseInt(expires, 10, 64)
	if err != nil || r.URL.Query().Get("signature") != site.HMAC(p.fileKey, id+"\n"+expires) {
		http.Error(w, "SignatureDoesNotMatch", http.StatusForbidden)
		return
	}
	if time.Now().Unix() > n {
		http.Error(w, "Request has expired", http.StatusForbidden)
		return
	}
	p.mu.Lock()
	ph := p.photo(id)
	p.mu.Unlock()
	if ph == nil {
		http.Error(w, "NoSuchKey", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", ph.Type)
	w.Header().Set("Content-Disposition", `inline; filename="`+ph.Name+`"`)
	w.Write(ph.bytes)
}

func (p *photos) upload(w http.ResponseWriter, r *http.Request, _ *claims) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	f, header, err := r.FormFile("file")
	if err != nil {
		site.Fail(w, 400, "bad_upload", "Send multipart/form-data with the photo in a field named file.")
		return
	}
	defer f.Close()
	body, err := io.ReadAll(f)
	if err != nil {
		site.Fail(w, 400, "bad_upload", err.Error())
		return
	}
	kind := http.DetectContentType(body)
	if strings.HasSuffix(strings.ToLower(header.Filename), ".svg") {
		kind = "image/svg+xml"
	}
	if !strings.HasPrefix(kind, "image/") {
		site.Fail(w, 415, "not_an_image", "Shoebox keeps images only; that file is "+kind+".")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !slices.ContainsFunc(p.data.Albums, func(a Album) bool { return a.ID == r.PathValue("id") }) {
		site.Fail(w, 404, "not_found", "There is no album "+r.PathValue("id")+".")
		return
	}
	ph := &Photo{ID: fmt.Sprintf("ph-%03d", len(p.data.Photos)+1), Album: r.PathValue("id"), Name: header.Filename,
		Taken: time.Now().UTC().Format(time.RFC3339), Type: kind, Size: len(body), bytes: body}
	p.data.Photos = append(p.data.Photos, ph)
	site.JSON(w, 201, ph)
}

func (p *photos) share(w http.ResponseWriter, r *http.Request, _ *claims) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.photo(r.PathValue("id")) == nil {
		site.Fail(w, 404, "not_found", "There is no photo "+r.PathValue("id")+".")
		return
	}
	token := site.Token()[:16]
	p.shares[token] = r.PathValue("id")
	site.JSON(w, 201, map[string]string{"url": hosts.URL(hosts.Photos) + "/s/" + token})
}

// shared is a share link's page: anyone with the link sees the photo.
func (p *photos) shared(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	ph := p.photo(p.shares[r.PathValue("token")])
	p.mu.Unlock()
	if ph == nil {
		http.Error(w, "This link does not work any more.", http.StatusNotFound)
		return
	}
	expires := time.Now().Add(urlFor).Unix()
	src := fmt.Sprintf("%s/o/%s?expires=%d&signature=%s", hosts.URL(hosts.PhotoFiles), ph.ID, expires, site.HMAC(p.fileKey, ph.ID+"\n"+strconv.FormatInt(expires, 10)))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><title>%s · Shoebox</title><body style="font-family: system-ui; text-align: center"><h1>%s</h1><img src="%s" alt="%s" style="max-width: 90vw"></body>`,
		ph.Name, ph.Name, src, ph.Name)
}

// picture draws a seed photo: a coloured card with its name.
func picture(ph *Photo) []byte {
	return []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 400 300">`+
		`<rect width="400" height="300" fill="%s"/><circle cx="310" cy="80" r="40" fill="#fff" opacity=".35"/>`+
		`<path d="M0 230 L120 150 L210 220 L290 170 L400 240 V300 H0Z" fill="#000" opacity=".2"/>`+
		`<text x="20" y="40" font-family="sans-serif" font-size="22" fill="#fff">%s</text>`+
		`<text x="20" y="66" font-family="sans-serif" font-size="14" fill="#fff" opacity=".8">%s</text></svg>`,
		ph.Width, ph.Height, ph.Color, ph.Name, ph.Taken[:10]))
}
