// Package fixture serves the fixture sites: made-up websites that Harbor is
// tested against, and that you use Harbor with in dev mode. See README.md.
//
// The sites run in Harbor's own process, from Start to Stop: the dev app
// starts them at launch, and a benchmark run from the CLI starts its own.
// Every site is at https://<name>.localhost:8443. They listen on that port
// when it is free, and on another otherwise, as for a second worktree's
// Harbor; Harbor's browsers (Browser) and Go clients (Transport) send the
// URLs there, so the URLs in the seed data and tests never change.
package fixture

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/mail"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// Port is the port in every fixture site's URL.
const Port = hosts.Port

// Fixtures is the fixture sites, serving.
type Fixtures struct {
	addr  string
	ca    *x509.Certificate
	caPEM []byte
	store *site.Store
	srv   *http.Server
}

var (
	mu sync.Mutex
	// running is the fixtures this process serves, or nil.
	running *Fixtures
)

// Start serves every fixture site, until Stop, with their store in dir:
// their sessions, the tokens that stand for a sign-in, their keys, and
// their CA. Two processes may share dir, as Harbor and a benchmark do.
func Start(dir string) (*Fixtures, error) {
	mu.Lock()
	defer mu.Unlock()
	if running != nil {
		return nil, errors.New("the fixture sites are serving in this process already")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	store, err := site.OpenStore(filepath.Join(dir, "fixtures.db"))
	if err != nil {
		return nil, err
	}
	ca, signer, err := authority(store)
	if err != nil {
		store.Close()
		return nil, err
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+Port)
	if err != nil {
		if l, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			store.Close()
			return nil, err
		}
	}
	f := &Fixtures{addr: l.Addr().String(), ca: ca, caPEM: pemCertificate(ca), store: store}
	sites := sites(store, &mail.Box{}, site.Client(f.transport(http.DefaultTransport.(*http.Transport))), f.caPEM)
	f.srv = &http.Server{
		Handler:           logged(route(sites)),
		TLSConfig:         &tls.Config{GetCertificate: signer.certificate},
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(os.Stderr, "fixtures: ", log.LstdFlags),
	}
	go f.srv.ServeTLS(l, "", "")
	running = f
	log.Printf("fixtures: serving every site at https://<name>.localhost:%s, on %s; see %s", Port, f.addr, hosts.URL(hosts.Index))
	return f, nil
}

// Stop stops serving, and closes the store.
func (f *Fixtures) Stop() {
	mu.Lock()
	defer mu.Unlock()
	// A WebSocket or an event stream does not end on its own, so a shutdown
	// that waits for every connection could wait for ever.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if f.srv.Shutdown(ctx) != nil {
		f.srv.Close()
	}
	f.store.Close()
	if running == f {
		running = nil
	}
}

func current() *Fixtures {
	mu.Lock()
	defer mu.Unlock()
	return running
}

// Transport returns base changed to reach the fixture sites while this
// process serves them: it sends each fixture URL's connection where they
// listen, and trusts their CA as well as the system's. Otherwise it returns
// base as it is.
func Transport(base *http.Transport) *http.Transport {
	f := current()
	if f == nil {
		return base
	}
	return f.transport(base)
}

func (f *Fixtures) transport(base *http.Transport) *http.Transport {
	t := base.Clone()
	if t.TLSClientConfig == nil {
		t.TLSClientConfig = &tls.Config{}
	} else {
		t.TLSClientConfig = t.TLSClientConfig.Clone()
	}
	roots := t.TLSClientConfig.RootCAs
	if roots == nil {
		var err error
		if roots, err = x509.SystemCertPool(); err != nil {
			// Never on macOS or Linux, where Harbor runs.
			panic(err)
		}
	}
	roots = roots.Clone()
	roots.AddCert(f.ca)
	t.TLSClientConfig.RootCAs = roots
	dial := t.DialContext
	if dial == nil {
		dial = (&net.Dialer{Timeout: 30 * time.Second}).DialContext
	}
	t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, port, err := net.SplitHostPort(addr); err == nil && port == Port && local(host) {
			addr = f.addr
		}
		return dial(ctx, network, addr)
	}
	return t
}

// Browser is what a browser.Options needs to reach the fixture sites while
// this process serves them: the rules that send their URLs where they
// listen, and their CA. Both are nil otherwise.
func Browser() (hostRules []string, cas []*x509.Certificate) {
	f := current()
	if f == nil {
		return nil, nil
	}
	return []string{
		fmt.Sprintf("MAP *.localhost:%s %s", Port, f.addr),
		fmt.Sprintf("MAP localhost:%s %s", Port, f.addr),
	}, []*x509.Certificate{f.ca}
}

// local reports whether host is localhost or a name under it.
func local(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// route sends each request to its host's site.
func route(sites map[string]http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := strings.Cut(r.Host, ":")
		h := sites[host]
		if h == nil {
			http.Error(w, "No fixture site is called "+host+". See "+hosts.URL(hosts.Index), http.StatusNotFound)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// logged logs each request: host, method, path, status, and time taken.
func logged(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &status{ResponseWriter: w, code: 200}
		h.ServeHTTP(rec, r)
		host, _, _ := strings.Cut(r.Host, ":")
		log.Printf("fixtures: %s %s %s %d %s", host, r.Method, r.URL.RequestURI(), rec.code, time.Since(start).Round(time.Millisecond))
	})
}

type status struct {
	http.ResponseWriter
	code int
}

func (s *status) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush lets server-sent events through the log wrapper.
func (s *status) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the connection, for WebSockets.
func (s *status) Unwrap() http.ResponseWriter { return s.ResponseWriter }
