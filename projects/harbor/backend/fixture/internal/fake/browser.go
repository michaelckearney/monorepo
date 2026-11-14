// Package fake is a browser for the apps' tests. It uses the real host
// names, so cookies' domains and Origin work as in Chromium, and sends each
// host's requests to the handler serving it.
package fake

import (
	"context"
	"crypto/tls"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

type Browser struct {
	t      *testing.T
	host   string // where paths go
	sites  *sync.Map
	Client *http.Client
	// Header is sent with every request.
	Header http.Header
}

// New returns a browser whose paths go to host, served by h.
func New(t *testing.T, host string, h http.Handler) *Browser {
	sites := &sync.Map{}
	jar, _ := cookiejar.New(nil)
	dialer := &net.Dialer{}
	client := &http.Client{Jar: jar, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			s, ok := sites.Load(host)
			if !ok {
				t.Fatalf("no handler for %s", host)
			}
			return dialer.DialContext(ctx, network, s.(*httptest.Server).Listener.Addr().String())
		},
	}}
	b := &Browser{t: t, host: host, sites: sites, Client: client, Header: http.Header{}}
	b.Serve(host, h)
	return b
}

// Serve serves another host to this browser.
func (b *Browser) Serve(host string, h http.Handler) {
	s := httptest.NewTLSServer(h)
	b.t.Cleanup(s.Close)
	b.sites.Store(host, s)
}

// At returns the same browser (cookies and all), with paths going to host.
func (b *Browser) At(host string) *Browser {
	c := *b
	c.host = host
	c.Header = b.Header.Clone()
	return &c
}

// Page is a response, after redirects.
type Page struct {
	Status int
	URL    *url.URL
	Path   string // where it ended up, on its host
	Body   string
	Header http.Header
}

// Do sends a request. target is a path on the browser's host, or a URL.
func (b *Browser) Do(method, target string, body io.Reader, header http.Header) Page {
	b.t.Helper()
	if strings.HasPrefix(target, "/") {
		target = hosts.URL(b.host) + target
	}
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		b.t.Fatal(err)
	}
	for k, v := range b.Header {
		req.Header[k] = v
	}
	for k, v := range header {
		req.Header[k] = v
	}
	if method != http.MethodGet && req.Header.Get("Origin") == "" {
		req.Header.Set("Origin", hosts.URL(b.host))
	}
	resp, err := b.Client.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return Page{Status: resp.StatusCode, URL: resp.Request.URL, Path: resp.Request.URL.RequestURI(), Body: string(data), Header: resp.Header}
}

func (b *Browser) Get(target string) Page {
	b.t.Helper()
	return b.Do(http.MethodGet, target, nil, nil)
}

// Post sends a form.
func (b *Browser) Post(target string, form url.Values) Page {
	b.t.Helper()
	return b.Do(http.MethodPost, target, strings.NewReader(form.Encode()),
		http.Header{"Content-Type": {"application/x-www-form-urlencoded"}})
}

// JSON sends a JSON body.
func (b *Browser) JSON(method, target, body string, header http.Header) Page {
	b.t.Helper()
	h := http.Header{"Content-Type": {"application/json"}}
	for k, v := range header {
		h[k] = v
	}
	return b.Do(method, target, strings.NewReader(body), h)
}

// Cookie returns the value of a cookie the browser would send to its host,
// or "".
func (b *Browser) Cookie(name string) string {
	u, _ := url.Parse(hosts.URL(b.host) + "/")
	for _, c := range b.Client.Jar.Cookies(u) {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// Field returns the value of a form field in a page: name="x" value="y".
func Field(body, name string) string {
	i := strings.Index(body, `name="`+name+`"`)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	j := strings.Index(rest, `value="`)
	if j < 0 || j > 200 {
		return ""
	}
	rest = rest[j+len(`value="`):]
	// As a browser reads it: html/template escapes some characters, as
	// base64's + in a SAML request.
	return html.UnescapeString(rest[:strings.IndexByte(rest, '"')])
}

// Store is a store of the fixtures' own for one test.
func Store(t *testing.T) *site.Store {
	s, err := site.OpenStore(filepath.Join(t.TempDir(), "fixtures.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
