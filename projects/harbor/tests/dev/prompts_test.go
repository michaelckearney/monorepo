package dev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/framework/browser/securitykey"
	"github.com/michaelckearney/monorepo/framework/browser/securitykey/softkey"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

// promptFor waits for the live view of a sign-in to name to show a prompt
// of kind, and returns it.
func promptFor(t *testing.T, e *engine.Engine, name, kind string) engine.Prompt {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if _, p, err := e.Shown(name); err == nil && p != nil && p.Kind == kind {
			return *p
		}
	}
	t.Fatalf("the sign-in to %s never asked for a %s", name, kind)
	return engine.Prompt{}
}

// showing waits for the live view of name to show the page at path, loaded.
// The view opens before the sign-in loads its page, and a click or a key
// before then is lost. Ask the page itself, not the streamed state it shows
// you: the state's URL and loading flag follow events a load reports in an
// order nothing guarantees, so an action taken on its word can land on the
// page still there, on the new one mid-load, or before its scripts exist.
// One evaluate reads where the page is and whether its load is done; an
// evaluate the page throws by navigating just means not yet.
func showing(t *testing.T, e *engine.Engine, name, path string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		if tab, err := shownTab(e, name); err == nil {
			at, err := tab.Evaluate(ctx, `() => ({ path: location.pathname, ready: document.readyState })`)
			var p struct {
				Path  string `json:"path"`
				Ready string `json:"ready"`
			}
			if err == nil && json.Unmarshal(at, &p) == nil && p.Path == path && p.Ready == "complete" {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the live view of %s never showed %s", name, path)
		}
	}
}

// clickPage clicks the top left of the page at path, once the live view of
// a sign-in shows it.
func clickPage(t *testing.T, e *engine.Engine, name, path string) {
	t.Helper()
	showing(t, e, name, path)
	for _, typ := range []string{"mousePressed", "mouseReleased"} {
		ev := browser.InputEvent{Kind: "mouse", Type: typ, X: 50, Y: 50, Button: "left", ClickCount: 1}
		if err := input(e, name, ev); err != nil {
			t.Fatal(err)
		}
	}
}

// router is a website behind an HTTP password, as a home router's admin page
// is: user alex, password letmein.
func router(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "alex" || p != "letmein" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Router"`)
			http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "<!doctype html><title>Router</title><p>Router admin</p>")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const routerService = `export default {
  name: "router",
  title: "Router",
  url: "URL/admin",
  login: {
    url: "/admin",
    check: async () => (await fetch("/admin/status")).ok,
  },
  tools: {},
}
`

func TestAnswersASitesRequestForAnHTTPPassword(t *testing.T) {
	srv := router(t)
	ctx := context.Background()
	svc := load(t, routerService, srv.URL)
	secrets := &memorySecrets{}
	e := engine.New(t.TempDir(), secrets, noWindow{}, &memoryFiles{})
	t.Cleanup(e.Close)

	t.Run("signed out, with no error, until you give the password", func(t *testing.T) {
		if ok, err := e.SignedIn(ctx, svc); err != nil || ok {
			t.Errorf("SignedIn = %v, %v; want false, nil", ok, err)
		}
	})

	t.Run("asks you in the sign-in, again when the password is wrong", func(t *testing.T) {
		// The login page does not load, so shows nothing new, until you
		// answer.
		done := startSignIn(e, svc)
		p := promptFor(t, e, "router", "password")
		if p.Site != strings.TrimPrefix(srv.URL, "http://") || p.Realm != "Router" || p.Wrong {
			t.Errorf("the prompt = %+v", p)
		}
		if err := e.Answer("router", engine.Answer{Prompt: p.ID, Username: "alex", Password: "wrong"}); err != nil {
			t.Fatal(err)
		}
		p = promptFor(t, e, "router", "password")
		if !p.Wrong {
			t.Errorf("the second prompt does not say the password was wrong: %+v", p)
		}
		if err := e.Answer("router", engine.Answer{Prompt: p.ID, Username: "alex", Password: "letmein"}); err != nil {
			t.Fatal(err)
		}
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
		if c, ok, _ := secrets.Password(srv.URL, "Router"); !ok || c.Password != "letmein" {
			t.Errorf("the password saved is %+v, %v", c, ok)
		}
	})

	t.Run("gives the saved password to a new browser", func(t *testing.T) {
		again := engine.New(t.TempDir(), secrets, noWindow{}, &memoryFiles{})
		t.Cleanup(again.Close)
		if ok, err := again.SignedIn(ctx, svc); err != nil || !ok {
			t.Errorf("SignedIn = %v, %v; want true", ok, err)
		}
	})

	t.Run("a wrong saved password is signed out and not an error", func(t *testing.T) {
		wrong := &memorySecrets{}
		wrong.SavePassword(srv.URL, "Router", browser.Credentials{Username: "alex", Password: "old"})
		again := engine.New(t.TempDir(), wrong, noWindow{}, &memoryFiles{})
		t.Cleanup(again.Close)
		if ok, err := again.SignedIn(ctx, svc); err != nil || ok {
			t.Errorf("SignedIn = %v, %v; want false, nil", ok, err)
		}
	})
}

// passkeySite is a website you sign up to and sign in to with a passkey,
// which go-webauthn checks as a real site would. Its pages are one big
// button. It is served as localhost, which browsers treat as secure, as
// passkeys need.
func passkeySite(t *testing.T) string {
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	origin := strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)
	rp, err := webauthn.New(&webauthn.Config{RPID: "localhost", RPDisplayName: "Keyed", RPOrigins: []string{origin}})
	if err != nil {
		t.Fatal(err)
	}
	u := &webUser{}
	var mu sync.Mutex
	var session *webauthn.SessionData

	button := func(script string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `<!doctype html><body style="margin:0">
				<button style="position:fixed;inset:0;width:100%%">Go</button>
				<script>document.querySelector("button").onclick = async () => {
					try { %s } catch (e) { document.title = e.name + ": " + e.message }
				}</script>`, script)
		}
	}
	mux.HandleFunc("/signup", button(`
		const o = await fetch("/register/begin").then((r) => r.json());
		const c = await navigator.credentials.create({ publicKey: PublicKeyCredential.parseCreationOptionsFromJSON(o.publicKey) });
		await fetch("/register/finish", { method: "POST", body: JSON.stringify(c.toJSON()) });`))
	mux.HandleFunc("/signin", button(`
		const o = await fetch("/login/begin").then((r) => r.json());
		const c = await navigator.credentials.get({ publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(o.publicKey) });
		await fetch("/login/finish", { method: "POST", body: JSON.stringify(c.toJSON()) });`))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<!doctype html><p>Keyed") })
	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "signedin", Value: "", Path: "/", MaxAge: -1})
	})
	reply := func(w http.ResponseWriter, v any, err error) {
		if err != nil {
			t.Errorf("%v", err)
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/register/begin", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		o, s, err := rp.BeginRegistration(u, webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired))
		session = s
		reply(w, o, err)
	})
	mux.HandleFunc("/register/finish", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		c, err := rp.FinishRegistration(u, *session, r)
		if err == nil {
			u.creds = append(u.creds, *c)
			http.SetCookie(w, &http.Cookie{Name: "signedup", Value: "yes", Path: "/"})
		}
		reply(w, map[string]bool{"ok": true}, err)
	})
	mux.HandleFunc("/login/begin", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		o, s, err := rp.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
		session = s
		reply(w, o, err)
	})
	mux.HandleFunc("/login/finish", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_, err := rp.FinishDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
			if string(userHandle) != string(u.WebAuthnID()) {
				return nil, errors.New("no such user")
			}
			return u, nil
		}, *session, r)
		if err == nil {
			http.SetCookie(w, &http.Cookie{Name: "signedin", Value: "yes", Path: "/"})
		}
		reply(w, map[string]bool{"ok": true}, err)
	})
	return origin
}

type webUser struct{ creds []webauthn.Credential }

func (u *webUser) WebAuthnID() []byte                         { return []byte("user-1") }
func (u *webUser) WebAuthnName() string                       { return "alex@example.localhost" }
func (u *webUser) WebAuthnDisplayName() string                { return "Alex" }
func (u *webUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func TestMakesAndUsesPasskeysInASignIn(t *testing.T) {
	origin := passkeySite(t)
	e := newEngine(t)
	ctx := context.Background()

	t.Run("makes one, once you allow it", func(t *testing.T) {
		svc := gatedService(t, "signedup", "/signup", origin)
		done := signIn(t, e, svc)
		clickPage(t, e, "signedup", "/signup")
		p := promptFor(t, e, "signedup", "passkey")
		if !p.Create || p.Site != "localhost" || p.Account != "alex@example.localhost" {
			t.Errorf("the prompt = %+v", p)
		}
		if err := e.Answer("signedup", engine.Answer{Prompt: p.ID}); err != nil {
			t.Fatal(err)
		}
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("signs in with it, once you pick it", func(t *testing.T) {
		svc := gatedService(t, "signedin", "/signin", origin)
		done := signIn(t, e, svc)
		clickPage(t, e, "signedin", "/signin")
		p := promptFor(t, e, "signedin", "passkey")
		if p.Create || p.Site != "localhost" || len(p.Passkeys) != 1 || p.Passkeys[0].Account != "alex@example.localhost" {
			t.Fatalf("the prompt = %+v", p)
		}
		if err := e.Answer("signedin", engine.Answer{Prompt: p.ID, Passkey: p.Passkeys[0].ID, WithoutAsking: true}); err != nil {
			t.Fatal(err)
		}
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("then on its own once you let it use it without asking", func(t *testing.T) {
		svc := load(t, `export default {
  name: "auto",
  title: "Auto",
  url: "URL",
  login: {
    url: "/signin",
    check: async ({ cookies }) => cookies.some((c) => c.name === "signedin"),
    run: async () => { document.querySelector("button")!.click() },
  },
  tools: {},
}`, origin)
		if _, err := e.Navigate(ctx, origin+"/logout"); err != nil {
			t.Fatal(err)
		}
		// A live view would wait for you; this one must not need you.
		quick, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if ok, err := e.SignIn(quick, svc, ""); err != nil || !ok {
			t.Fatalf("SignIn = %v, %v", ok, err)
		}
	})

	t.Run("gives a page none outside a sign-in", func(t *testing.T) {
		// The agent's tab is no sign-in.
		if _, err := e.Navigate(ctx, origin+"/"); err != nil {
			t.Fatal(err)
		}
		out, _, err := e.Evaluate(ctx, `async () => {
			const o = await fetch("/login/begin").then((r) => r.json());
			try { await navigator.credentials.get({ publicKey: PublicKeyCredential.parseRequestOptionsFromJSON(o.publicKey) }); return "signed in" }
			catch (e) { return e.name + ": " + e.message }
		}`, "")
		if err != nil || !strings.Contains(string(out), "NotAllowedError") {
			t.Errorf("a page's passkey request outside a sign-in: %s, %v", out, err)
		}
	})
}

func TestMakesAndUsesPasskeysOnASecurityKey(t *testing.T) {
	origin := passkeySite(t)
	e := newEngine(t)
	key := softkey.New("1234")
	var mu sync.Mutex
	plugged := false
	e.SecurityKeys = func() ([]securitykey.Device, error) {
		mu.Lock()
		defer mu.Unlock()
		if !plugged {
			return nil, nil
		}
		return []securitykey.Device{key}, nil
	}

	t.Run("makes one: plug it in, then the PIN", func(t *testing.T) {
		svc := gatedService(t, "signedup", "/signup", origin)
		done := signIn(t, e, svc)
		clickPage(t, e, "signedup", "/signup")
		p := promptFor(t, e, "signedup", "passkey")
		if err := e.Answer("signedup", engine.Answer{Prompt: p.ID, SecurityKey: true}); err != nil {
			t.Fatal(err)
		}
		if p = promptFor(t, e, "signedup", "security-key"); p.Step != "insert" {
			t.Errorf("first, the prompt = %+v", p)
		}
		mu.Lock()
		plugged = true
		mu.Unlock()
		for p.Step != "pin" {
			p = promptFor(t, e, "signedup", "security-key")
		}
		if p.Retries != 8 {
			t.Errorf("the PIN prompt = %+v", p)
		}
		if err := e.Answer("signedup", engine.Answer{Prompt: p.ID, PIN: "1234"}); err != nil {
			t.Fatal(err)
		}
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("signs in with it", func(t *testing.T) {
		svc := gatedService(t, "signedin", "/signin", origin)
		done := signIn(t, e, svc)
		clickPage(t, e, "signedin", "/signin")
		p := promptFor(t, e, "signedin", "passkey")
		if err := e.Answer("signedin", engine.Answer{Prompt: p.ID, SecurityKey: true}); err != nil {
			t.Fatal(err)
		}
		for p.Step != "pin" {
			p = promptFor(t, e, "signedin", "security-key")
		}
		if err := e.Answer("signedin", engine.Answer{Prompt: p.ID, PIN: "1234"}); err != nil {
			t.Fatal(err)
		}
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
		if key.Touched != 2 {
			t.Errorf("the key waited for %d touches; want 2", key.Touched)
		}
	})
}
