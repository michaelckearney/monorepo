package dev

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

const vaultSecret = "JBSWY3DPEHPK3PXP"

// code is what an authenticator app shows for secret now.
func code(secret string, at time.Time) string {
	key, _ := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	mac := hmac.New(sha1.New, key)
	binary.Write(mac, binary.BigEndian, uint64(at.Unix()/30))
	sum := mac.Sum(nil)
	o := sum[len(sum)-1] & 0xf
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[o:o+4])&0x7fffffff)%1_000_000)
}

// vault is a website you sign in to as a bank has you: your username on one
// page, your password on the next, and an authenticator code on the last.
// The user is alex, the password letmein. step is the last page reached.
type vault struct {
	*httptest.Server
	mu   sync.Mutex
	step string
}

func (v *vault) reached() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.step
}

func newVault(t *testing.T) *vault {
	v := &vault{}
	mux := http.NewServeMux()
	page := func(step, field, kind string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			v.mu.Lock()
			v.step = step
			v.mu.Unlock()
			fmt.Fprintf(w, `<!doctype html><form method="post"><input name=%q type=%q autofocus><button>Go</button></form>`, field, kind)
		}
	}
	mux.HandleFunc("GET /login", page("login", "username", "text"))
	mux.HandleFunc("POST /login", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("username") != "alex" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/password", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /password", page("password", "password", "password"))
	mux.HandleFunc("POST /password", func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("password") != "letmein" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/code", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /code", page("code", "code", "text"))
	mux.HandleFunc("POST /code", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		if c := r.FormValue("code"); c != code(vaultSecret, now) && c != code(vaultSecret, now.Add(-30*time.Second)) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "yes", Path: "/"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<!doctype html><p>Vault") })
	v.Server = httptest.NewServer(mux)
	t.Cleanup(v.Close)
	return v
}

const vaultService = `import { defineService, fill } from "harbor"

const field = (name: string) => document.querySelector<HTMLInputElement>('input[name="' + name + '"]')

export default defineService({
  name: "vault",
  title: "Vault",
  url: "URL",
  login: {
    url: "/login",
    check: async ({ cookies }) => cookies.some((c) => c.name === "session"),
    run: async () => {
      for (const what of ["username", "password", "code"] as const) {
        const f = field(what)
        if (f) {
          await fill(f, what)
          f.form!.requestSubmit()
          return
        }
      }
    },
  },
  tools: {
    peek: {
      description: "Try to fill in a password outside a sign-in.",
      async run() {
        document.body.innerHTML = "<input>"
        await fill(document.querySelector("input")!, "password")
        return {}
      },
    },
  },
})
`

// typeIn types text into the focused field of the live view, then Enter.
func typeIn(t *testing.T, e *engine.Engine, name, text string) {
	t.Helper()
	typeText(t, e, name, text)
	for _, typ := range []string{"keyDown", "keyUp"} {
		ev := browser.InputEvent{Kind: "key", Type: typ, Key: "Enter", Code: "Enter", KeyCode: 13}
		if typ == "keyDown" {
			ev.Text = "\r"
		}
		if err := input(e, name, ev); err != nil {
			t.Fatal(err)
		}
	}
}

func (v *vault) waitFor(t *testing.T, step string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); v.reached() != step; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the sign-in never reached the %s page; it is on %s", step, v.reached())
		}
	}
	// The page has loaded; give it a moment to focus its field.
	time.Sleep(300 * time.Millisecond)
}

func vaultEngine(t *testing.T, secrets *memorySecrets) *engine.Engine {
	e := engine.New(t.TempDir(), secrets, noWindow{}, &memoryFiles{})
	t.Cleanup(e.Close)
	return e
}

func TestSignsInWithASavedLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("on its own across three pages", func(t *testing.T) {
		v := newVault(t)
		secrets := &memorySecrets{}
		secrets.SaveLogin(v.URL, engine.Login{Username: "alex", Password: "letmein", TOTPSecret: vaultSecret})
		e := vaultEngine(t, secrets)
		svc := load(t, vaultService, v.URL)
		if ok, err := e.SignIn(ctx, svc, ""); err != nil || !ok {
			t.Fatalf("SignIn = %v, %v", ok, err)
		}
	})

	t.Run("with you when nothing is saved and then offers to save it", func(t *testing.T) {
		v := newVault(t)
		secrets := &memorySecrets{}
		e := vaultEngine(t, secrets)
		svc := load(t, vaultService, v.URL)
		done := startSignIn(e, svc)
		v.waitFor(t, "login")
		waitView(t, e, "vault")
		typeIn(t, e, "vault", "alex")
		v.waitFor(t, "password")
		typeIn(t, e, "vault", "letmein")
		v.waitFor(t, "code")
		typeIn(t, e, "vault", code(vaultSecret, time.Now()))
		p := promptFor(t, e, "vault", "save-login")
		if p.Account != "alex" || p.Site != strings.TrimPrefix(v.URL, "http://") {
			t.Errorf("the prompt = %+v", p)
		}
		if err := e.Answer("vault", engine.Answer{Prompt: p.ID}); err != nil {
			t.Fatal(err)
		}
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
		if l, ok, _ := secrets.Login(v.URL); !ok || l.Username != "alex" || l.Password != "letmein" {
			t.Errorf("saved %+v, %v", l, ok)
		}
	})

	t.Run("with you for the code when no secret is saved", func(t *testing.T) {
		v := newVault(t)
		secrets := &memorySecrets{}
		secrets.SaveLogin(v.URL, engine.Login{Username: "alex", Password: "letmein"})
		e := vaultEngine(t, secrets)
		svc := load(t, vaultService, v.URL)
		done := startSignIn(e, svc)
		v.waitFor(t, "code")
		waitView(t, e, "vault")
		typeIn(t, e, "vault", code(vaultSecret, time.Now()))
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("not for a tool's run", func(t *testing.T) {
		v := newVault(t)
		secrets := &memorySecrets{}
		secrets.SaveLogin(v.URL, engine.Login{Username: "alex", Password: "letmein", TOTPSecret: vaultSecret})
		e := vaultEngine(t, secrets)
		svc := load(t, vaultService, v.URL)
		_, err := e.Call(ctx, svc, "peek", nil)
		if err == nil || !strings.Contains(err.Error(), "only while it signs in") {
			t.Errorf("fill in a tool's run: %v", err)
		}
	})
}

// waitView waits for the live view of name to show.
func waitView(t *testing.T, e *engine.Engine, name string) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if _, _, err := e.Shown(name); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no live view of %s", name)
		}
	}
}
