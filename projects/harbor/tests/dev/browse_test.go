package dev

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

// store is a small site to browse: a search form, a page that fetches its
// data from an API, a button that asks first, and a link that opens a tab.
func store(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Shop</title>
<h1>Shop</h1>
<nav><a href="/orders">Orders</a> <a href="/help" target="_blank">Help</a></nav>
<form action="/results">
  <label>Search <input name="q"></label>
  <label>Size <select name="size"><option value="s">Small</option><option value="l">Large</option></select></label>
  <label><input type="checkbox" name="new"> New only</label>
  <button>Find</button>
</form>
<button id="empty" onclick="if (confirm('Empty the cart?')) document.title = 'Emptied'">Empty cart</button>
<div style="cursor: pointer" onclick="document.title = 'Div'">Clickable div</div>
<p hidden>Hidden text</p>`)
	})
	mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><title>Results</title><p>q=%s size=%s new=%s</p>`, r.URL.Query().Get("q"), r.URL.Query().Get("size"), r.URL.Query().Get("new"))
	})
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Orders</title><ul id="list"></ul><script>
fetch("/api/orders", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ page: 1 }) })
  .then((r) => r.json())
  .then((d) => { for (const o of d.orders) document.getElementById("list").innerHTML += "<li>" + o + "</li>" })
</script>`)
	})
	mux.HandleFunc("/api/orders", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"orders":["Order 1","Order 2"]}`)
	})
	mux.HandleFunc("/help", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Help</title><p>How can we help?</p>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// refOf is the ref of the snapshot's line that has what in it.
func refOf(t *testing.T, p engine.Page, what string) string {
	t.Helper()
	for _, line := range strings.Split(p.Snapshot, "\n") {
		if strings.Contains(line, what) {
			if m := regexp.MustCompile(`\[ref=(e\d+)\]`).FindStringSubmatch(line); m != nil {
				return m[1]
			}
		}
	}
	t.Fatalf("no line with %q and a ref in\n%s", what, p.Snapshot)
	return ""
}

func TestTheAgentBrowsesASite(t *testing.T) {
	srv := store(t)
	e := newEngine(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	must := func(p engine.Page, err error) engine.Page {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	if _, err := e.Snapshot(ctx); err != engine.ErrNoPage {
		t.Errorf("a snapshot before going anywhere: %v", err)
	}
	home := must(e.Navigate(ctx, srv.URL+"/"))

	t.Run("reads the page as its accessibility tree", func(t *testing.T) {
		for _, want := range []string{
			`heading "Shop" [level=1]`,
			`link "Orders"`,
			"/url: /orders",
			`textbox "Search"`,
			`combobox "Size"`,
			`checkbox "New only"`,
			`button "Empty cart"`,
			"[cursor=pointer] [ref=",
			"]: Clickable div",
		} {
			if !strings.Contains(home.Snapshot, want) {
				t.Errorf("no %q in\n%s", want, home.Snapshot)
			}
		}
		if strings.Contains(home.Snapshot, "Hidden text") {
			t.Errorf("hidden text is in\n%s", home.Snapshot)
		}
		if again := must(e.Snapshot(ctx)); refOf(t, again, `button "Empty cart"`) != refOf(t, home, `button "Empty cart"`) {
			t.Error("a second snapshot named the button anew")
		}
	})

	t.Run("fills in a form, and submits it", func(t *testing.T) {
		must(e.FillForm(ctx, []engine.Field{
			{Ref: refOf(t, home, `textbox "Search"`), Type: "textbox", Value: "boots"},
			{Ref: refOf(t, home, `combobox "Size"`), Type: "combobox", Value: "Large"},
			{Ref: refOf(t, home, `checkbox "New only"`), Type: "checkbox", Value: "true"},
		}))
		p := must(e.Click(ctx, refOf(t, home, `button "Find"`), false))
		if p.Title != "Results" || !strings.Contains(p.Snapshot, "q=boots size=l new=on") {
			t.Errorf("after Find: %+v", p)
		}
		if back := must(e.Back(ctx)); back.Title != "Shop" {
			t.Errorf("back went to %+v", back)
		}
	})

	t.Run("types and presses Enter", func(t *testing.T) {
		p := must(e.Snapshot(ctx))
		must(e.Type(ctx, refOf(t, p, `textbox "Search"`), "hats", false))
		p = must(e.PressKey(ctx, "Enter"))
		if !strings.Contains(p.Snapshot, "q=hats") {
			t.Errorf("after Enter: %+v", p)
		}
		must(e.Back(ctx))
	})

	t.Run("answers a dialog the page opens", func(t *testing.T) {
		p := must(e.Snapshot(ctx))
		p = must(e.Click(ctx, refOf(t, p, `button "Empty cart"`), false))
		if p.Dialog == nil || p.Dialog.Kind != "confirm" || p.Dialog.Message != "Empty the cart?" {
			t.Fatalf("after the click: %+v", p)
		}
		if _, err := e.Snapshot(ctx); err == nil || !strings.Contains(err.Error(), "browser_handle_dialog") {
			t.Errorf("a snapshot with the dialog open: %v", err)
		}
		if p := must(e.HandleDialog(ctx, true, "")); p.Title != "Emptied" || p.Dialog != nil {
			t.Errorf("after OK: %+v", p)
		}
	})

	t.Run("clicks what only looks clickable", func(t *testing.T) {
		p := must(e.Snapshot(ctx))
		if p = must(e.Click(ctx, refOf(t, p, "Clickable div"), false)); p.Title != "Div" {
			t.Errorf("after the click: %+v", p)
		}
	})

	t.Run("runs code with an element", func(t *testing.T) {
		p := must(e.Snapshot(ctx))
		out, _, err := e.Evaluate(ctx, `(el) => el.tagName`, refOf(t, p, `button "Empty cart"`))
		if err != nil || string(out) != `"BUTTON"` {
			t.Errorf("evaluate = %s, %v", out, err)
		}
		// What the code did comes back with what it returned, by name.
		_, events, err := e.Evaluate(ctx, `() => { localStorage.setItem("app.token", "s3cret"); }`, "")
		if err != nil || !slices.Contains(events, "localStorage of "+srv.URL+": added app.token") || strings.Contains(strings.Join(events, " "), "s3cret") {
			t.Errorf("evaluate's events = %q, %v", events, err)
		}
		if _, _, err := e.Evaluate(ctx, `(el) => el.tagName`, "e999"); err == nil || !strings.Contains(err.Error(), "take a new snapshot") {
			t.Errorf("a ref not on the page: %v", err)
		}
	})

	t.Run("lists the page's requests, and reads one", func(t *testing.T) {
		p := must(e.Navigate(ctx, srv.URL+"/orders"))
		if len(p.Events) == 0 || p.Events[0] != "went to "+srv.URL+"/orders: 200" {
			t.Errorf("the page load's events = %q", p.Events)
		}
		p = must(e.WaitFor(ctx, "Order 2", "", 0))
		if !strings.Contains(p.Snapshot, "Order 1") {
			t.Errorf("the orders page: %s", p.Snapshot)
		}
		requests, err := e.Requests(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var api string
		for _, r := range requests {
			if r.URL == srv.URL+"/api/orders" {
				api = r.ID
			}
		}
		if api == "" {
			t.Fatalf("no request to the API in %+v", requests)
		}
		d, err := e.Request(ctx, api)
		if err != nil {
			t.Fatal(err)
		}
		if d.Method != "POST" || d.Status != 200 || d.RequestBody != `{"page":1}` || d.ResponseBody != `{"orders":["Order 1","Order 2"]}` {
			t.Errorf("the API request = %+v", d)
		}
	})

	t.Run("acts in the tab a link opened", func(t *testing.T) {
		p := must(e.Navigate(ctx, srv.URL+"/"))
		p = must(e.Click(ctx, refOf(t, p, `link "Help"`), false))
		if p.Title != "Help" || len(p.Tabs) != 2 {
			t.Errorf("after the link: %+v", p)
		}
	})

	t.Run("takes a screenshot", func(t *testing.T) {
		shot, err := e.Screenshot(ctx, false)
		if err != nil || !bytes.HasPrefix(shot, []byte{0xff, 0xd8}) {
			t.Errorf("screenshot = %d bytes, %v", len(shot), err)
		}
	})
}

func TestClearingTheAgentsBrowserSignsItOut(t *testing.T) {
	srv := site(t)
	e := newEngine(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	fake := load(t, fakeService, srv.URL)
	status := func() string {
		t.Helper()
		if _, err := e.Navigate(ctx, srv.URL+"/"); err != nil {
			t.Fatal(err)
		}
		out, _, err := e.Evaluate(ctx, `async () => (await fetch("/api/items")).status`, "")
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	// A sign-in's cookies are named in what it did, never their values.
	p, err := e.Navigate(ctx, srv.URL+"/login")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Events, "; "); !strings.Contains(got, "cookie of 127.0.0.1: added session") || strings.Contains(got, "s3cret") {
		t.Errorf("the sign-in's events: %s", got)
	}
	if got := status(); got != "200" {
		t.Fatalf("signed in, the API says %s", got)
	}
	call(t, e, fake, "list_items", "{}")

	if err := e.ClearData(ctx, srv.URL); err != nil {
		t.Fatal(err)
	}
	if got := status(); got != "401" {
		t.Errorf("after clearing the site, the API says %s", got)
	}
	// The service's tab opens again, signed out: its call needs a sign-in.
	quick, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := e.CallWithoutWaiting(quick, fake, "list_items", nil); err == nil {
		t.Error("the service's tool still worked")
	}
}

// browser_sign_in shows the agent's tab at the site until you click Done,
// and says what your sign-in did, by name.
func TestTheAgentWatchesASignIn(t *testing.T) {
	srv := site(t)
	e := newEngine(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	type result struct {
		r   engine.SignInWatched
		err error
	}
	got := make(chan result, 1)
	go func() {
		r, err := e.WatchSignIn(ctx, srv.URL+"/")
		got <- result{r, err}
	}()
	name := engine.SiteView + strings.TrimPrefix(srv.URL, "http://")
	var tab *browser.Tab
	for tab == nil {
		var err error
		if tab, _, err = e.Shown(name); err != nil && !errors.Is(err, engine.ErrNoView) {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// You sign in, then click Done.
	if err := tab.Navigate(ctx, srv.URL+"/login"); err != nil {
		t.Fatal(err)
	}
	if err := e.CloseView(name); err != nil {
		t.Fatal(err)
	}
	r := <-got
	if r.err != nil {
		t.Fatal(r.err)
	}
	all := strings.Join(r.r.Events, "; ")
	if !strings.Contains(all, "went to "+srv.URL+"/login") || !strings.Contains(all, "cookie of 127.0.0.1: added session") || strings.Contains(all, "s3cret") {
		t.Errorf("what the sign-in did: %s", all)
	}
	// The agent's browser stays signed in, to explore the site.
	out, _, err := e.Evaluate(ctx, `async () => (await fetch("/api/items")).status`, "")
	if err != nil || string(out) != "200" {
		t.Errorf("after the sign-in, the API says %s, %v", out, err)
	}
}
