package browser

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestATabListsItsPagesRequests(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Page</title>`)
	})
	mux.HandleFunc("/old", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api/items", http.StatusFound)
	})
	mux.HandleFunc("/api/items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"items":["a"]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	b := open(t, Options{})
	ctx := quick(t)
	tab, err := b.Open(ctx, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	evaluate(t, tab, `async () => {
		await (await fetch("/api/items", { method: "POST", headers: { "X-Token": "t1" }, body: JSON.stringify({ q: 1 }) })).text();
		await fetch("/old");
	}`)
	find := func(method, path string) Request {
		for _, r := range tab.Requests() {
			if r.Method == method && r.URL == srv.URL+path {
				return r
			}
		}
		t.Fatalf("no %s %s in %+v", method, path, tab.Requests())
		return Request{}
	}
	eventually(t, "the redirect's response arrived", func() bool {
		for _, r := range tab.Requests() {
			if r.URL == srv.URL+"/api/items" && r.Method == "GET" && r.Status == 200 {
				return true
			}
		}
		return false
	})

	if r := find("GET", "/"); r.Type != "Document" || r.Status != 200 {
		t.Errorf("the page = %+v", r)
	}
	post := find("POST", "/api/items")
	if post.Type != "Fetch" || post.Status != 200 || post.MimeType != "application/json" || !post.HasBody || post.RequestHeaders["X-Token"] != "t1" {
		t.Errorf("the POST = %+v", post)
	}
	if body, err := tab.RequestBody(ctx, post.ID); err != nil || string(body) != `{"q":1}` {
		t.Errorf("RequestBody = %q, %v", body, err)
	}
	if body, err := tab.ResponseBody(ctx, post.ID); err != nil || string(body) != `{"items":["a"]}` {
		t.Errorf("ResponseBody = %q, %v", body, err)
	}
	if r := find("GET", "/old"); r.Status != 302 || r.ResponseHeaders["Location"] != "/api/items" {
		t.Errorf("the redirect = %+v", r)
	}

	// A new page keeps what the pages before it made, and a Mark asks for
	// what came after it: here, the page load, and what the page saved.
	before := len(pageRequests(tab.Requests()))
	mark := tab.Mark()
	if err := tab.Navigate(ctx, srv.URL+"/?again"); err != nil {
		t.Fatal(err)
	}
	evaluate(t, tab, `() => { localStorage.setItem("app.token", "secret"); localStorage.setItem("app.token", "newer"); sessionStorage.setItem("tab", "1"); localStorage.removeItem("app.token"); document.cookie = "sid=1" }`)
	if got := pageRequests(tab.Requests()); len(got) != before+1 {
		t.Errorf("after a new page, %d requests, want %d", len(got), before+1)
	}
	if got := pageRequests(tab.RequestsSince(mark)); len(got) != 1 || !got[0].Navigation || got[0].Page != srv.URL+"/?again" {
		t.Errorf("RequestsSince = %+v", got)
	}
	eventually(t, "the storage events arrived", func() bool { return len(tab.SavedSince(mark)) == 4 })
	// In order for each storage; Chromium does not keep the order between
	// localStorage and sessionStorage.
	saved := map[bool][]string{}
	for _, s := range tab.SavedSince(mark) {
		saved[s.Local] = append(saved[s.Local], fmt.Sprintf("%s %s %s", s.Origin, s.Change, s.Key))
	}
	if got, want := strings.Join(append(saved[true], saved[false]...), "; "), fmt.Sprintf("%[1]s added app.token; %[1]s changed app.token; %[1]s removed app.token; %[1]s added tab", srv.URL); got != want {
		t.Errorf("SavedSince = %s\nwant %s", got, want)
	}
	all, err := b.AllCookies(ctx)
	if err != nil || len(all) != 1 || all[0].Name != "sid" {
		t.Errorf("AllCookies = %+v, %v", all, err)
	}
}

func TestClearDataRemovesASitesCookiesAndStorage(t *testing.T) {
	page := `<!doctype html><title>Page</title>`
	srv := site(t, map[string]string{"/": page})
	other := site(t, map[string]string{"/": page})
	b := open(t, Options{})
	ctx := quick(t)
	keep := `() => { document.cookie = "c=1; path=/"; localStorage.setItem("k", "1"); }`
	read := `() => document.cookie + "|" + localStorage.getItem("k")`
	// httptest serves both on 127.0.0.1; localhost is another site to the
	// browser.
	otherURL := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	tab, err := b.Open(ctx, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := b.Open(ctx, otherURL+"/")
	if err != nil {
		t.Fatal(err)
	}
	evaluate(t, tab, keep)
	evaluate(t, elsewhere, keep)

	if err := b.ClearData(ctx, srv.URL+"/some/page"); err != nil {
		t.Fatal(err)
	}
	if err := tab.Navigate(ctx, srv.URL+"/"); err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, tab, read); got != `"|null"` {
		t.Errorf("the cleared site has %s", got)
	}
	if got := evaluate(t, elsewhere, read); got != `"c=1|1"` {
		t.Errorf("the other site has %s, want its own still", got)
	}
	if err := b.ClearData(ctx, "example.com"); err == nil {
		t.Error("a site with no scheme was taken")
	}

	// Everything: the profile goes, and the browser starts again empty.
	marker := filepath.Join(b.dir(), "marker")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.ClearData(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("the profile is still there: %v", err)
	}
	again, err := b.Open(ctx, otherURL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, again, read); got != `"|null"` {
		t.Errorf("after clearing everything, the other site has %s", got)
	}
}

func TestScreenshot(t *testing.T) {
	srv := site(t, map[string]string{"/": `<!doctype html><body style="height:3000px;background:#08f">`})
	b := open(t, Options{})
	ctx := quick(t)
	tab, err := b.Open(ctx, srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	window, err := tab.Screenshot(ctx, false)
	if err != nil || !bytes.HasPrefix(window, []byte{0xff, 0xd8}) {
		t.Fatalf("Screenshot = %d bytes, %v; want a JPEG", len(window), err)
	}
	full, err := tab.Screenshot(ctx, true)
	if err != nil || len(full) <= len(window) {
		t.Errorf("the whole page is %d bytes, %v; the window %d", len(full), err, len(window))
	}
}

// pageRequests leaves out the favicon, which Chromium asks for on its own
// and, on Linux, at a time of its own.
func pageRequests(rs []Request) []Request {
	var out []Request
	for _, r := range rs {
		if !strings.HasSuffix(r.URL, "/favicon.ico") {
			out = append(out, r)
		}
	}
	return out
}
