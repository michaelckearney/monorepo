package favicon

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var (
	pngBytes   = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x01}, 32)...)
	icoBytes   = append([]byte("\x00\x00\x01\x00"), bytes.Repeat([]byte{0x02}, 32)...)
	gifBytes   = append([]byte("GIF89a"), bytes.Repeat([]byte{0x03}, 32)...)
	svgBytes   = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"></svg>`)
	htmlPage   = []byte("<!doctype html><html><head><title>Sign in</title></head><body>nope</body></html>")
	testClient = func() *http.Client {
		client := NewClient()
		return client
	}
)

func fetch(t *testing.T, server *httptest.Server, prior Conditional) (Result, bool) {
	t.Helper()
	result, notModified, err := Fetch(context.Background(), testClient(), server.URL, prior)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	return result, notModified
}

func TestFetchFallsBackToFaviconICOWhenNothingIsDeclared(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte("<html><head></head><body></body></html>"))
		case "/favicon.ico":
			w.Write(icoBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if result.Status != StatusOK {
		t.Fatalf("status = %q, want %q", result.Status, StatusOK)
	}
	if result.Light.MIMEType != "image/x-icon" {
		t.Fatalf("mime = %q, want image/x-icon", result.Light.MIMEType)
	}
	if !bytes.Equal(result.Light.Bytes, icoBytes) {
		t.Fatal("bytes do not match the served icon")
	}
}

func TestFetchResolvesRelativeLinkHref(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="icon" href="assets/brand.png"></head></html>`))
		case "/assets/brand.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if result.Light.MIMEType != "image/png" {
		t.Fatalf("mime = %q, want image/png", result.Light.MIMEType)
	}
	if !strings.HasSuffix(result.Light.IconURL, "/assets/brand.png") {
		t.Fatalf("icon URL = %q, want it to end in /assets/brand.png", result.Light.IconURL)
	}
}

// A login redirect is the common shape for an authenticated service, and the
// icon it declares is relative to where the redirect landed, not to the origin.
func TestFetchResolvesHrefAgainstTheFinalURLAfterRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "/login/", http.StatusFound)
		case "/login/":
			w.Write([]byte(`<html><head><link rel="icon" href="brand.png"></head></html>`))
		case "/login/brand.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if !strings.HasSuffix(result.Light.IconURL, "/login/brand.png") {
		t.Fatalf("icon URL = %q, want it resolved against the redirect target", result.Light.IconURL)
	}
}

// Chromium's desktop handler targets 16 CSS px at 1x and 2x, so a 32px icon
// beats both a 16px one and a 512px one. The largest-wins rule this replaced
// picked artwork drawn for a home-screen tile.
func TestFetchPrefersTheSizeClosestToTheRenderedSize(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="icon" sizes="16x16" href="/small.png">
				<link rel="icon" sizes="512x512" href="/huge.png">
				<link rel="icon" sizes="32x32" href="/right.png">
			</head></html>`))
		case "/right.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, _ = fetch(t, server, Conditional{}); requested[1] != "/right.png" {
		t.Fatalf("first icon request = %q, want /right.png", requested[1])
	}
}

// The regression that started this: Grafana declares rel="icon" alongside a
// rel="mask-icon" SVG, which is Safari's pinned-tab glyph — a black silhouette,
// not the brand mark. Chromium does not recognize mask-icon as an icon at all.
func TestFetchIgnoresMaskIconAndFluidIcon(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="icon" type="image/png" href="/fav32.png">
				<link rel="apple-touch-icon" sizes="180x180" href="/touch.png">
				<link rel="mask-icon" href="/mask.svg" color="#F05A28">
				<link rel="fluid-icon" href="/fluid.png">
			</head></html>`))
		case "/fav32.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if !strings.HasSuffix(result.Light.IconURL, "/fav32.png") {
		t.Fatalf("icon URL = %q, want /fav32.png", result.Light.IconURL)
	}
	for _, path := range requested {
		if path == "/mask.svg" || path == "/fluid.png" {
			t.Fatalf("requested a non-icon rel: %q", path)
		}
	}
}

// "shortcut icon" is a candidate because of its icon token. A bare "shortcut"
// declares nothing, which is also how Chromium reads it.
func TestFetchIgnoresABareShortcutRel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="shortcut" href="/not-an-icon.png"></head></html>`))
		case "/favicon.ico":
			w.Write(icoBytes)
		case "/not-an-icon.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if result.Light.MIMEType != "image/x-icon" {
		t.Fatalf("mime = %q, want the /favicon.ico fallback", result.Light.MIMEType)
	}
}

// Two unsized icons both score 1.0, and Document::IconURLs is built so that
// "icons seen later take precedence".
func TestFetchPrefersTheLastDeclaredIconOnATie(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="icon" href="/first.png">
				<link rel="icon" href="/last.png">
			</head></html>`))
		case "/last.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, _ = fetch(t, server, Conditional{}); requested[1] != "/last.png" {
		t.Fatalf("first icon request = %q, want /last.png", requested[1])
	}
}

// A declared rel=icon outranks an apple-touch-icon however large the latter is.
// Chromium's desktop handler will not download a touch icon at all; this package
// keeps it as a lower tier so a service that ships nothing else still gets one.
func TestFetchPrefersAnyFaviconOverATouchIcon(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="apple-touch-icon" sizes="180x180" href="/touch.png">
				<link rel="icon" sizes="16x16" href="/small.png">
			</head></html>`))
		case "/small.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, _ = fetch(t, server, Conditional{}); requested[1] != "/small.png" {
		t.Fatalf("first icon request = %q, want /small.png", requested[1])
	}
}

func TestFetchFallsBackToATouchIconWhenNoFaviconIsDeclared(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="apple-touch-icon" href="/touch.png"></head></html>`))
		case "/touch.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if !strings.HasSuffix(result.Light.IconURL, "/touch.png") {
		t.Fatalf("icon URL = %q, want /touch.png", result.Light.IconURL)
	}
}

// Serving an HTML login page from /favicon.ico with a 200 is common enough that
// trusting the status code alone would cache markup as an image.
func TestFetchRejectsHTMLServedAsAnIcon(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/x-icon")
		w.Write(htmlPage)
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if result.Status != StatusNone {
		t.Fatalf("status = %q, want %q", result.Status, StatusNone)
	}
	if len(result.Light.Bytes) != 0 {
		t.Fatal("HTML was cached as icon bytes")
	}
}

func TestFetchReportsNoneWhenEverythingIs404(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	result, notModified := fetch(t, server, Conditional{})
	if notModified {
		t.Fatal("notModified = true with no prior icon")
	}
	if result.Status != StatusNone {
		t.Fatalf("status = %q, want %q", result.Status, StatusNone)
	}
}

func TestFetchReportsNotModifiedForTheStoredIconURL(t *testing.T) {
	var conditionalSeen string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="icon" href="/brand.png"></head></html>`))
		case "/brand.png":
			conditionalSeen = r.Header.Get("If-None-Match")
			if conditionalSeen == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, notModified := fetch(t, server, Conditional{Light: Validators{IconURL: server.URL + "/brand.png", ETag: `"v1"`}})
	if !notModified {
		t.Fatalf("notModified = false, want true (status %q)", result.Status)
	}
	if conditionalSeen != `"v1"` {
		t.Fatalf("If-None-Match = %q, want \"v1\"", conditionalSeen)
	}
}

// Validators belong to the URL that issued them. Replaying them against a
// different candidate could return a 304 about an image we never stored.
func TestFetchDoesNotSendValidatorsToADifferentCandidate(t *testing.T) {
	var iconRequest *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="icon" href="/new.png"></head></html>`))
		case "/new.png":
			iconRequest = r
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if _, notModified := fetch(t, server, Conditional{Light: Validators{IconURL: server.URL + "/old.png", ETag: `"v1"`}}); notModified {
		t.Fatal("notModified = true for a candidate the validators were not issued for")
	}
	if got := iconRequest.Header.Get("If-None-Match"); got != "" {
		t.Fatalf("If-None-Match = %q, want it omitted", got)
	}
}

func TestFetchRejectsAnOversizedIcon(t *testing.T) {
	oversized := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x04}, maxIconBytes)...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte("<html><head></head></html>"))
			return
		}
		w.Write(oversized)
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if result.Status != StatusNone {
		t.Fatalf("status = %q, want %q for an oversized icon", result.Status, StatusNone)
	}
}

func TestFetchGivesUpOnARedirectLoop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if result.Status != StatusNone {
		t.Fatalf("status = %q, want %q", result.Status, StatusNone)
	}
}

func TestFetchIgnoresNonIconLinkTags(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="stylesheet" href="/app.css">
				<link rel="preconnect" href="/cdn">
			</head></html>`))
		case "/favicon.ico":
			w.Write(gifBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if result.Light.MIMEType != "image/gif" {
		t.Fatalf("mime = %q, want image/gif", result.Light.MIMEType)
	}
	for _, path := range requested {
		if path == "/app.css" || path == "/cdn" {
			t.Fatalf("requested a non-icon link: %q", path)
		}
	}
}

// The declaration form sites actually use for a scheme-aware favicon. Both
// icons are downloaded and both are kept, because the app cannot know which
// scheme it will be rendering under when the sweep runs.
func TestFetchStoresBothIconsWhenTheSchemesDiffer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="icon" href="/light.png" media="(prefers-color-scheme: light)">
				<link rel="icon" href="/dark.gif" media="(prefers-color-scheme: dark)">
			</head></html>`))
		case "/light.png":
			w.Write(pngBytes)
		case "/dark.gif":
			w.Write(gifBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if !strings.HasSuffix(result.Light.IconURL, "/light.png") {
		t.Fatalf("light icon = %q, want /light.png", result.Light.IconURL)
	}
	if result.Dark == nil {
		t.Fatal("Dark is nil, want the dark-scheme icon")
	}
	if !strings.HasSuffix(result.Dark.IconURL, "/dark.gif") {
		t.Fatalf("dark icon = %q, want /dark.gif", result.Dark.IconURL)
	}
	if result.Dark.MIMEType != "image/gif" {
		t.Fatalf("dark mime = %q, want image/gif", result.Dark.MIMEType)
	}
}

// The overwhelmingly common shape. A site that draws no distinction must cost
// exactly one icon request, not two.
func TestFetchMakesNoSecondRequestWhenTheSchemesAgree(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="icon" href="/brand.png"></head></html>`))
		case "/brand.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if result.Dark != nil {
		t.Fatalf("Dark = %q, want nil when one icon serves both schemes", result.Dark.IconURL)
	}
	if len(requested) != 2 {
		t.Fatalf("requests = %v, want the document and one icon", requested)
	}
}

// A scheme-neutral icon declared alongside a dark override serves the light
// scheme, so only the dark half is a separate image.
func TestFetchPairsAnUnqualifiedIconWithADarkOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="icon" href="/brand.png">
				<link rel="icon" href="/brand-dark.gif" media="(prefers-color-scheme: dark)">
			</head></html>`))
		case "/brand.png":
			w.Write(pngBytes)
		case "/brand-dark.gif":
			w.Write(gifBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if !strings.HasSuffix(result.Light.IconURL, "/brand.png") {
		t.Fatalf("light icon = %q, want /brand.png", result.Light.IconURL)
	}
	if result.Dark == nil || !strings.HasSuffix(result.Dark.IconURL, "/brand-dark.gif") {
		t.Fatalf("dark icon = %+v, want /brand-dark.gif", result.Dark)
	}
}

// A dark override that will not download must not cost the origin its icon.
func TestFetchKeepsTheLightIconWhenTheDarkOneIsUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="icon" href="/brand.png">
				<link rel="icon" href="/missing.png" media="(prefers-color-scheme: dark)">
			</head></html>`))
		case "/brand.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if result.Status != StatusOK || !strings.HasSuffix(result.Light.IconURL, "/brand.png") {
		t.Fatalf("light icon = %q (status %q), want /brand.png", result.Light.IconURL, result.Status)
	}
	if result.Dark != nil {
		t.Fatalf("Dark = %q, want nil when the declared dark icon 404s", result.Dark.IconURL)
	}
}

func TestFetchReportsNotModifiedOnlyWhenBothVariantsRevalidate(t *testing.T) {
	darkETag := `"dark-v1"`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head>
				<link rel="icon" href="/light.png">
				<link rel="icon" href="/dark.gif" media="(prefers-color-scheme: dark)">
			</head></html>`))
		case "/light.png":
			if r.Header.Get("If-None-Match") == `"light-v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Write(pngBytes)
		case "/dark.gif":
			if r.Header.Get("If-None-Match") == darkETag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"dark-v2"`)
			w.Write(gifBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prior := Conditional{
		Light: Validators{IconURL: server.URL + "/light.png", ETag: `"light-v1"`},
		Dark:  Validators{IconURL: server.URL + "/dark.gif", ETag: darkETag},
	}

	if _, notModified := fetch(t, server, prior); !notModified {
		t.Fatal("notModified = false when both variants revalidated")
	}

	// Now only the dark icon has moved. The record is written whole, so the
	// revalidated light half has to come back with bytes rather than empty.
	darkETag = `"dark-v0"`
	result, notModified := fetch(t, server, prior)
	if notModified {
		t.Fatal("notModified = true when the dark icon changed")
	}
	if len(result.Light.Bytes) == 0 {
		t.Fatal("light bytes are empty after a mixed revalidation")
	}
	if result.Dark == nil || len(result.Dark.Bytes) == 0 {
		t.Fatal("dark bytes are empty after a mixed revalidation")
	}
}

// An origin that drops its dark icon has changed, even though the light icon it
// still declares revalidates cleanly.
func TestFetchReportsAChangeWhenADarkIconDisappears(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="icon" href="/brand.png"></head></html>`))
		case "/brand.png":
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, notModified := fetch(t, server, Conditional{
		Light: Validators{IconURL: server.URL + "/brand.png", ETag: `"v1"`},
		Dark:  Validators{IconURL: server.URL + "/gone.png", ETag: `"v1"`},
	})
	if notModified {
		t.Fatal("notModified = true after the dark icon stopped being declared")
	}
	if result.Dark != nil {
		t.Fatalf("Dark = %q, want nil", result.Dark.IconURL)
	}
	if len(result.Light.Bytes) == 0 {
		t.Fatal("light bytes are empty, so the rewritten record would drop the icon")
	}
}

// A media query naming a feature this package cannot evaluate keeps its icon as
// a candidate. A worse icon beats no icon for a decorative cache.
func TestFetchKeepsIconsWithUnevaluableMediaQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="icon" href="/brand.png" media="(min-width: 500px)"></head></html>`))
		case "/brand.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, _ := fetch(t, server, Conditional{})
	if !strings.HasSuffix(result.Light.IconURL, "/brand.png") {
		t.Fatalf("icon URL = %q, want /brand.png", result.Light.IconURL)
	}
	if result.Dark != nil {
		t.Fatalf("Dark = %q, want nil", result.Dark.IconURL)
	}
}

// Chromium sends *.localhost to loopback without asking DNS, which on macOS
// would answer "no such host".
func TestFetchReachesLocalhostNamesOnLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte(`<html><head><link rel="icon" href="/brand.png"></head></html>`))
		case "/brand.png":
			w.Write(pngBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	origin := strings.Replace(server.URL, "127.0.0.1", "app.example.localhost", 1)
	result, _, err := Fetch(context.Background(), testClient(), origin, Conditional{})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if result.Status != StatusOK || result.Light.IconURL != origin+"/brand.png" {
		t.Fatalf("result = %q %q, want the icon at %s/brand.png", result.Status, result.Light.IconURL, origin)
	}
}

// A site that is down says nothing about its icon. StatusNone would be cached
// for weeks, and would replace the icon the caller already has.
func TestFetchFailsWhenTheSiteDoesNotAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer server.Close()
	if _, _, err := Fetch(context.Background(), testClient(), server.URL, Conditional{}); err == nil {
		t.Error("a 502 everywhere was not an error")
	}
	server.Close()
	if _, _, err := Fetch(context.Background(), testClient(), server.URL, Conditional{}); err == nil {
		t.Error("a refused connection was not an error")
	}
}

// A sign-in wall is an answer: the site is up, and has no icon for strangers.
func TestFetchReportsNoneBehindHTTPAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="router"`)
		http.Error(w, "sign in", http.StatusUnauthorized)
	}))
	defer server.Close()
	result, _ := fetch(t, server, Conditional{})
	if result.Status != StatusNone {
		t.Errorf("status = %q, want %q", result.Status, StatusNone)
	}
}
