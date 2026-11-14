package dev

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

// bank is a website that, like a real bank, wants your password again
// before a transfer: /transfer answers 403 until /confirm?password=letmein.
// Like some banks, it asks only once you try: /status says you need to
// confirm only after a transfer was refused.
func bank(t *testing.T) *httptest.Server {
	var mu sync.Mutex
	asked, confirmed := false, false
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Bank</title><p>Bank`)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "yes", Path: "/"})
		fmt.Fprint(w, "signed in")
	})
	// A page you type the password into, as the live view lets you.
	mux.HandleFunc("/confirm-page", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><body style="margin:0"><input autofocus style="position:fixed;inset:0;width:100%">
			<script>document.querySelector("input").addEventListener("input", (e) => {
				if (e.target.value.includes("letmein")) fetch("/confirm?password=letmein");
			});</script>`)
	})
	mux.HandleFunc("/confirm", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		confirmed = r.URL.Query().Get("password") == "letmein"
		fmt.Fprint(w, confirmed)
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		fmt.Fprint(w, !asked || confirmed)
	})
	mux.HandleFunc("/transfer", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !confirmed {
			asked = true
			http.Error(w, "confirm it's you", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "sent")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// bankService signs in on its own; its transfer tool needs you to confirm,
// which it does on its own when RUN is a run function.
const bankService = `export default {
  name: "NAME",
  title: "Bank",
  url: "URL",
  login: {
    url: "/",
    check: async ({ cookies }) => cookies.some((c) => c.name === "session"),
    run: async () => { await fetch("/login") },
  },
  tools: {
    transfer: {
      description: "Send money.",
      login: {
        url: "/confirm-page",
        check: async () => (await (await fetch("/status")).text()) === "true",
        RUN
      },
      async run() {
        const r = await fetch("/transfer")
        if (!r.ok) throw new Error("HTTP " + r.status)
        return { said: await r.text() }
      },
    },
  },
}
`

func bankWith(t *testing.T, srv *httptest.Server, name, run string) engine.Service {
	src := bankService
	for _, r := range [][2]string{{"NAME", name}, {"RUN", run}} {
		src = replaceOnce(t, src, r[0], r[1])
	}
	return load(t, src, srv.URL)
}

func replaceOnce(t *testing.T, s, old, new string) string {
	t.Helper()
	if strings.Count(s, old) != 1 {
		t.Fatalf("%q is not in the service once", old)
	}
	return strings.Replace(s, old, new, 1)
}

func TestGetsThroughAToolsLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("on its own: signs in and then confirms once the bank asks", func(t *testing.T) {
		srv := bank(t)
		e := newEngine(t)
		svc := bankWith(t, srv, "auto", `run: async () => { await fetch("/confirm?password=letmein") },`)
		if got := call(t, e, svc, "transfer", "{}"); got["said"] != "sent" {
			t.Errorf("transfer = %v", got)
		}
	})

	t.Run("with you in the live view when it cannot on its own", func(t *testing.T) {
		srv := bank(t)
		e := newEngine(t)
		svc := bankWith(t, srv, "shown", "")
		done := make(chan error, 1)
		go func() {
			_, err := e.Call(ctx, svc, "transfer", nil)
			done <- err
		}()
		// The live view shows the confirm page; you type the password.
		showing(t, e, "shown", "/confirm-page")
		typeText(t, e, "shown", "letmein")
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("without waiting: says so at once; SignIn gets you through", func(t *testing.T) {
		srv := bank(t)
		e := newEngine(t)
		svc := bankWith(t, srv, "dash", "")
		_, err := e.CallWithoutWaiting(ctx, svc, "transfer", nil)
		var needed *engine.SignInNeeded
		if !errors.As(err, &needed) || needed.Tool != "transfer" {
			t.Fatalf("the call ended with %v; want SignInNeeded for transfer", err)
		}
		done := make(chan error, 1)
		go func() {
			ok, err := e.SignIn(ctx, svc, "transfer")
			if err == nil && !ok {
				err = errors.New("SignIn says you are not through")
			}
			done <- err
		}()
		showing(t, e, "dash", "/confirm-page")
		typeText(t, e, "dash", "letmein")
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
		out, err := e.CallWithoutWaiting(ctx, svc, "transfer", nil)
		if err != nil || !strings.Contains(string(out), "sent") {
			t.Errorf("transfer after signing in = %s, %v", out, err)
		}
	})

	t.Run("signed out if you close the live view", func(t *testing.T) {
		srv := bank(t)
		e := newEngine(t)
		svc := bankWith(t, srv, "closed", "")
		done := make(chan error, 1)
		go func() {
			_, err := e.Call(ctx, svc, "transfer", nil)
			done <- err
		}()
		for e.CloseView("closed") != nil {
			time.Sleep(100 * time.Millisecond)
		}
		select {
		case err := <-done:
			if !errors.Is(err, engine.ErrSignInNeeded) {
				t.Errorf("the call ended with %v; want ErrSignInNeeded", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the call did not end")
		}
	})
}

func TestBrowsesAService(t *testing.T) {
	srv := bank(t)
	e := newEngine(t)
	svc := load(t, `export default { name: "look", title: "Look", url: "URL", tools: {} }`, srv.URL)
	done := make(chan error, 1)
	go func() { done <- e.Browse(context.Background(), svc) }()
	waitView(t, e, "look")
	if err := e.CloseView("look"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Errorf("Browse ended with %v", err)
	}
}
