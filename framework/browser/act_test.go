package browser

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestActionsUseThePageAsAPersonDoes(t *testing.T) {
	srv := site(t, map[string]string{"/": `<!doctype html><title>Form</title>
		<label>Name <input id="name" value="old"></label>
		<label>Size <select id="size"><option value="s">Small</option><option value="l">Large</option></select></label>
		<label>Agree <input id="agree" type="checkbox"></label>
		<label>Volume <input id="volume" type="range" min="0" max="10" value="2"></label>
		<button id="go" onclick="this.textContent = 'went'" ondblclick="document.title = 'twice'">Go</button>
		<div id="cover" style="position:fixed;inset:0;display:none;background:white">a banner</div>
		<button id="under">Under</button>
		<script>
			let keys = [];
			addEventListener("keydown", (e) => keys.push((e.ctrlKey ? "Control+" : "") + e.key));
		</script>`})
	b := open(t, Options{})
	ctx := quick(t)
	tab, err := b.Open(ctx, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	ref := func(selector string) string {
		t.Helper()
		r, err := tab.Find(ctx, selector)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	s, err := tab.Snapshot(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Form" || !strings.Contains(s.Text, `textbox "Name" [ref=e`) || !strings.Contains(s.Text, `button "Go" [ref=`) || s.Truncated {
		t.Errorf("snapshot = %+v", s)
	}
	// Find names an element the snapshot named by the same ref.
	if go1 := ref("#go"); !strings.Contains(s.Text, `button "Go" [ref=`+go1+`]`) {
		t.Errorf("Find gave %s for Go, but the snapshot says\n%s", go1, s.Text)
	}
	if short, _ := tab.Snapshot(ctx, 40); !short.Truncated || len(short.Text) > 40 {
		t.Errorf("a snapshot cut to 40 = %+v", short)
	}

	if err := tab.Type(ctx, ref("#name"), "Ada"); err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, tab, `() => document.querySelector("#name").value`); got != `"Ada"` {
		t.Errorf("after Type, the field has %s", got)
	}
	if err := tab.Type(ctx, ref("#name"), ""); err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, tab, `() => document.querySelector("#name").value`); got != `""` {
		t.Errorf("after typing nothing, the field has %s", got)
	}
	if err := tab.Type(ctx, ref("#volume"), "7"); err != nil {
		t.Fatal(err)
	}
	if err := tab.Select(ctx, ref("#size"), "Large"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		// The second does nothing: it is ticked already.
		if err := tab.Check(ctx, ref("#agree"), true); err != nil {
			t.Fatal(err)
		}
	}
	if got := evaluate(t, tab, `() => [document.querySelector("#volume").value, document.querySelector("#size").value, document.querySelector("#agree").checked]`); got != `["7","l",true]` {
		t.Errorf("the slider, the select, and the checkbox are %s", got)
	}

	if err := tab.Click(ctx, ref("#go")); err != nil {
		t.Fatal(err)
	}
	if err := tab.DoubleClick(ctx, ref("#go")); err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, tab, `() => [document.querySelector("#go").textContent, document.title]`); got != `["went","twice"]` {
		t.Errorf("after the clicks, the button and the title are %s", got)
	}

	if err := tab.Press(ctx, "Control+a"); err != nil {
		t.Fatal(err)
	}
	if err := tab.Press(ctx, "Enter"); err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, tab, `() => keys.slice(-2)`); got != `["Control+a","Enter"]` {
		t.Errorf("the page heard %s", got)
	}
	if err := tab.Press(ctx, "Hyper+a"); err == nil || !strings.Contains(err.Error(), "not a modifier") {
		t.Errorf("Press with no such modifier: %v", err)
	}

	under := ref("#under")
	evaluate(t, tab, `() => { document.querySelector("#cover").style.display = "block" }`)
	if err := tab.Click(ctx, under); err == nil || !strings.Contains(err.Error(), under+" is under another element, div#cover") {
		t.Errorf("a click on a covered button: %v", err)
	}
	evaluate(t, tab, `() => document.querySelector("#under").remove()`)
	if err := tab.Click(ctx, under); err == nil || !strings.Contains(err.Error(), "there is no element "+under) {
		t.Errorf("a click on a removed button: %v", err)
	}
	if _, err := tab.Find(ctx, "#nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Find of nothing: %v", err)
	}
}

// A click that opens an alert returns, though Chromium answers it only once
// the alert is answered; until then, Input refuses.
func TestAClickThatOpensADialogReturns(t *testing.T) {
	srv := site(t, map[string]string{"/": `<!doctype html>
		<button id="ask" onclick="alert('sure?'); this.textContent = 'answered'">Ask</button>
		<button id="down" onmousedown="alert('down')" onmouseup="this.textContent = 'released'">Down</button>`})
	shown, answer := make(chan Dialog, 1), make(chan bool)
	var hold atomic.Bool
	hold.Store(true)
	b := open(t, Options{Dialog: func(ctx context.Context, _ *Tab, d Dialog) (bool, string) {
		if !hold.Load() {
			return true, ""
		}
		shown <- d
		return <-answer, ""
	}})
	ctx := quick(t)
	tab, err := b.Open(ctx, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	ask, err := tab.Find(ctx, "#ask")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := tab.Click(ctx, ask); err != nil {
		t.Fatal(err)
	}
	if d := <-shown; d.Message != "sure?" || time.Since(start) > 3*time.Second {
		t.Errorf("the dialog was %+v, after %s", d, time.Since(start))
	}
	if err := tab.Press(ctx, "Enter"); !errors.Is(err, ErrDialogOpen) {
		t.Errorf("a key while the dialog is open: %v", err)
	}
	answer <- true
	eventually(t, "the page went on after the dialog", func() bool {
		raw, err := tab.Evaluate(ctx, `() => document.querySelector("#ask").textContent`)
		return err == nil && string(raw) == `"answered"`
	})

	// An alert the hook answers at once, on the press, leaves the rest of
	// the click to send.
	hold.Store(false)
	down, err := tab.Find(ctx, "#down")
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.Click(ctx, down); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the mouse was let go after the alert", func() bool {
		raw, err := tab.Evaluate(ctx, `() => document.querySelector("#down").textContent`)
		return err == nil && string(raw) == `"released"`
	})
}

func TestCookieChangesSayWhatChangedByName(t *testing.T) {
	before := cookieMark([]Cookie{{Name: "sid", Domain: "tasks.example.com", Path: "/", Value: "a"}, {Name: "old", Domain: "tasks.example.com", Path: "/", Value: "x"}, {Name: "same", Domain: "example.com", Path: "/", Value: "s"}})
	after := cookieMark([]Cookie{{Name: "sid", Domain: "tasks.example.com", Path: "/", Value: "b"}, {Name: "session", Domain: ".example.com", Path: "/", Value: "secret"}, {Name: "same", Domain: "example.com", Path: "/", Value: "s"}})
	got := cookieChanges(before, after)
	want := []CookieChange{{"session", ".example.com", "/", "added"}, {"old", "tasks.example.com", "/", "removed"}, {"sid", "tasks.example.com", "/", "changed"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	srv := site(t, map[string]string{"/": `<!doctype html><title>Cookies</title>`})
	b := open(t, Options{})
	ctx := quick(t)
	tab, err := b.Open(ctx, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	m, err := b.CookieMark(ctx)
	if err != nil {
		t.Fatal(err)
	}
	evaluate(t, tab, `() => { document.cookie = "token=secret; path=/" }`)
	changes, err := b.CookiesChangedSince(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Name != "token" || changes[0].Change != "added" {
		t.Errorf("after a script set a cookie: %+v", changes)
	}
}
