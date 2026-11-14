package dev

import (
	"bytes"
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

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/framework/browser/passkey"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// site is a small website with a sign-in: /login sets an HttpOnly session
// cookie and a readable theme cookie, and /api/items needs the session.
func site(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<!doctype html><title>Fake %s</title><p>%s</p>", r.URL.Path, r.URL.Path)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "s3cret", Path: "/", HttpOnly: true, Expires: time.Now().Add(time.Hour)})
		http.SetCookie(w, &http.Cookie{Name: "theme", Value: "dark", Path: "/"})
		fmt.Fprint(w, "signed in")
	})
	mux.HandleFunc("/api/items", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err != nil || c.Value != "s3cret" {
			http.Error(w, "signed out", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"items":["a","b"]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const fakeService = `import { defineService } from "harbor"

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

export default defineService({
  name: "fake",
  title: "Fake",
  url: "URL",
  login: {
    url: "/signin",
    check: async ({ cookies }) => cookies.some((c) => c.name === "session" && !c.expired),
  },
  tools: {
    list_items: {
      description: "List the items.",
      annotations: { readOnlyHint: true },
      outputSchema: { type: "object", required: ["items"], properties: { items: { type: "array", items: { type: "string" } } } },
      async run() {
        const response = await fetch("/api/items")
        if (!response.ok) throw new Error("HTTP " + response.status)
        return await response.json()
      },
    },
    look: {
      description: "Report what the tool can see.",
      inputSchema: { type: "object", required: ["word"], properties: { word: { type: "string" } } },
      async run(input, { cookies }) {
        const w = window as any
        w.calls = (w.calls ?? 0) + 1
        const byName = Object.fromEntries(cookies.map((c) => [c.name, c]))
        return { word: input.word, calls: w.calls, path: location.pathname, session: byName.session, theme: byName.theme }
      },
    },
    wander: {
      description: "Leave the page the tab was on.",
      async run() {
        setTimeout(() => location.assign("/elsewhere"), 0)
        return {}
      },
    },
    slow: {
      description: "Take a while.",
      async run() {
        const start = Date.now()
        await sleep(300)
        return { start, end: Date.now() }
      },
    },
    lie: {
      description: "Return what the schema forbids.",
      outputSchema: { type: "object", properties: { n: { type: "integer" } } },
      async run() { return { n: "not a number" } },
    },
    fail: {
      description: "Throw.",
      async run() { throw new Error("the site said no") },
    },
  },
})
`

// signer has no login check, so it can sign in on the same site.
const signer = `export default {
  name: "signer",
  title: "Signer",
  url: "URL",
  tools: {
    sign_in: { description: "Sign in.", async run() { await fetch("/login"); return {} } },
  },
}
`

func load(t *testing.T, source, url string) engine.Service {
	t.Helper()
	source = strings.Replace(source, "URL", url, 1)
	m, err := service.Read(source)
	if err != nil {
		t.Fatal(err)
	}
	return engine.Service{Manifest: m, Source: source}
}

func newEngine(t *testing.T) *engine.Engine {
	t.Helper()
	e := engine.New(t.TempDir(), &memorySecrets{}, noWindow{}, &memoryFiles{})
	t.Cleanup(e.Close)
	return e
}

// noWindow is Harbor's window in tests, which drive live views themselves
// (Shown, input).
type noWindow struct{}

func (noWindow) Open(string, string, string, bool) (func(), error) { return func() {}, nil }
func (noWindow) Prompt(string, *engine.Prompt) error               { return nil }

// shownTab is the tab the live view of name shows you: as Harbor's window
// does, the newest tab the service's tab opened, or that tab.
func shownTab(e *engine.Engine, name string) (*browser.Tab, error) {
	tab, _, err := e.Shown(name)
	if err != nil {
		return nil, err
	}
	if opened := tab.Opened(); len(opened) > 0 {
		tab = opened[len(opened)-1]
	}
	return tab, nil
}

// input sends ev to the tab the live view of name shows you.
func input(e *engine.Engine, name string, ev browser.InputEvent) error {
	tab, err := shownTab(e, name)
	if err != nil {
		return err
	}
	return tab.Input(context.Background(), ev)
}

// typeText types text into the empty field the live view's page focuses,
// once it has. A page in a tab behind the others focuses its autofocus
// field only once the tab comes to the front, as input brings it, and a
// frame after that: text sent at once goes nowhere. So the mouse moves over
// the page first, as yours would.
func typeText(t *testing.T, e *engine.Engine, name, text string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := input(e, name, browser.InputEvent{Kind: "mouse", Type: "mouseMoved", X: 1, Y: 1}); err != nil {
		t.Fatal(err)
	}
	for {
		if tab, err := shownTab(e, name); err == nil {
			ok, err := tab.Evaluate(ctx, `() => document.readyState === "complete" && document.activeElement?.matches("input, textarea") && document.activeElement.value === ""`)
			if err == nil && string(ok) == "true" {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("the live view of %s never focused a field to type in", name)
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err := input(e, name, browser.InputEvent{Kind: "text", Text: text}); err != nil {
		t.Fatal(err)
	}
}

// memorySecrets keeps an engine's passwords and passkeys for one test.
type memorySecrets struct {
	mu        sync.Mutex
	passwords map[string]browser.Credentials
	keys      []engine.Passkey
	logins    map[string]engine.Login
}

func (m *memorySecrets) Password(origin, realm string) (browser.Credentials, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.passwords[origin+" "+realm]
	return c, ok, nil
}

func (m *memorySecrets) SavePassword(origin, realm string, c browser.Credentials) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.passwords == nil {
		m.passwords = map[string]browser.Credentials{}
	}
	m.passwords[origin+" "+realm] = c
	return nil
}

func (m *memorySecrets) Passkeys(rpID string) ([]engine.Passkey, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []engine.Passkey
	for _, k := range m.keys {
		if k.RPID == rpID {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *memorySecrets) UsePasskeyWithoutAsking(id []byte, auto bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.keys {
		if bytes.Equal(m.keys[i].ID, id) {
			m.keys[i].Auto = auto
		}
	}
	return nil
}

func (m *memorySecrets) Login(origin string) (engine.Login, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.logins[origin]
	return l, ok, nil
}

func (m *memorySecrets) SaveLogin(origin string, l engine.Login) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.logins == nil {
		m.logins = map[string]engine.Login{}
	}
	m.logins[origin] = l
	return nil
}

func (m *memorySecrets) SavePasskey(k passkey.Key) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys = append(m.keys, engine.Passkey{Key: k})
	return nil
}

func call(t *testing.T, e *engine.Engine, svc engine.Service, tool, input string) map[string]any {
	t.Helper()
	// A tool that works ends in seconds; one that is stuck fails the test.
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	out, err := e.Call(ctx, svc, tool, json.RawMessage(input))
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestEngine(t *testing.T) {
	srv := site(t)
	e := newEngine(t)
	fake := load(t, fakeService, srv.URL)
	ctx := context.Background()

	t.Run("signed out", func(t *testing.T) {
		if ok, err := e.SignedIn(ctx, fake); err != nil || ok {
			t.Fatalf("SignedIn = %v, %v; want false", ok, err)
		}
		// The call shows you the login page; closing it ends the call.
		done := make(chan error, 1)
		go func() {
			_, err := e.Call(ctx, fake, "list_items", nil)
			done <- err
		}()
		for e.CloseView("fake") != nil {
			time.Sleep(100 * time.Millisecond)
		}
		err := <-done
		if !errors.Is(err, engine.ErrSignInNeeded) || !strings.Contains(err.Error(), "sign in to Fake first") {
			t.Fatalf("Call while signed out: %v", err)
		}
	})

	t.Run("signed out, without waiting", func(t *testing.T) {
		// A dashboard's call fails at once, and says what to sign in to,
		// in the JSON Harbor's window gets.
		_, err := e.CallWithoutWaiting(ctx, fake, "list_items", nil)
		var needed *engine.SignInNeeded
		if !errors.As(err, &needed) {
			t.Fatalf("CallWithoutWaiting while signed out: %v", err)
		}
		got, _ := json.Marshal(&err)
		if want := `{"service":"fake","heading":"Sign in to Fake","message":"sign in to Fake first"}`; string(got) != want {
			t.Errorf("the error as JSON = %s; want %s", got, want)
		}
		if err := e.CloseView("fake"); !errors.Is(err, engine.ErrNoView) {
			t.Errorf("CloseView = %v; want ErrNoView, as no login page was shown", err)
		}
	})

	call(t, e, load(t, signer, srv.URL), "sign_in", "{}")

	t.Run("signed in", func(t *testing.T) {
		if ok, err := e.SignedIn(ctx, fake); err != nil || !ok {
			t.Fatalf("SignedIn = %v, %v; want true", ok, err)
		}
		got := call(t, e, fake, "list_items", "")
		if fmt.Sprint(got["items"]) != "[a b]" {
			t.Errorf("list_items = %v", got)
		}
	})

	t.Run("cookies, HttpOnly values withheld", func(t *testing.T) {
		got := call(t, e, fake, "look", `{"word":"hi"}`)
		session, _ := got["session"].(map[string]any)
		theme, _ := got["theme"].(map[string]any)
		if session == nil || session["httpOnly"] != true || session["value"] != nil || session["expires"] == nil || session["expired"] != false {
			t.Errorf("session cookie = %v", session)
		}
		if theme == nil || theme["value"] != "dark" || theme["httpOnly"] != false || theme["expires"] != nil {
			t.Errorf("theme cookie = %v", theme)
		}
	})

	t.Run("one tab per service, kept between calls", func(t *testing.T) {
		first := call(t, e, fake, "look", `{"word":"a"}`)["calls"].(float64)
		second := call(t, e, fake, "look", `{"word":"b"}`)["calls"].(float64)
		if second != first+1 {
			t.Errorf("calls went %v then %v; the tab was not kept", first, second)
		}
	})

	t.Run("loads the service again after the page navigates", func(t *testing.T) {
		call(t, e, fake, "wander", "{}")
		deadline := time.Now().Add(5 * time.Second)
		for {
			got := call(t, e, fake, "look", `{"word":"c"}`)
			if got["path"] == "/elsewhere" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the page never navigated: %v", got)
			}
			time.Sleep(100 * time.Millisecond)
		}
	})

	t.Run("the agent's tab goes to a page", func(t *testing.T) {
		p, err := e.Navigate(ctx, srv.URL+"/somewhere")
		if err != nil {
			t.Fatal(err)
		}
		if p.URL != srv.URL+"/somewhere" || p.Title != "Fake /somewhere" || !strings.Contains(p.Snapshot, "/somewhere") {
			t.Errorf("navigate = %+v", p)
		}
	})

	t.Run("runs code in the agent's tab, signed in", func(t *testing.T) {
		out, _, err := e.Evaluate(ctx, `async (context) => { const r = await fetch("/api/items"); return { status: r.status, cookies: context.cookies.length } }`, "")
		if err != nil || string(out) != `{"status":200,"cookies":2}` {
			t.Errorf("evaluate = %s, %v", out, err)
		}
		if _, _, err := e.Evaluate(ctx, `() => { throw new Error("nope") }`, ""); err == nil || err.Error() != "Error: nope" {
			t.Errorf("a thrown error: %v", err)
		}
	})

	t.Run("checks input and output", func(t *testing.T) {
		if _, err := e.Call(ctx, fake, "look", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "inputSchema") {
			t.Errorf("missing input: %v", err)
		}
		if _, err := e.Call(ctx, fake, "lie", nil); err == nil || !strings.Contains(err.Error(), "outputSchema") {
			t.Errorf("wrong output: %v", err)
		}
		if _, err := e.Call(ctx, fake, "nope", nil); err == nil || !strings.Contains(err.Error(), "has no tool nope") {
			t.Errorf("unknown tool: %v", err)
		}
	})

	t.Run("a thrown error is the call's error", func(t *testing.T) {
		_, err := e.Call(ctx, fake, "fail", nil)
		if err == nil || err.Error() != "Error: the site said no" {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("calls to one service take turns", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make([]map[string]any, 2)
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = call(t, e, fake, "slow", "{}")
			}()
		}
		wg.Wait()
		a, b := results[0], results[1]
		if a["start"].(float64) > b["start"].(float64) {
			a, b = b, a
		}
		if b["start"].(float64) < a["end"].(float64) {
			t.Errorf("the calls overlapped: %v and %v", a, b)
		}
	})
}

// memoryFiles keeps an engine's files for one test.
type memoryFiles struct {
	mu    sync.Mutex
	files []engine.File
}

func (m *memoryFiles) SaveFile(f engine.File) (engine.FileRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files = append(m.files, f)
	return engine.FileRef{File: fmt.Sprint(len(m.files) - 1), Name: f.Name, Type: f.Type, Size: len(f.Data)}, nil
}

func (m *memoryFiles) File(id string) (engine.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var i int
	if _, err := fmt.Sscan(id, &i); err != nil || i < 0 || i >= len(m.files) {
		return engine.File{}, fmt.Errorf("no file %s", id)
	}
	return m.files[i], nil
}
