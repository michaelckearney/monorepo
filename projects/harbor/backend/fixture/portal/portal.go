// Package portal is a company's own sign-in service, as Authelia is: its
// users, a login page, and one session on the company's parent domain that
// every app under it shares. On that, it is an OIDC provider for apps that
// sign in with it (OIDC), and the check a front end makes before each
// request to an app it guards (Guard).
//
// It does what the fixtures use, as Authelia does it: the same URLs, the
// same session cookie, the same redirects, and the same timeouts, so a
// browser sees what it would of Authelia. Authelia itself has no build for
// macOS, and the fixtures run on the developer's computer.
package portal

import (
	"crypto/subtle"
	"embed"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed templates static
var files embed.FS

// User is someone who can sign in.
type User struct {
	Username string
	Name     string
	Email    string
	Groups   []string
	Password string
}

// Options say which portal it is.
type Options struct {
	Brand site.Brand
	// Host is the portal's own, such as login.home.localhost.
	Host string
	// Domain is the session cookie's, the parent of every app that shares
	// the sign-in, such as home.localhost.
	Domain string
	// Cookie is the session cookie's name.
	Cookie string
	Users  []User
	// Store keeps the sessions.
	Store *site.Store
}

// Portal is one sign-in service. It serves the portal's host.
type Portal struct {
	opts     Options
	pages    *site.Pages
	sessions *site.Sessions[kept]
	mux      *http.ServeMux

	mu    sync.Mutex
	flows map[string]*flow
	codes map[string]*grant
	// tokens are access tokens, to the user they are for.
	tokens map[string]*access
	// clients are the apps that sign in with OIDC, by ID.
	clients map[string]Client
}

// signIn is one browser's session.
type signIn struct {
	user *User
	// at is when the user last typed their password.
	at time.Time
}

// kept is a session as the store keeps it.
type kept struct {
	Username string
	At       time.Time
}

// Authelia's defaults: a session ends after 5 minutes unused, or an hour
// after it started.
const (
	idle     = 5 * time.Minute
	absolute = time.Hour
)

func New(o Options) *Portal {
	p := &Portal{
		opts:  o,
		pages: site.NewPages(o.Brand, files, nil),
		sessions: site.NewSessions[kept](o.Store, site.SessionOptions{
			Cookie: o.Cookie, Domain: o.Domain, Idle: idle, Absolute: absolute,
		}),
		mux:     http.NewServeMux(),
		flows:   map[string]*flow{},
		codes:   map[string]*grant{},
		tokens:  map[string]*access{},
		clients: map[string]Client{},
	}
	site.Files(p.mux, files, "static", "/")
	p.mux.HandleFunc("GET /{$}", p.loginPage)
	p.mux.HandleFunc("POST /{$}", p.login)
	p.mux.HandleFunc("GET /logout", p.logout)
	p.mux.HandleFunc("GET /api/state", p.state)
	return p
}

func (p *Portal) ServeHTTP(w http.ResponseWriter, r *http.Request) { p.mux.ServeHTTP(w, r) }

// LoginURL is the login page, which goes on to rd once the user signs in.
// reauth asks for the password even of a user signed in already.
func (p *Portal) LoginURL(rd string, reauth bool) string {
	q := url.Values{"rd": {rd}}
	if reauth {
		q.Set("reauth", "1")
	}
	return hosts.URL(p.opts.Host) + "/?" + q.Encode()
}

// session is the request's sign-in, or nil.
func (p *Portal) session(r *http.Request) *signIn {
	s, _ := p.sessions.Get(r)
	if s == nil {
		return nil
	}
	for i := range p.opts.Users {
		if u := &p.opts.Users[i]; u.Username == s.Data.Username {
			return &signIn{user: u, at: s.Data.At}
		}
	}
	return nil
}

// allowed reports whether rd is somewhere the portal may send a browser:
// a page of its own, or of an app under its domain. Anywhere else would
// make it an open redirect.
func (p *Portal) allowed(rd string) bool {
	if strings.HasPrefix(rd, "/") && !strings.HasPrefix(rd, "//") {
		return true
	}
	u, err := url.Parse(rd)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	return host == p.opts.Domain || strings.HasSuffix(host, "."+p.opts.Domain)
}

type loginView struct {
	RD     string
	Reauth bool
	Error  string
	User   *User
}

func (p *Portal) loginPage(w http.ResponseWriter, r *http.Request) {
	rd, reauth := r.URL.Query().Get("rd"), r.URL.Query().Get("reauth") == "1"
	if s := p.session(r); s != nil && !reauth {
		if rd != "" && p.allowed(rd) {
			http.Redirect(w, r, rd, http.StatusFound)
			return
		}
		p.pages.Render(w, 200, "signedin", "Signed in", loginView{User: s.user})
		return
	}
	p.pages.Render(w, 200, "login", "Sign in", loginView{RD: rd, Reauth: reauth})
}

func (p *Portal) login(w http.ResponseWriter, r *http.Request) {
	rd, reauth := r.FormValue("rd"), r.FormValue("reauth") == "1"
	u := p.find(r.FormValue("username"), r.FormValue("password"))
	if u == nil {
		p.pages.Render(w, http.StatusUnauthorized, "login", "Sign in", loginView{RD: rd, Reauth: reauth, Error: "Incorrect username or password."})
		return
	}
	p.sessions.Start(w, kept{Username: u.Username, At: time.Now()})
	if rd == "" || !p.allowed(rd) {
		rd = "/"
	}
	http.Redirect(w, r, rd, http.StatusSeeOther)
}

// find is the user with that username or email and password, or nil.
func (p *Portal) find(name, password string) *User {
	for i := range p.opts.Users {
		u := &p.opts.Users[i]
		if (strings.EqualFold(name, u.Username) || strings.EqualFold(name, u.Email)) &&
			subtle.ConstantTimeCompare([]byte(password), []byte(u.Password)) == 1 {
			return u
		}
	}
	return nil
}

func (p *Portal) logout(w http.ResponseWriter, r *http.Request) {
	p.sessions.End(w, r)
	if rd := r.URL.Query().Get("rd"); rd != "" && p.allowed(rd) {
		http.Redirect(w, r, rd, http.StatusFound)
		return
	}
	p.pages.Render(w, 200, "signedout", "Signed out", nil)
}

// state is Authelia's /api/state: who the browser is signed in as, and
// how (1 is a password).
func (p *Portal) state(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"username": "", "authentication_level": 0}
	if s := p.session(r); s != nil {
		data = map[string]any{"username": s.user.Username, "authentication_level": 1}
	}
	site.JSON(w, 200, map[string]any{"status": "OK", "data": data})
}
