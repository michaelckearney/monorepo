package fixture

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/mail"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// every is a browser for every site, whose servers reach each other as the
// fixtures' do.
func every(t *testing.T) *fake.Browser {
	b := fake.New(t, hosts.Index, http.NotFoundHandler())
	for host, h := range sites(fake.Store(t), &mail.Box{}, site.Client(b.Client.Transport.(*http.Transport)), nil) {
		b.Serve(host, h)
	}
	return b
}

// signIn fills in a portal's login page, which the browser is on.
func signIn(t *testing.T, b *fake.Browser, page fake.Page, password string) fake.Page {
	t.Helper()
	if !strings.Contains(page.Body, `name="password"`) {
		t.Fatalf("not a login page: %s %d\n%s", page.URL, page.Status, page.Body)
	}
	return b.At(page.URL.Hostname()).Post(page.Path, url.Values{
		"username": {"alex@example.localhost"}, "password": {password}, "rd": {fake.Field(page.Body, "rd")},
		"reauth": {fake.Field(page.Body, "reauth")},
	})
}

func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Northwind ID signs Payday in on its server, then Tasklane with PKCE and
// consent and no password, then asks again when Payday wants the password,
// and signing out of it signs both out.
func TestNorthwindIDSignsBothAppsInAndOut(t *testing.T) {
	b := every(t)
	payday := b.At(hosts.Payroll)

	login := payday.Get("/auth/login")
	if login.URL.Hostname() != hosts.NorthwindID {
		t.Fatalf("Payday's sign-in went to %s", login.URL)
	}
	if wrong := signIn(t, b, login, "wrong"); wrong.Status != http.StatusUnauthorized || !strings.Contains(wrong.Body, "Incorrect username or password.") {
		t.Errorf("a wrong password: %d\n%s", wrong.Status, wrong.Body)
	}
	if home := signIn(t, b, login, site.Password); home.URL.Hostname() != hosts.Payroll || home.Path != "/" {
		t.Fatalf("after signing in, the browser is at %s", home.URL)
	}
	if r := payday.Do("POST", "/api/auth/refresh", nil, nil); r.Status != 200 {
		t.Fatalf("Payday after sign-in: %d %s", r.Status, r.Body)
	}

	// Tasklane: already signed in, so only consent.
	var config struct{ AuthorizeURL string }
	json.Unmarshal([]byte(b.At(hosts.Tasks).Get("/api/auth/config").Body), &config)
	verifier := "a-verifier-long-enough-for-pkce-0123456789"
	if bare := b.Get(config.AuthorizeURL + "&state=s1"); !strings.HasPrefix(bare.Path, "/callback?error=invalid_request") {
		t.Errorf("a public client with no PKCE ended at %s", bare.Path)
	}
	consent := b.Get(config.AuthorizeURL + "&state=s1&code_challenge_method=S256&code_challenge=" + challenge(verifier))
	if consent.URL.Hostname() != hosts.NorthwindID || !strings.Contains(consent.Body, "Tasklane</strong> is asking for access") {
		t.Fatalf("Tasklane's sign-in: %s\n%s", consent.URL, consent.Body)
	}
	back := b.At(hosts.NorthwindID).Post("/consent", url.Values{"flow": {fake.Field(consent.Body, "flow")}, "decision": {"accept"}})
	q := back.URL.Query()
	if back.URL.Hostname() != hosts.Tasks || q.Get("state") != "s1" || q.Get("code") == "" {
		t.Fatalf("after consent, the browser is at %s", back.URL)
	}
	tasklane := b.At(hosts.Tasks)
	if r := tasklane.JSON("POST", "/api/auth/exchange", `{"code":"`+q.Get("code")+`","codeVerifier":"not-the-verifier"}`, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("a wrong PKCE verifier: %d %s", r.Status, r.Body)
	}
	// The code was used up by the failed try, as Authelia does.
	consent = b.Get(config.AuthorizeURL + "&state=s2&code_challenge_method=S256&code_challenge=" + challenge(verifier))
	back = b.At(hosts.NorthwindID).Post("/consent", url.Values{"flow": {fake.Field(consent.Body, "flow")}, "decision": {"accept"}})
	r := tasklane.JSON("POST", "/api/auth/exchange", `{"code":"`+back.URL.Query().Get("code")+`","codeVerifier":"`+verifier+`"}`, nil)
	var tok struct{ RefreshToken string }
	if json.Unmarshal([]byte(r.Body), &tok); r.Status != 200 || tok.RefreshToken == "" {
		t.Fatalf("Tasklane's exchange: %d %s", r.Status, r.Body)
	}

	// Payday asks for the password again before a change to bank details.
	again := payday.Get("/auth/login?reauth=1")
	if again.URL.Hostname() != hosts.NorthwindID || !strings.Contains(again.Body, "Enter your password again") {
		t.Fatalf("Payday's re-authentication: %s\n%s", again.URL, again.Body)
	}
	if home := signIn(t, b, again, site.Password); home.URL.Hostname() != hosts.Payroll {
		t.Fatalf("after the password again, the browser is at %s", home.URL)
	}

	// Signing out of Northwind ID, from Tasklane, signs Payday out too.
	out := b.Get("https://" + hosts.NorthwindID + ":" + hosts.Port + "/logout?rd=" + url.QueryEscape(hosts.URL(hosts.Tasks)+"/"))
	if out.URL.Hostname() != hosts.Tasks {
		t.Errorf("after signing out, the browser is at %s", out.URL)
	}
	if r := tasklane.JSON("POST", "/api/auth/refresh", `{"refreshToken":"`+tok.RefreshToken+`"}`, nil); r.Status != http.StatusUnauthorized || !strings.Contains(r.Body, "signed_out") {
		t.Errorf("Tasklane after signing out of Northwind ID: %d %s", r.Status, r.Body)
	}
	if r := payday.Do("POST", "/api/auth/refresh", nil, nil); r.Status != http.StatusUnauthorized {
		t.Errorf("Payday after signing out of Northwind ID: %d %s", r.Status, r.Body)
	}
	// And the portal sends nobody anywhere else.
	if away := b.Get("https://" + hosts.NorthwindID + ":" + hosts.Port + "/logout?rd=https://example.com/"); away.URL.Hostname() != hosts.NorthwindID {
		t.Errorf("logout went on to %s", away.URL)
	}
}

// Homebase's portal sends a page to sign in, answers a fetch with 401,
// and tells Homebase who signed in.
func TestHomebasePortalGuardsHomebase(t *testing.T) {
	b := every(t)
	home := b.At(hosts.Home)
	html := http.Header{"Accept": {"text/html"}}

	login := home.Do("GET", "/", nil, html)
	if login.URL.Hostname() != hosts.HomeLogin {
		t.Fatalf("Homebase signed out went to %s", login.URL)
	}
	if r := home.Get("/api/me"); r.Status != http.StatusUnauthorized {
		t.Errorf("a fetch signed out: %d %s", r.Status, r.Body)
	}
	if page := signIn(t, b, login, site.Password); page.URL.Hostname() != hosts.Home || page.Status != 200 {
		t.Fatalf("after signing in, the browser is at %s (%d)", page.URL, page.Status)
	}
	me := home.Get("/api/me")
	if me.Status != 200 || !strings.Contains(me.Body, `"username":"alex"`) || !strings.Contains(me.Body, "Alex Morgan") {
		t.Errorf("Homebase's /api/me: %d %s", me.Status, me.Body)
	}
	// A header from the browser does not say who it is.
	if r := home.Do("GET", "/api/me", nil, http.Header{"Remote-User": {"mallory"}}); strings.Contains(r.Body, "mallory") {
		t.Errorf("Homebase believed the browser's Remote-User: %s", r.Body)
	}
	out := b.Do("GET", "https://"+hosts.HomeLogin+":"+hosts.Port+"/logout?rd="+url.QueryEscape(hosts.URL(hosts.Home)+"/"), nil, html)
	if out.URL.Hostname() != hosts.HomeLogin || !strings.Contains(out.Body, `name="password"`) {
		t.Errorf("after signing out, Homebase showed %s", out.URL)
	}
}
