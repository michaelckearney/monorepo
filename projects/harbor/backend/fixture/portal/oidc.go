package portal

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// Client is an app that signs in with the portal over OIDC.
type Client struct {
	ID, Name string
	// Secret is a confidential client's, sent with HTTP Basic. A public
	// client has none, and must use PKCE with S256.
	Secret       string
	RedirectURIs []string
	// Consent asks the user each time the app signs in, as Authelia's
	// explicit consent mode does.
	Consent bool
}

// The scopes a client may ask for.
var scopes = []string{"openid", "profile", "email", "groups"}

// Authelia's defaults: a code lasts a minute, an access token an hour.
const (
	codeFor   = time.Minute
	accessFor = time.Hour
	// flowFor is how long a sign-in may take, from the app's redirect to
	// the code.
	flowFor = 10 * time.Minute
)

// flow is one authorization request, while the user signs in and
// consents.
type flow struct {
	client      Client
	redirectURI string
	state       string
	challenge   string
	scope       string
	// login is set when the app asked for the password again (prompt=login,
	// or a max_age the sign-in is older than); a sign-in from before the
	// flow then does not do.
	login     bool
	started   time.Time
	consented bool
}

// grant is what a code is for, until the app trades it for a token.
type grant struct {
	client      string
	redirectURI string
	challenge   string
	scope       string
	user        *User
	expires     time.Time
}

type access struct {
	user    *User
	scope   string
	expires time.Time
}

// OIDC makes the portal an OIDC provider for clients, with the
// authorization code flow, at Authelia's paths. It gives no ID token: the
// fixture apps ask the userinfo endpoint who signed in.
func (p *Portal) OIDC(clients ...Client) {
	for _, c := range clients {
		p.clients[c.ID] = c
	}
	p.mux.HandleFunc("GET /api/oidc/authorization", p.authorize)
	p.mux.HandleFunc("GET /consent", p.consentPage)
	p.mux.HandleFunc("POST /consent", p.consent)
	p.mux.HandleFunc("POST /api/oidc/token", p.token)
	p.mux.HandleFunc("GET /api/oidc/userinfo", p.userinfo)
	p.mux.HandleFunc("POST /api/oidc/userinfo", p.userinfo)
}

func (p *Portal) issuer() string { return hosts.URL(p.opts.Host) }

// fail shows an error on the portal's own page, for a request whose
// redirect_uri cannot be trusted with it.
func (p *Portal) fail(w http.ResponseWriter, status int, message string) {
	p.pages.Render(w, status, "error", "Sign-in failed", message)
}

// back sends the browser to the app with an OAuth error.
func back(w http.ResponseWriter, r *http.Request, f *flow, code, description string) {
	q := url.Values{"error": {code}, "error_description": {description}}
	if f.state != "" {
		q.Set("state", f.state)
	}
	http.Redirect(w, r, f.redirectURI+"?"+q.Encode(), http.StatusFound)
}

func (p *Portal) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var id string
	var f *flow
	if id = q.Get("flow"); id != "" {
		p.mu.Lock()
		f = p.flows[id]
		p.mu.Unlock()
		if f == nil || time.Since(f.started) > flowFor {
			p.fail(w, http.StatusBadRequest, "This sign-in took too long, or did not start here. Go back to the app and sign in again.")
			return
		}
	} else {
		var ok bool
		if f, ok = p.newFlow(w, r, q); !ok {
			return
		}
		id = site.Token()
		p.mu.Lock()
		p.flows[id] = f
		p.mu.Unlock()
	}
	resume := "/api/oidc/authorization?flow=" + id
	s := p.session(r)
	if s == nil || f.login && s.at.Before(f.started) {
		http.Redirect(w, r, p.LoginURL(resume, f.login), http.StatusFound)
		return
	}
	p.mu.Lock()
	consented := f.consented
	p.mu.Unlock()
	if f.client.Consent && !consented {
		http.Redirect(w, r, "/consent?flow="+id, http.StatusFound)
		return
	}
	code := site.Token()
	p.mu.Lock()
	delete(p.flows, id)
	p.codes[code] = &grant{client: f.client.ID, redirectURI: f.redirectURI, challenge: f.challenge, scope: f.scope, user: s.user, expires: time.Now().Add(codeFor)}
	p.mu.Unlock()
	out := url.Values{"code": {code}, "iss": {p.issuer()}}
	if f.state != "" {
		out.Set("state", f.state)
	}
	http.Redirect(w, r, f.redirectURI+"?"+out.Encode(), http.StatusFound)
}

// newFlow checks an app's authorization request. An unknown client or
// redirect_uri is shown here; anything else goes back to the app.
func (p *Portal) newFlow(w http.ResponseWriter, r *http.Request, q url.Values) (*flow, bool) {
	c, ok := p.clients[q.Get("client_id")]
	if !ok {
		p.fail(w, http.StatusBadRequest, "No app is registered as "+strconv.Quote(q.Get("client_id"))+".")
		return nil, false
	}
	if !slices.Contains(c.RedirectURIs, q.Get("redirect_uri")) {
		p.fail(w, http.StatusBadRequest, c.Name+" asked to return to "+q.Get("redirect_uri")+", which it has not registered.")
		return nil, false
	}
	f := &flow{client: c, redirectURI: q.Get("redirect_uri"), state: q.Get("state"), scope: q.Get("scope"), started: time.Now()}
	if q.Get("response_type") != "code" {
		back(w, r, f, "unsupported_response_type", "The authorization server does not support obtaining a token using this method.")
		return nil, false
	}
	asked := strings.Fields(f.scope)
	if !slices.Contains(asked, "openid") || slices.ContainsFunc(asked, func(s string) bool { return !slices.Contains(scopes, s) }) {
		back(w, r, f, "invalid_scope", "The requested scope is invalid, unknown, or malformed.")
		return nil, false
	}
	if challenge, method := q.Get("code_challenge"), q.Get("code_challenge_method"); challenge != "" || c.Secret == "" {
		if challenge == "" || method != "S256" {
			back(w, r, f, "invalid_request", "Clients must include a code_challenge with the S256 method.")
			return nil, false
		}
		f.challenge = challenge
	}
	maxAge, err := strconv.Atoi(q.Get("max_age"))
	f.login = q.Get("prompt") == "login" || err == nil && maxAge == 0
	if s := p.session(r); err == nil && maxAge > 0 && s != nil && time.Since(s.at) > time.Duration(maxAge)*time.Second {
		f.login = true
	}
	return f, true
}

// flowOf is the consent request's flow, if the browser is signed in for it.
func (p *Portal) flowOf(w http.ResponseWriter, r *http.Request) (string, *flow, *signIn) {
	id := r.FormValue("flow")
	p.mu.Lock()
	f := p.flows[id]
	p.mu.Unlock()
	s := p.session(r)
	if f == nil || s == nil || time.Since(f.started) > flowFor {
		p.fail(w, http.StatusBadRequest, "This sign-in took too long, or did not start here. Go back to the app and sign in again.")
		return "", nil, nil
	}
	return id, f, s
}

type consentView struct {
	Flow   string
	Client string
	Scopes []string
	User   *User
}

func (p *Portal) consentPage(w http.ResponseWriter, r *http.Request) {
	id, f, s := p.flowOf(w, r)
	if f == nil {
		return
	}
	p.pages.Render(w, 200, "consent", "Consent request", consentView{Flow: id, Client: f.client.Name, Scopes: strings.Fields(f.scope), User: s.user})
}

func (p *Portal) consent(w http.ResponseWriter, r *http.Request) {
	id, f, _ := p.flowOf(w, r)
	if f == nil {
		return
	}
	if r.FormValue("decision") != "accept" {
		p.mu.Lock()
		delete(p.flows, id)
		p.mu.Unlock()
		back(w, r, f, "access_denied", "The resource owner or authorization server denied the request.")
		return
	}
	p.mu.Lock()
	f.consented = true
	p.mu.Unlock()
	http.Redirect(w, r, "/api/oidc/authorization?flow="+id, http.StatusSeeOther)
}

// tokenError is an OAuth error from the token endpoint.
func tokenError(w http.ResponseWriter, status int, code, description string) {
	site.JSON(w, status, map[string]string{"error": code, "error_description": description})
}

func (p *Portal) token(w http.ResponseWriter, r *http.Request) {
	id, secret, basic := r.BasicAuth()
	if basic {
		// client_secret_basic form-encodes both before Basic does.
		id, _ = url.QueryUnescape(id)
		secret, _ = url.QueryUnescape(secret)
	} else {
		id = r.FormValue("client_id")
	}
	c, ok := p.clients[id]
	if !ok || c.Secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(c.Secret)) != 1 || c.Secret == "" && basic {
		tokenError(w, http.StatusUnauthorized, "invalid_client", "Client authentication failed.")
		return
	}
	if r.FormValue("grant_type") != "authorization_code" {
		tokenError(w, http.StatusBadRequest, "unsupported_grant_type", "The authorization grant type is not supported by the authorization server.")
		return
	}
	code := r.FormValue("code")
	p.mu.Lock()
	g := p.codes[code]
	delete(p.codes, code)
	p.mu.Unlock()
	invalid := "The provided authorization grant is invalid, expired, revoked, or was issued to another client."
	if g == nil || time.Now().After(g.expires) || g.client != c.ID || g.redirectURI != r.FormValue("redirect_uri") {
		tokenError(w, http.StatusBadRequest, "invalid_grant", invalid)
		return
	}
	if g.challenge != "" {
		sum := sha256.Sum256([]byte(r.FormValue("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
			tokenError(w, http.StatusBadRequest, "invalid_grant", invalid+" The PKCE code verifier does not match the code challenge.")
			return
		}
	}
	token := "nwid_at_" + site.Token()
	p.mu.Lock()
	p.tokens[token] = &access{user: g.user, scope: g.scope, expires: time.Now().Add(accessFor)}
	p.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	site.JSON(w, 200, map[string]any{"access_token": token, "token_type": "bearer", "expires_in": int(accessFor.Seconds()) - 1, "scope": g.scope})
}

func (p *Portal) userinfo(w http.ResponseWriter, r *http.Request) {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	p.mu.Lock()
	a := p.tokens[token]
	p.mu.Unlock()
	if a == nil || time.Now().After(a.expires) {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token", error_description="The access token is not valid."`)
		tokenError(w, http.StatusUnauthorized, "invalid_token", "The access token is not valid.")
		return
	}
	claims := map[string]any{"sub": subject(a.user)}
	asked := strings.Fields(a.scope)
	if slices.Contains(asked, "profile") {
		claims["preferred_username"], claims["name"] = a.user.Username, a.user.Name
	}
	if slices.Contains(asked, "email") {
		claims["email"], claims["email_verified"] = a.user.Email, true
	}
	if slices.Contains(asked, "groups") {
		claims["groups"] = a.user.Groups
	}
	site.JSON(w, 200, claims)
}

// subject is the user's opaque, lasting ID, a UUID as Authelia's are.
func subject(u *User) string {
	b := sha256.Sum256([]byte("subject:" + u.Username))
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
