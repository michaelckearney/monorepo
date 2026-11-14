package dev

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

// gate is a website you sign in to through pages, as a person would: /login
// sets the cookie named by ?name, and the sign-in pages lead there.
func gate(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	page := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "<!doctype html><title>Gate</title><body style=\"margin:0\">%s", body)
		}
	}
	mux.HandleFunc("/", page("<p>Gate</p>"))
	// The whole page is a button that opens the sign-in in a pop-up.
	mux.HandleFunc("/popup-signin", page(`<button style="position:fixed;inset:0;width:100%"
		onclick="window.open('/popup', 'signin', 'width=400,height=300')">Sign in</button>`))
	// The pop-up signs in once you type the password, then closes, as many do.
	mux.HandleFunc("/popup", page(`<input autofocus style="position:fixed;inset:0;width:100%">
		<script>
		document.querySelector("input").addEventListener("input", async (e) => {
			if (e.target.value.includes("letmein")) { await fetch("/login?name=popup"); window.close(); }
		});
		</script>`))
	mux.HandleFunc("/email-signin", page("<p>We emailed you a link.</p>"))
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: r.URL.Query().Get("name"), Value: "yes", Path: "/", HttpOnly: true})
		fmt.Fprint(w, "signed in")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

const gated = `export default {
  name: "NAME",
  title: "Gate NAME",
  url: "URL",
  login: { url: "LOGIN", check: async ({ cookies }) => cookies.some((c) => c.name === "NAME") },
  tools: {},
}
`

func gatedService(t *testing.T, name, login, url string) engine.Service {
	return load(t, strings.NewReplacer("NAME", name, "LOGIN", login).Replace(gated), url)
}

// startSignIn runs SignIn in the background.
func startSignIn(e *engine.Engine, svc engine.Service) <-chan error {
	done := make(chan error, 1)
	go func() {
		ok, err := e.SignIn(context.Background(), svc, "")
		if err == nil && !ok {
			err = errors.New("not signed in")
		}
		done <- err
	}()
	return done
}

// signIn runs SignIn in the background, and waits for its live view.
func signIn(t *testing.T, e *engine.Engine, svc engine.Service) <-chan error {
	t.Helper()
	done := startSignIn(e, svc)
	waitView(t, e, svc.Manifest.Name)
	return done
}

func wait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the sign-in did not end")
		return nil
	}
}

func TestEngineSignsInThroughTheLiveView(t *testing.T) {
	srv := gate(t)
	e := newEngine(t)
	ctx := context.Background()

	t.Run("cancelled", func(t *testing.T) {
		svc := gatedService(t, "cancel", "/email-signin", srv.URL)
		done := signIn(t, e, svc)
		if err := e.CloseView("cancel"); err != nil {
			t.Fatal(err)
		}
		if err := wait(t, done); err == nil || err.Error() != "not signed in" {
			t.Fatalf("a cancelled sign-in ended with %v", err)
		}
		if _, _, err := e.Shown("cancel"); !errors.Is(err, engine.ErrNoView) {
			t.Errorf("the live view outlived its sign-in: %v", err)
		}
	})

	t.Run("in a pop-up, with mouse and keys", func(t *testing.T) {
		svc := gatedService(t, "popup", "/popup-signin", srv.URL)
		done := signIn(t, e, svc)
		clickPage(t, e, "popup", "/popup-signin")
		// The view follows the page to the pop-up it opens.
		showing(t, e, "popup", "/popup")
		typeText(t, e, "popup", "letmein")
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("with an emailed link", func(t *testing.T) {
		svc := gatedService(t, "email", "/email-signin", srv.URL)
		done := signIn(t, e, svc)
		// The view opens before the sign-in loads its login page, so
		// wait for that load to finish: pasting the link while it is
		// still going aborts it, and the sign-in ends with its
		// ERR_ABORTED.
		showing(t, e, "email", "/email-signin")
		// As you would paste it into the address bar.
		tab, _, err := e.Shown("email")
		if err != nil {
			t.Fatal(err)
		}
		if err := tab.Navigate(ctx, srv.URL+"/login?name=email"); err != nil {
			t.Fatal(err)
		}
		if err := wait(t, done); err != nil {
			t.Fatal(err)
		}
		if ok, err := e.SignedIn(ctx, svc); err != nil || !ok {
			t.Errorf("SignedIn after the sign-in = %v, %v", ok, err)
		}
	})
}
