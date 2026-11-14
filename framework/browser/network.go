package browser

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/domstorage"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// What a tab's pages asked the network for, and what they saved in their
// storage, and clearing what sites keep in the profile. An agent that
// writes code for a site finds its APIs by watching the requests its pages
// make, which the page's own scripts cannot see all of: a page load, a
// worker's, one sent before a script runs. And it finds where a site keeps
// its sign-in by what a sign-in saved.

// maxRequests is how many requests a tab keeps, the newest. A page that
// polls would otherwise grow the list for as long as the tab is open.
const maxRequests = 1000

// Request is one request the tab's pages made.
type Request struct {
	// ID names it to RequestBody and ResponseBody.
	ID     string `json:"id"`
	Method string `json:"method"`
	URL    string `json:"url"`
	// Type is Chromium's resource type: Document, XHR, Fetch, Script,
	// Stylesheet, Image, Font, and so on.
	Type string `json:"type"`
	// Status is 0 until the response arrives, and for a request that
	// failed.
	Status   int64  `json:"status"`
	MimeType string `json:"mimeType,omitempty"`
	// Failed is Chromium's reason, for a request that failed.
	Failed          string            `json:"failed,omitempty"`
	RequestHeaders  map[string]string `json:"requestHeaders"`
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"`
	// HasBody is set when the request sent a body, as a POST does.
	HasBody bool `json:"hasBody,omitempty"`
	// Page is the address of the page that made it; for a page load, of
	// the page it loads.
	Page string `json:"page,omitempty"`
	// Navigation is set for a load of the tab's page, and for each redirect
	// on the way to it.
	Navigation bool `json:"navigation,omitempty"`
	// At is when it was sent, to order it with what the tab's pages saved.
	At Mark `json:"-"`
}

// Mark is a moment in a tab's life, to ask what happened after it: the
// requests its pages made, and what they saved.
type Mark uint64

// Mark is now, in the tab's life. A tab opened later has seen nothing
// before any Mark: its whole life is after it.
func (t *Tab) Mark() Mark { return Mark(t.seq.Load()) }

// requestLog is a tab's requests, oldest first. A new page does not start
// it again: a sign-in goes through several pages, and what it did is in
// the requests of the pages it left.
type requestLog struct {
	mu   sync.Mutex
	list []*Request
	byID map[network.RequestID]*Request
	// redirects counts each request's redirects, to name the hops.
	redirects map[network.RequestID]int
	// page is the address of the page loading or loaded.
	page string
}

// note keeps the tab's requests, from its network events. It runs in
// Chromium's event handler.
func (l *requestLog) note(ev any, main cdp.FrameID, seq *atomic.Uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byID == nil {
		l.byID = map[network.RequestID]*Request{}
		l.redirects = map[network.RequestID]int{}
	}
	switch e := ev.(type) {
	case *network.EventRequestWillBeSent:
		if e.RedirectResponse != nil {
			// Chromium sends a redirect's next hop under the same ID: the hop
			// before keeps its response, under a name of its own.
			if r := l.byID[e.RequestID]; r != nil {
				respond(r, e.RedirectResponse)
				l.redirects[e.RequestID]++
				r.ID = fmt.Sprintf("%s-redirect-%d", e.RequestID, l.redirects[e.RequestID])
				delete(l.byID, e.RequestID)
			}
		}
		r := &Request{ID: string(e.RequestID), Method: e.Request.Method, URL: e.Request.URL + e.Request.URLFragment,
			Type: string(e.Type), RequestHeaders: headers(e.Request.Headers), HasBody: e.Request.HasPostData, At: Mark(seq.Add(1))}
		if e.Type == network.ResourceTypeDocument && e.FrameID == main {
			l.page, r.Navigation = r.URL, true
		}
		r.Page = l.page
		l.list = append(l.list, r)
		l.byID[e.RequestID] = r
		if len(l.list) > maxRequests {
			old := l.list[0]
			l.list = l.list[1:]
			if l.byID[network.RequestID(old.ID)] == old {
				delete(l.byID, network.RequestID(old.ID))
			}
		}
	case *network.EventResponseReceived:
		if r := l.byID[e.RequestID]; r != nil {
			respond(r, e.Response)
		}
	case *network.EventLoadingFailed:
		if r := l.byID[e.RequestID]; r != nil {
			r.Failed = e.ErrorText
			if e.Canceled {
				r.Failed = "canceled"
			}
		}
	}
}

func respond(r *Request, resp *network.Response) {
	r.Status, r.MimeType, r.ResponseHeaders = resp.Status, resp.MimeType, headers(resp.Headers)
}

func headers(h network.Headers) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// Requests are the requests the tab's pages made, oldest first, up to the
// newest thousand.
func (t *Tab) Requests() []Request { return t.RequestsSince(0) }

// RequestsSince are the requests the tab's pages made after m, oldest
// first.
func (t *Tab) RequestsSince(m Mark) []Request {
	t.requests.mu.Lock()
	defer t.requests.mu.Unlock()
	out := []Request{}
	for _, r := range t.requests.list {
		if r.At > m {
			out = append(out, *r)
		}
	}
	return out
}

// Saved is a change a tab's page made to its localStorage or its
// sessionStorage.
type Saved struct {
	// Origin is the site whose storage it is, such as https://example.com.
	Origin string `json:"origin"`
	// Local is set for localStorage, and not for sessionStorage.
	Local bool `json:"local"`
	// Change is "added", "changed", "removed", or "cleared", for every key.
	Change string `json:"change"`
	// Key is the key, and empty when the storage was cleared. Values are
	// left out: a sign-in's token is a secret.
	Key string `json:"key,omitempty"`
	// At is when it changed, to order it with the tab's requests.
	At Mark `json:"-"`
}

// savedLog is what the tab's pages saved, oldest first, up to the newest
// thousand.
type savedLog struct {
	mu   sync.Mutex
	list []Saved
}

// note keeps the tab's storage changes, from its DOMStorage events. It runs
// in Chromium's event handler.
func (l *savedLog) note(ev any, seq *atomic.Uint64) {
	var id *domstorage.StorageID
	var change, key string
	switch e := ev.(type) {
	case *domstorage.EventDomStorageItemAdded:
		id, change, key = e.StorageID, "added", e.Key
	case *domstorage.EventDomStorageItemUpdated:
		id, change, key = e.StorageID, "changed", e.Key
	case *domstorage.EventDomStorageItemRemoved:
		id, change, key = e.StorageID, "removed", e.Key
	case *domstorage.EventDomStorageItemsCleared:
		id, change = e.StorageID, "cleared"
	default:
		return
	}
	origin := id.SecurityOrigin
	if origin == "" {
		origin = strings.TrimSuffix(string(id.StorageKey), "/")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.list = append(l.list, Saved{Origin: origin, Local: id.IsLocalStorage, Change: change, Key: key, At: Mark(seq.Add(1))})
	if len(l.list) > maxRequests {
		l.list = l.list[1:]
	}
}

// SavedSince are the changes the tab's pages made to their storage after
// m, oldest first.
func (t *Tab) SavedSince(m Mark) []Saved {
	t.saved.mu.Lock()
	defer t.saved.mu.Unlock()
	out := []Saved{}
	for _, s := range t.saved.list {
		if s.At > m {
			out = append(out, s)
		}
	}
	return out
}

// RequestBody is the body a request sent, such as a POST's form or JSON.
func (t *Tab) RequestBody(ctx context.Context, id string) ([]byte, error) {
	var body []byte
	err := t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		body, err = network.GetRequestPostData(network.RequestID(id)).Do(ctx)
		return err
	}))
	return body, err
}

// ResponseBody is the body of a request's response, while Chromium still
// has it: a page that loaded since may have taken its place.
func (t *Tab) ResponseBody(ctx context.Context, id string) ([]byte, error) {
	var body []byte
	err := t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		body, err = network.GetResponseBody(network.RequestID(id)).Do(ctx)
		return err
	}))
	return body, err
}

// AllCookies are every cookie the profile keeps, for every site.
func (b *Browser) AllCookies(ctx context.Context) ([]Cookie, error) {
	b.mu.Lock()
	p, err := b.running()
	b.mu.Unlock()
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	var got []*network.Cookie
	err = chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		got, err = storage.GetCookies().Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		return err
	}))
	if err != nil {
		return nil, err
	}
	return cookies(got), nil
}

// CookieMark is the profile's cookies at a moment, to ask later what
// changed. Chromium tells of no cookie a page's script sets, so the
// cookies are compared.
type CookieMark struct{ values map[cookieKey]string }

type cookieKey struct{ name, domain, path string }

// CookieChange is a cookie added, changed, or removed. The value is left
// out: a sign-in's cookie is a secret.
type CookieChange struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
	// Change is "added", "changed", or "removed".
	Change string `json:"change"`
}

// CookieMark is the profile's cookies now, to ask what changed since.
func (b *Browser) CookieMark(ctx context.Context) (CookieMark, error) {
	all, err := b.AllCookies(ctx)
	if err != nil {
		return CookieMark{}, err
	}
	return cookieMark(all), nil
}

func cookieMark(all []Cookie) CookieMark {
	m := CookieMark{values: make(map[cookieKey]string, len(all))}
	for _, c := range all {
		m.values[cookieKey{c.Name, c.Domain, c.Path}] = c.Value
	}
	return m
}

// CookiesChangedSince are the cookies added, changed, and removed since m,
// in every site, by domain and then name.
func (b *Browser) CookiesChangedSince(ctx context.Context, m CookieMark) ([]CookieChange, error) {
	now, err := b.CookieMark(ctx)
	if err != nil {
		return nil, err
	}
	return cookieChanges(m, now), nil
}

func cookieChanges(before, after CookieMark) []CookieChange {
	changes := []CookieChange{}
	for k, v := range after.values {
		old, ok := before.values[k]
		switch {
		case !ok:
			changes = append(changes, CookieChange{k.name, k.domain, k.path, "added"})
		case old != v:
			changes = append(changes, CookieChange{k.name, k.domain, k.path, "changed"})
		}
	}
	for k := range before.values {
		if _, ok := after.values[k]; !ok {
			changes = append(changes, CookieChange{k.name, k.domain, k.path, "removed"})
		}
	}
	slices.SortFunc(changes, func(a, b CookieChange) int {
		return cmp.Or(strings.Compare(a.Domain, b.Domain), strings.Compare(a.Name, b.Name), strings.Compare(a.Path, b.Path))
	})
	return changes
}

// ClearData removes what the profile keeps for the site at origin, such as
// https://mail.example.com: every cookie the browser would send it, its
// parent domain's included, and its storage. Pages open on the site keep
// what they hold in memory until they load again.
//
// An empty origin removes everything: Chromium stops, closing every tab,
// and its profile is deleted. The next Open starts it again, empty.
func (b *Browser) ClearData(ctx context.Context, origin string) error {
	if origin == "" {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.halt()
		if err := os.RemoveAll(b.dir()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%q is not a site's address, such as https://example.com", origin)
	}
	origin = u.Scheme + "://" + u.Host
	b.mu.Lock()
	p, err := b.running()
	b.mu.Unlock()
	if err != nil {
		return err
	}
	// p.ctx is Chromium's first tab, which the Network and Storage domains
	// need to run in.
	runCtx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	return chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		cookies, err := network.GetCookies().WithURLs([]string{origin + "/"}).Do(ctx)
		if err != nil {
			return err
		}
		for _, c := range cookies {
			if err := network.DeleteCookies(c.Name).WithDomain(c.Domain).WithPath(c.Path).Do(ctx); err != nil {
				return err
			}
		}
		types := []string{"local_storage", "indexeddb", "cache_storage", "service_workers", "file_systems", "websql", "shared_storage", "storage_buckets"}
		return storage.ClearDataForOrigin(origin, strings.Join(types, ",")).Do(ctx)
	}))
}
