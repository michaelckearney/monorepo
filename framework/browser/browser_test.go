package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/framework/internal/deps"
	"github.com/michaelckearney/monorepo/framework/internal/pin"
)

// Chrome names the extension after the key in its manifest; the browser
// works the name out the same way, to open the extension's page.
func TestExtensionID(t *testing.T) {
	id, err := extensionID()
	if err != nil {
		t.Fatal(err)
	}
	// What Chrome called the extension when it loaded it.
	if want := "ddmnknpllepkbgcbhkidgajlmfmmkaah"; id != want {
		t.Errorf("extensionID() = %s, want %s", id, want)
	}
}

// open starts a browser on the Chromium desktop pins, downloading it into
// ~/.desktop the first time, as an app's dependency would be.
func open(t *testing.T, o Options) *Browser {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Chromium")
	}
	d, ok := pin.Chromium[deps.Platform()]
	if !ok {
		t.Skipf("desktop pins no Chromium for %s", deps.Platform())
	}
	dir, err := deps.Pinned(context.Background(), pin.ChromiumDep, d)
	if err != nil {
		t.Fatal(err)
	}
	o.Dir = t.TempDir()
	b := Open(o)
	if b.exec, err = Executable(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	return b
}

// site serves pages, by path.
func site(t *testing.T, pages map[string]string) *httptest.Server {
	mux := http.NewServeMux()
	for path, body := range pages {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if name, ok := strings.CutPrefix(body, "download:"); ok {
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
				fmt.Fprint(w, "the file")
				return
			}
			fmt.Fprint(w, body)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// quick is a context for the test's calls to the browser: none takes more
// than a few seconds when it works, so a stuck one fails the test instead of
// waiting for go test's ten minutes.
func quick(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// eventually waits for ok, for up to ten seconds.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
	}
}

func evaluate(t *testing.T, tab *Tab, fn string, args ...any) string {
	t.Helper()
	raw, err := tab.Evaluate(quick(t), fn, args...)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// clickOn clicks the element, as you would: a page opens a file picker
// only for a click a person made.
func clickOn(t *testing.T, tab *Tab, selector string) {
	t.Helper()
	ref, err := tab.Find(quick(t), selector)
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.Click(quick(t), ref); err != nil {
		t.Fatal(err)
	}
}

func TestATabKnowsWhereItIsAndWhatItOpened(t *testing.T) {
	srv := site(t, map[string]string{
		"/":      `<!doctype html><title>One</title><a href="/two">two</a> <a href="/new" target="_blank">new</a>`,
		"/two":   `<!doctype html><title>Two</title>`,
		"/new":   `<!doctype html><title>New</title><button onclick="window.close()">close</button>`,
		"/blank": `<!doctype html><title>Blank</title>`,
	})
	b := open(t, Options{})
	ctx := quick(t)
	tab, err := b.Open(ctx, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	eventually(t, "the state says the first page", func() bool {
		s := tab.State()
		return s.Title == "One" && s.URL == srv.URL+"/" && !s.CanGoBack
	})

	t.Run("back and forward", func(t *testing.T) {
		if err := tab.Navigate(ctx, srv.URL+"/two"); err != nil {
			t.Fatal(err)
		}
		eventually(t, "it can go back", func() bool { s := tab.State(); return s.Title == "Two" && s.CanGoBack })
		if err := tab.Back(ctx); err != nil {
			t.Fatal(err)
		}
		eventually(t, "it went back, and can go forward", func() bool {
			s := tab.State()
			return s.Title == "One" && s.CanGoForward && !s.CanGoBack
		})
		if err := tab.Back(ctx); err == nil {
			t.Error("Back with nothing to go back to did not fail")
		}
	})

	t.Run("a tab it opens, until that closes", func(t *testing.T) {
		changed := tab.Changed()
		clickOn(t, tab, `a[target]`)
		select {
		case <-changed:
		case <-time.After(10 * time.Second):
			t.Fatal("Changed did not say a tab opened")
		}
		eventually(t, "the new tab is among those it opened", func() bool { return len(tab.Opened()) == 1 })
		opened := tab.Opened()[0]
		if opened.Opener() != tab {
			t.Errorf("the new tab's Opener is %v, want the tab that opened it", opened.Opener())
		}
		eventually(t, "the new tab is on its page", func() bool { return opened.State().Title == "New" })
		// The page closes its tab as the click lands, which on Linux can be
		// before the click's own reply: then the click fails as closed.
		ref, err := opened.Find(quick(t), "button")
		if err != nil {
			t.Fatal(err)
		}
		if err := opened.Click(quick(t), ref); err != nil && err.Error() != "the tab was closed" {
			t.Fatal(err)
		}
		select {
		case <-opened.Done():
		case <-time.After(10 * time.Second):
			t.Fatal("the page closed its tab, and Done never said so")
		}
		eventually(t, "the closed tab is not among those it opened", func() bool { return len(tab.Opened()) == 0 })
	})
}

func TestExposeTellsGoTheCallersSite(t *testing.T) {
	srv := site(t, map[string]string{"/": `<!doctype html><title>Page</title>`})
	b := open(t, Options{})
	ctx := quick(t)
	tab, err := b.Open(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	err = tab.Expose(ctx, "ask", func(ctx context.Context, origin string, arg json.RawMessage) (any, error) {
		if string(arg) == `"fail"` {
			return nil, errors.New("Go said no")
		}
		return map[string]any{"origin": origin, "arg": arg}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.Navigate(ctx, srv.URL+"/"); err != nil {
		t.Fatal(err)
	}
	got := evaluate(t, tab, `async () => await ask({ n: 1 })`)
	if want := fmt.Sprintf(`{"arg":{"n":1},"origin":%q}`, srv.URL); got != want {
		t.Errorf("ask returned %s, want %s", got, want)
	}
	got = evaluate(t, tab, `async () => { try { await ask("fail") } catch (e) { return e.message } }`)
	if got != `"Go said no"` {
		t.Errorf("a call that failed in Go threw %s", got)
	}
}

func TestDialogsFilesAndDownloadsGoToTheHooks(t *testing.T) {
	srv := site(t, map[string]string{
		"/":     `<!doctype html><title>Page</title><input type="file" accept=".txt" style="width:200px;height:50px"> <a href="/file" download>file</a>`,
		"/file": "download:report.txt",
	})
	upload := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(upload, []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	var asked Dialog
	var accept string
	downloaded := make(chan Download, 1)
	b := open(t, Options{
		Dialog: func(ctx context.Context, tab *Tab, d Dialog) (bool, string) {
			asked = d
			return true, "typed"
		},
		ChooseFiles: func(ctx context.Context, tab *Tab, r FileRequest) ([]string, error) {
			accept = r.Accept
			return []string{upload}, nil
		},
		Downloaded: func(tab *Tab, d Download) { downloaded <- d },
	})
	ctx := quick(t)
	tab, err := b.Open(ctx, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a dialog", func(t *testing.T) {
		if got := evaluate(t, tab, `() => prompt("Your name?", "Ada")`); got != `"typed"` {
			t.Errorf("prompt returned %s", got)
		}
		if asked.Kind != "prompt" || asked.Message != "Your name?" || asked.Default != "Ada" {
			t.Errorf("the hook was asked %+v", asked)
		}
	})

	t.Run("files for an input", func(t *testing.T) {
		clickOn(t, tab, "input")
		eventually(t, "the input has the file", func() bool {
			return evaluate(t, tab, `() => document.querySelector("input").files[0]?.name ?? ""`) == `"notes.txt"`
		})
		if accept != ".txt" {
			t.Errorf("the hook was told the input accepts %q", accept)
		}
	})

	t.Run("a download", func(t *testing.T) {
		clickOn(t, tab, "a")
		select {
		case d := <-downloaded:
			body, err := os.ReadFile(d.Path)
			if d.Name != "report.txt" || d.URL != srv.URL+"/file" || err != nil || string(body) != "the file" {
				t.Errorf("downloaded %+v: %q, %v", d, body, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the download never reached the hook")
		}
	})
}

// A page that would not load is said in a sentence someone using the app
// can act on, with Chromium's own reason kept for whoever is debugging.
func TestAFailedPageLoadSaysWhatToDo(t *testing.T) {
	for _, reason := range []string{"net::ERR_UNSAFE_PORT", "page load error net::ERR_UNSAFE_PORT"} {
		err := pageLoadError("http://127.0.0.1:9", reason)
		sentence, detail, ok := strings.Cut(err.Error(), "\n")
		if !ok {
			t.Fatalf("the error keeps no separate reason: %q", err)
		}
		if want := "Could not open http://127.0.0.1:9. Check the address and that the site is up."; sentence != want {
			t.Errorf("the sentence is %q, want %q", sentence, want)
		}
		if detail != "net::ERR_UNSAFE_PORT" {
			t.Errorf("the reason is %q, want the code Chromium gave", detail)
		}
	}
	if _, ok := isPageLoadError(errors.New("the tab was closed")); ok {
		t.Error("a closed tab was taken for a page that would not load")
	}
	if _, ok := isPageLoadError(errors.New("page load error net::ERR_CONNECTION_REFUSED")); !ok {
		t.Error("chromedp's own load failure was not recognised")
	}
}

// A site can name a download anything.
func TestSafeName(t *testing.T) {
	for name, want := range map[string]string{
		"report.pdf":     "report.pdf",
		"../../.bashrc":  ".bashrc",
		`..\..\evil.exe`: "evil.exe",
		"":               "download",
		"..":             "download",
		"/":              "download",
	} {
		if got := safeName(name); got != want {
			t.Errorf("safeName(%q) = %q, want %q", name, got, want)
		}
	}
}

// Holding Option sends its key again and again. Chromium once took its code,
// 18, as a Mac's own key code, and on a Mac that opened System Information.
// Option-Delete deletes a word.
func TestHoldingOptionIsOnlyAKey(t *testing.T) {
	// Option and its word deletion are the Mac's: Chromium on Linux takes
	// Alt+Backspace as undo, and the field comes back empty.
	if runtime.GOOS != "darwin" {
		t.Skip("Option is a Mac key")
	}
	running := func() bool { return exec.Command("pgrep", "-x", "System Information").Run() == nil }
	before := running()
	srv := site(t, map[string]string{"/": `<!doctype html><input autofocus>`})
	b := open(t, Options{})
	ctx := quick(t)
	tab, err := b.Open(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	in := func(ev InputEvent) {
		t.Helper()
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := tab.Input(c, ev); err != nil {
			t.Fatalf("%s %s: %v", ev.Type, ev.Key, err)
		}
	}
	in(InputEvent{Kind: "text", Text: "hello big world"})
	option := InputEvent{Kind: "key", Type: "rawKeyDown", Key: "Alt", Code: "AltLeft", KeyCode: 18, Modifiers: 1}
	for range 10 {
		in(option)
	}
	in(InputEvent{Kind: "key", Type: "rawKeyDown", Key: "Backspace", Code: "Backspace", KeyCode: 8, Modifiers: 1})
	in(InputEvent{Kind: "key", Type: "keyUp", Key: "Backspace", Code: "Backspace", KeyCode: 8, Modifiers: 1})
	in(InputEvent{Kind: "key", Type: "keyUp", Key: "Alt", Code: "AltLeft", KeyCode: 18})
	got, err := tab.Evaluate(ctx, `() => document.activeElement.value`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"hello big "`; string(got) != want {
		t.Errorf("the field has %s, want %s", got, want)
	}
	if runtime.GOOS == "darwin" && !before && running() {
		t.Error("holding Option opened System Information")
	}
}
