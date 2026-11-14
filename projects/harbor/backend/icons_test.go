package backend

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/framework"
)

var iconPNG = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 16)...)

// site serves a page declaring /icon.png, and the icon with an ETag. up is
// false to make every request fail.
func site(t *testing.T, up *bool, requests *int) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests++
		if !*up {
			http.Error(w, "down", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="icon" href="/icon.png"></head></html>`))
		case "/icon.png":
			if r.Header.Get("If-None-Match") == `"1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"1"`)
			w.Write(iconPNG)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

type iconRow struct {
	status, mime string
	bytes        []byte
	failures     int
}

func iconOf(t *testing.T, i *Icons, origin string) iconRow {
	t.Helper()
	var r iconRow
	if err := i.db.QueryRow(`SELECT status, coalesce(mime_type, ''), bytes, failure_count FROM service_icons WHERE origin = ?`,
		origin).Scan(&r.status, &r.mime, &r.bytes, &r.failures); err != nil {
		t.Fatal(err)
	}
	return r
}

func addService(t *testing.T, i *Icons, name, url string) {
	t.Helper()
	if _, err := i.db.Exec(`INSERT INTO services (name, source, manifest) VALUES (?, '', json_object('url', ?))`, name, url); err != nil {
		t.Fatal(err)
	}
}

// due makes every icon due, as if its refresh or backoff had passed.
func due(t *testing.T, i *Icons) {
	t.Helper()
	if _, err := i.db.Exec(`UPDATE service_icons SET next_attempt_at = ?`, stamp(time.Now().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
}

func TestSweepFindsEachSitesIconOnce(t *testing.T) {
	up, requests := true, 0
	server := site(t, &up, &requests)
	i := newIcons(desktop.TestDB(t))
	// Two services on one site share its icon.
	addService(t, i, "a", server.URL+"/accounts")
	addService(t, i, "b", server.URL+"/")

	i.sweep(context.Background())
	if got := iconOf(t, i, server.URL); got.status != "ok" || got.mime != "image/png" || !bytes.Equal(got.bytes, iconPNG) {
		t.Fatalf("icon = %+v", got)
	}
	if requests != 2 {
		t.Errorf("%d requests, want 2: the page and the icon", requests)
	}

	requests = 0
	i.sweep(context.Background())
	if requests != 0 {
		t.Errorf("a sweep before the refresh is due made %d requests", requests)
	}

	// A refresh is conditional, and a 304 keeps the icon.
	due(t, i)
	i.sweep(context.Background())
	if got := iconOf(t, i, server.URL); got.status != "ok" || !bytes.Equal(got.bytes, iconPNG) {
		t.Errorf("after a 304, icon = %+v", got)
	}
}

func TestSweepKeepsAnIconWhenTheSiteIsDown(t *testing.T) {
	up, requests := true, 0
	server := site(t, &up, &requests)
	i := newIcons(desktop.TestDB(t))
	addService(t, i, "a", server.URL)
	i.sweep(context.Background())

	up = false
	due(t, i)
	i.sweep(context.Background())
	got := iconOf(t, i, server.URL)
	if got.status != "ok" || !bytes.Equal(got.bytes, iconPNG) || got.failures != 1 {
		t.Errorf("after a failure, icon = %+v", got)
	}
}

func TestSweepNotesASiteThatIsDownAndRetriesLater(t *testing.T) {
	up, requests := false, 0
	server := site(t, &up, &requests)
	i := newIcons(desktop.TestDB(t))
	addService(t, i, "a", server.URL)
	i.sweep(context.Background())
	if got := iconOf(t, i, server.URL); got.status != "error" || got.failures != 1 {
		t.Fatalf("icon = %+v", got)
	}

	requests = 0
	i.sweep(context.Background())
	if requests != 0 {
		t.Errorf("retried before the backoff, with %d requests", requests)
	}

	up = true
	due(t, i)
	i.sweep(context.Background())
	if got := iconOf(t, i, server.URL); got.status != "ok" || got.failures != 0 {
		t.Errorf("after the site came up, icon = %+v", got)
	}
}

func TestSweepForgetsTheIconsOfSitesNoServiceUses(t *testing.T) {
	up, requests := true, 0
	server := site(t, &up, &requests)
	i := newIcons(desktop.TestDB(t))
	addService(t, i, "a", server.URL)
	i.sweep(context.Background())
	i.db.Exec(`DELETE FROM services`)

	i.sweep(context.Background())
	var n int
	i.db.QueryRow(`SELECT count(*) FROM service_icons`).Scan(&n)
	if n != 1 {
		t.Fatal("forgot an icon fetched just now")
	}
	i.db.Exec(`UPDATE service_icons SET fetched_at = ?`, stamp(time.Now().Add(-iconForgetAfter-time.Hour)))
	i.sweep(context.Background())
	i.db.QueryRow(`SELECT count(*) FROM service_icons`).Scan(&n)
	if n != 0 {
		t.Error("kept the icon of a site no service has used for a month")
	}
}

func TestIconBackoff(t *testing.T) {
	for failures, want := range map[int]time.Duration{
		1: 30 * time.Minute, 2: time.Hour, 3: 2 * time.Hour, 6: 16 * time.Hour, 7: 24 * time.Hour, 20: 24 * time.Hour,
	} {
		if got := iconBackoff(failures); got != want {
			t.Errorf("iconBackoff(%d) = %v, want %v", failures, got, want)
		}
	}
}

func TestSavingAServiceAsksForASweep(t *testing.T) {
	db := desktop.TestDB(t)
	d := Documents{db: db, icons: newIcons(db)}
	if _, err := d.SaveService(0, serviceStarter); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.icons.requests:
	default:
		t.Error("no sweep was asked for")
	}
}
