// Package favicon discovers and downloads a site's favicon over plain HTTP.
//
// It exists so Harbor can show each service's icon without asking you to
// set anything up. That purpose sets the whole error policy: every
// failure here is ordinary, none of it is worth reporting to a person, and the
// only consequence of giving up is that a placeholder glyph renders instead.
//
// The engine's browser is deliberately not involved. It costs a Chromium
// session, and the overwhelming majority of origins — including self-hosted
// services behind a login — serve their icon to an unauthenticated GET.
//
// Not involving Chromium does not mean disagreeing with it. Which <link> counts
// as an icon, and which of several wins, follow Chromium's desktop favicon path
// (Document::IconURLs plus FaviconHandler's NON_TOUCH_16_DIP handler) closely
// enough that the icon here is the icon in the browser's tab. Where this package
// deviates, rank.go and the comments below say so and why.
package favicon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture"
)

// Status reports what a completed lookup found.
type Status string

const (
	// StatusOK means Result carries renderable bytes.
	StatusOK Status = "ok"
	// StatusNone means the origin answered but offers no usable icon. It is a
	// successful outcome: caching it is what stops the sweep from asking again
	// on every launch.
	StatusNone Status = "none"
)

const (
	// maxIconBytes caps a single download. Favicons run to a few KB; the largest
	// legitimate outlier is an apple-touch-icon, which stays well inside this.
	maxIconBytes = 256 << 10
	// maxHTMLBytes caps the document scanned for <link> tags. Icon declarations
	// live in <head>, so reading further only buys the parser more work.
	maxHTMLBytes = 512 << 10
	maxRedirects = 5
	// maxCandidates bounds how many declared icons are attempted. A page that
	// lists thirty sizes should not become thirty requests.
	maxCandidates = 6
)

// Validators are the conditional-request headers stored alongside one cached
// image. They belong to the URL they were issued for, which is why IconURL
// travels with them.
type Validators struct {
	IconURL      string
	ETag         string
	LastModified string
}

// Conditional carries the validators stored from a previous successful fetch so
// a refresh can come back as a 304. Dark is zero for the ordinary origin that
// declares one icon for both color schemes.
type Conditional struct {
	Light Validators
	Dark  Validators
}

// Icon is one downloaded image.
type Icon struct {
	IconURL      string
	MIMEType     string
	Bytes        []byte
	ETag         string
	LastModified string
}

// Result is a completed lookup.
type Result struct {
	Status Status
	// Light is the icon to render by default: what the origin resolves to under
	// prefers-color-scheme: light, which for almost every site is simply "the
	// favicon". It carries bytes whenever Status is StatusOK.
	Light Icon
	// Dark is set only when the origin declares an icon that resolves to
	// different bytes under prefers-color-scheme: dark. A nil Dark means "render
	// Light in both schemes", not "the dark icon is missing".
	Dark *Icon
}

// NewClient builds the HTTP client Fetch expects: redirects bounded, no cookie
// jar, and no TLS leniency. A self-hosted service on a self-signed certificate
// takes the placeholder rather than an exception.
//
// It sends localhost and every name under .localhost to the loopback address
// itself, as Chromium does. macOS does not resolve *.localhost, so without this
// a service at https://app.localhost works in Harbor's browser and has no icon.
// It reaches the fixture sites where this process serves them, as Harbor's
// browser does, and trusts their certificates.
func NewClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, port, err := net.SplitHostPort(addr); err == nil && isLocalhost(host) {
			addr = net.JoinHostPort("127.0.0.1", port)
		}
		return dial(ctx, network, addr)
	}
	return &http.Client{
		Transport: fixture.Transport(transport),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("refusing non-http redirect")
			}
			return nil
		},
	}
}

// isLocalhost reports whether Chromium resolves host to loopback without
// asking DNS (net::IsLocalHostname).
func isLocalhost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// Fetch resolves the icon or icons for an origin produced by Origin.
//
// notModified is true when every image the origin currently resolves to was
// revalidated against what the caller already has, in which case Result carries
// no bytes and the caller should keep its copy untouched.
func Fetch(ctx context.Context, client *http.Client, origin string, prior Conditional) (result Result, notModified bool, err error) {
	if client == nil {
		client = NewClient()
	}
	base, err := url.Parse(origin)
	if err != nil {
		return Result{}, false, err
	}

	lightCandidates, darkCandidates, unanswered := discover(ctx, client, base)

	light, lightUnchanged, ok := pick(ctx, client, lightCandidates, prior.Light)
	if !ok && unanswered != nil {
		// The site is down or unreachable, which says nothing about its icon.
		// Reporting StatusNone here would have the caller cache "no icon" for a
		// site that was only offline, and replace an icon it already had.
		return Result{}, false, fmt.Errorf("the site did not answer: %w", unanswered)
	}
	if !ok {
		// Every candidate was tried and none produced an image. That is an
		// answer, not a failure: the caller caches it.
		return Result{Status: StatusNone}, false, nil
	}

	dark, darkUnchanged := resolveDark(ctx, client, lightCandidates, darkCandidates, light, prior.Dark)

	// A prior dark icon that the origin has stopped declaring is a real change,
	// even though nothing that is still declared moved.
	darkDropped := dark == nil && prior.Dark.IconURL != ""
	if lightUnchanged && darkUnchanged && !darkDropped {
		return Result{}, true, nil
	}

	// One variant revalidated while the other changed. The record is written
	// whole, so the unchanged half has to be re-read to have bytes at all. This
	// costs one small GET, and only on the sweep where a site changed one of its
	// two icons — the alternative is teaching every caller to splice a partial
	// result over its stored copy.
	if lightUnchanged {
		refreshed, _, ok := pick(ctx, client, []string{light.IconURL}, Validators{})
		if !ok {
			return Result{}, false, errors.New("revalidated icon could not be re-read")
		}
		light = refreshed
	}
	if dark != nil && darkUnchanged {
		refreshed, _, ok := pick(ctx, client, []string{dark.IconURL}, Validators{})
		if !ok {
			return Result{}, false, errors.New("revalidated icon could not be re-read")
		}
		dark = &refreshed
	}

	return Result{Status: StatusOK, Light: light, Dark: dark}, false, nil
}

// resolveDark downloads the dark-scheme icon when the origin declares a
// different one, and reports whether it revalidated.
//
// The common case costs nothing: a site with no color-scheme media queries
// produces identical candidate lists, so no second request is made at all.
func resolveDark(
	ctx context.Context,
	client *http.Client,
	lightCandidates, darkCandidates []string,
	light Icon,
	prior Validators,
) (*Icon, bool) {
	if equalCandidates(lightCandidates, darkCandidates) {
		// Nothing distinguishes the schemes. Reporting "unchanged" is right
		// whether or not the caller stored a dark icon before — a dropped one is
		// caught by the darkDropped check in Fetch.
		return nil, true
	}
	dark, unchanged, ok := pick(ctx, client, darkCandidates, prior)
	if !ok {
		// The declared dark icon would not download. Rendering the light one in
		// both schemes is a better outcome than the placeholder.
		return nil, true
	}
	if !unchanged && dark.IconURL == light.IconURL {
		// Different declarations, same image after redirects. Storing it twice
		// would double the row for no visible difference.
		return nil, true
	}
	return &dark, unchanged
}

func equalCandidates(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// discover returns the icon URLs to try for each color scheme, best first,
// always ending with /favicon.ico so an origin that declares nothing — or
// declares only icons that will not download — still has one shot.
//
// Chromium synthesizes /favicon.ico only when no rel=icon was declared at all.
// Keeping it as a universal last resort is deliberate: a declared icon that
// 404s should fall through to the conventional path rather than to the
// placeholder.
//
// unanswered is why the site's page got no response short of a server
// error, or nil when it did, such as with a 200 or a 401. A lookup that
// finds nothing can only say the site has no icon when it is nil.
func discover(ctx context.Context, client *http.Client, base *url.URL) (light, dark []string, unanswered error) {
	fallback := base.JoinPath("favicon.ico").String()

	document, documentURL, answered, err := fetchHTML(ctx, client, base)
	if err != nil {
		if answered {
			err = nil
		}
		return []string{fallback}, []string{fallback}, err
	}

	links := parseIconLinks(document, documentURL)
	return candidatesFor(links, false, fallback), candidatesFor(links, true, fallback), nil
}

func fetchHTML(ctx context.Context, client *http.Client, base *url.URL) ([]byte, *url.URL, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return nil, nil, false, err
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	response, err := client.Do(request)
	if err != nil {
		// A response comes with the error only when a redirect was refused,
		// as in a loop: the site answered, and will not give a page.
		return nil, nil, response != nil, err
	}
	defer response.Body.Close()
	answered := response.StatusCode < http.StatusInternalServerError
	if response.StatusCode != http.StatusOK {
		return nil, nil, answered, errors.New("unexpected status " + response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxHTMLBytes))
	if err != nil {
		return nil, nil, false, err
	}
	// Relative hrefs resolve against the document that declared them, which is
	// wherever the redirect chain ended rather than where it started.
	return body, response.Request.URL, true, nil
}

type iconLink struct {
	href    string
	rel     iconRel
	score   float64
	order   int
	schemes colorSchemes
}

// parseIconLinks pulls icon declarations out of a document, in document order.
//
// Chromium looks only at <link> elements that are direct children of <head>.
// This scans every <link> until <body> starts, which is looser: a head wrapped
// in a template or emitted by a framework still yields its icons, and the extra
// elements a stricter walk would exclude are ones a browser would also have
// found in practice.
func parseIconLinks(document []byte, documentURL *url.URL) []iconLink {
	tokenizer := html.NewTokenizer(bytes.NewReader(document))
	var links []iconLink
	order := 0
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return links
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := tokenizer.TagName()
			if string(name) == "body" {
				// Icon declarations belong to <head>. Once the body starts,
				// continuing only scans markup that cannot contribute.
				return links
			}
			if string(name) != "link" || !hasAttr {
				continue
			}
			attributes := linkAttributes(tokenizer)
			rel, isIcon := relFor(attributes.rel)
			if !isIcon || strings.TrimSpace(attributes.href) == "" {
				continue
			}
			resolved, err := documentURL.Parse(strings.TrimSpace(attributes.href))
			if err != nil || (resolved.Scheme != "http" && resolved.Scheme != "https") {
				continue
			}
			schemes := matchColorSchemes(attributes.media)
			if !schemes.light && !schemes.dark {
				// A media query that can never match is not a candidate under
				// either scheme, which is what Chromium's matches() check does.
				continue
			}
			sizes, hasAny := parseIconSizes(attributes.sizes)
			order++
			links = append(links, iconLink{
				href:    resolved.String(),
				rel:     rel,
				score:   scoreSizes(sizes, hasAny),
				order:   order,
				schemes: schemes,
			})
		}
	}
}

// candidatesFor orders the links that apply to one color scheme, best first.
//
// The comparison is Chromium's, in three keys. Kind comes first: the desktop
// handler downloads only rel=icon, and apple-touch-icon is an Android-only type
// there. Keeping it as a lower tier is this package's one ranking deviation —
// a self-hosted service often ships an apple-touch-icon and nothing else, and
// falling to it beats falling to /favicon.ico. Score comes next, from the sizes
// attribute alone. Ties break toward the last declaration, because
// Document::IconURLs is written "so that icons seen later take precedence".
func candidatesFor(links []iconLink, dark bool, fallback string) []string {
	matched := make([]iconLink, 0, len(links))
	for _, link := range links {
		if link.schemes.matches(dark) {
			matched = append(matched, link)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].rel != matched[j].rel {
			return matched[i].rel < matched[j].rel
		}
		if matched[i].score != matched[j].score {
			return matched[i].score > matched[j].score
		}
		return matched[i].order > matched[j].order
	})

	seen := map[string]struct{}{}
	candidates := make([]string, 0, len(matched)+1)
	for _, link := range matched {
		if _, ok := seen[link.href]; ok {
			continue
		}
		seen[link.href] = struct{}{}
		candidates = append(candidates, link.href)
		if len(candidates) == maxCandidates {
			break
		}
	}
	if _, ok := seen[fallback]; !ok {
		candidates = append(candidates, fallback)
	}
	return candidates
}

type linkAttrs struct {
	rel   string
	href  string
	sizes string
	media string
}

func linkAttributes(tokenizer *html.Tokenizer) linkAttrs {
	var attributes linkAttrs
	for {
		key, value, more := tokenizer.TagAttr()
		switch strings.ToLower(string(key)) {
		case "rel":
			attributes.rel = string(value)
		case "href":
			attributes.href = string(value)
		case "sizes":
			attributes.sizes = string(value)
		case "media":
			attributes.media = string(value)
		}
		if !more {
			return attributes
		}
	}
}

// pick downloads the first candidate that yields an image, reporting whether
// the origin answered 304 for the copy the caller already holds.
func pick(ctx context.Context, client *http.Client, candidates []string, prior Validators) (Icon, bool, bool) {
	for _, candidate := range candidates {
		// Validators only apply to the URL they were issued for. Sending them
		// with a different candidate would invite a 304 about the wrong image.
		conditional := Validators{}
		if prior.IconURL != "" && candidate == prior.IconURL {
			conditional = prior
		}
		icon, unchanged, err := download(ctx, client, candidate, conditional)
		if err != nil {
			continue
		}
		if unchanged {
			// A 304 carries no body, so the URL is all this can report; the
			// caller's stored bytes are the ones still in force.
			return Icon{IconURL: candidate}, true, true
		}
		return icon, false, true
	}
	return Icon{}, false, false
}

func download(ctx context.Context, client *http.Client, candidate string, conditional Validators) (Icon, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate, nil)
	if err != nil {
		return Icon{}, false, err
	}
	request.Header.Set("Accept", "image/*")
	if conditional.ETag != "" {
		request.Header.Set("If-None-Match", conditional.ETag)
	}
	if conditional.LastModified != "" {
		request.Header.Set("If-Modified-Since", conditional.LastModified)
	}
	response, err := client.Do(request)
	if err != nil {
		return Icon{}, false, err
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotModified {
		return Icon{}, true, nil
	}
	if response.StatusCode != http.StatusOK {
		return Icon{}, false, errors.New("unexpected status " + response.Status)
	}

	// One byte past the cap distinguishes "exactly at the limit" from "truncated",
	// so an oversized image is rejected rather than stored half-read.
	body, err := io.ReadAll(io.LimitReader(response.Body, maxIconBytes+1))
	if err != nil {
		return Icon{}, false, err
	}
	if len(body) > maxIconBytes {
		return Icon{}, false, errors.New("icon exceeds size limit")
	}

	// The declared Content-Type is not evidence. Servers routinely answer
	// /favicon.ico with a 200 login page, and taking their word for it would
	// cache HTML as an image and render a broken slot forever.
	mimeType, ok := sniff(body)
	if !ok {
		return Icon{}, false, errors.New("response is not an image")
	}

	return Icon{
		IconURL:      response.Request.URL.String(),
		MIMEType:     mimeType,
		Bytes:        body,
		ETag:         response.Header.Get("ETag"),
		LastModified: response.Header.Get("Last-Modified"),
	}, false, nil
}

// sniff identifies image bytes by their own header. The formats listed are the
// ones a browser will render in an <img>, which is the only place these end up.
func sniff(body []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", true
	case bytes.HasPrefix(body, []byte("GIF87a")), bytes.HasPrefix(body, []byte("GIF89a")):
		return "image/gif", true
	case bytes.HasPrefix(body, []byte("\xff\xd8\xff")):
		return "image/jpeg", true
	case bytes.HasPrefix(body, []byte("\x00\x00\x01\x00")):
		return "image/x-icon", true
	case bytes.HasPrefix(body, []byte("\x00\x00\x02\x00")):
		return "image/x-icon", true
	case bytes.HasPrefix(body, []byte("RIFF")) && len(body) >= 12 && bytes.Equal(body[8:12], []byte("WEBP")):
		return "image/webp", true
	case isSVG(body):
		return "image/svg+xml", true
	}
	return "", false
}

// isSVG looks for an <svg> element near the start, allowing for a leading XML
// declaration, doctype, comment, or BOM.
func isSVG(body []byte) bool {
	head := body
	if len(head) > 1024 {
		head = head[:1024]
	}
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	lowered := bytes.ToLower(bytes.TrimSpace(head))
	if !bytes.HasPrefix(lowered, []byte("<?xml")) &&
		!bytes.HasPrefix(lowered, []byte("<!doctype svg")) &&
		!bytes.HasPrefix(lowered, []byte("<!--")) &&
		!bytes.HasPrefix(lowered, []byte("<svg")) {
		return false
	}
	return bytes.Contains(lowered, []byte("<svg"))
}
