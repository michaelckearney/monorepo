// Package fixture is a made-up server and a made-up GitHub, which a dev build
// of Slipway talks to instead of the real ones: what `harness run`, the e2e
// tests, and `harness record` show, on a machine that reaches neither. See
// README.md.
//
// It runs in the app's own process, from Start to Stop, on a port of its
// own. Transport sends the app's requests for api.github.com and for
// server.fixture.localhost there, so the app's own code asks it exactly as it
// asks the real ones: over HTTP, with the same queries, reading the same
// answers.
package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Server is the made-up server's address, which a dev build starts with
// (database/dev/).
const Server = "http://server.fixture.localhost"

// Token is the GitHub token a dev build starts with. The made-up GitHub
// refuses any other, as GitHub refuses a wrong one.
const Token = "fixture-token"

// Login is whose token Token is.
const Login = "alex-morgan"

// Repo is the made-up monorepo.
const Repo = "example/monorepo"

// Attachment is a screenshot a bot attached to its comment on #13, as the
// Markdown links it: in a private repository, the link alone shows nothing without
// GitHub's credentials, and GitHub's rendering has a signed link to it.
const Attachment = "https://github.com/user-attachments/assets/0b6c2f1e-6a51-4d1e-9a3c-5d8f0e7b2a14"

const (
	githubHost = "api.github.com"
	serverHost = "server.fixture.localhost"
	imagesHost = "private-user-images.githubusercontent.com"
	// signature is what makes a signed link good, as GitHub's JWT does.
	signature = "fixture"
)

// Fixture is the made-up server and GitHub, serving.
type Fixture struct {
	srv  *http.Server
	addr string

	mu     sync.Mutex
	data   *data
	live   map[chan line]bool
	timers []*time.Timer
}

// Start serves the fixtures until Stop, each start with the same made-up
// data again: what a reply or a new issue adds lasts until the app quits.
func Start() (*Fixture, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f := &Fixture{addr: l.Addr().String(), data: seed(time.Now()), live: map[chan line]bool{}}
	mux := http.NewServeMux()
	f.server(mux)
	f.github(mux)
	// An attachment, by its signed link, which needs no credentials.
	mux.HandleFunc("GET /images/{path...}", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("jwt") != signature || "https://github.com/user-attachments/assets/"+imageID(r.URL.Path) != Attachment {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(screenshot())
	})
	f.srv = &http.Server{Handler: mux}
	go f.srv.Serve(l)
	return f, nil
}

// Stop stops serving.
func (f *Fixture) Stop() {
	f.mu.Lock()
	for _, t := range f.timers {
		t.Stop()
	}
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f.srv.Shutdown(ctx)
}

// Transport sends requests for api.github.com, server.fixture.localhost, and
// GitHub's signed links to attachments to the fixtures, and every other
// request through base.
func (f *Fixture) Transport(base http.RoundTripper) http.RoundTripper {
	return roundTrip(func(r *http.Request) (*http.Response, error) {
		var prefix string
		switch r.URL.Hostname() {
		case githubHost:
			prefix = "/github"
		case serverHost:
			prefix = "/server"
		case imagesHost:
			prefix = "/images"
		default:
			return base.RoundTrip(r)
		}
		r = r.Clone(r.Context())
		r.URL = &url.URL{Scheme: "http", Host: f.addr, Path: prefix + r.URL.Path, RawQuery: r.URL.RawQuery}
		r.Host = f.addr
		return base.RoundTrip(r)
	})
}

type roundTrip func(*http.Request) (*http.Response, error)

func (rt roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return rt(r) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// The server's API: harness serve's /api/v1.
func (f *Fixture) server(mux *http.ServeMux) {
	mux.HandleFunc("GET /server/api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"api": 1, "harness": "fixture"})
	})
	mux.HandleFunc("GET /server/api/v1/projects", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		running := 0
		for _, t := range f.data.threads {
			if t.State == "running" {
				running++
			}
		}
		writeJSON(w, 200, []map[string]any{{"repo": Repo, "base": "main", "runner": "opencode,claude-code,openai-codex",
			"projects": []string{"harbor", "slipway"}, "ready": true, "threads": len(f.data.threads), "running": running}})
	})
	mux.HandleFunc("GET /server/api/v1/threads", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := []thread{}
		if repo := r.URL.Query().Get("repo"); repo == "" || repo == Repo {
			for _, t := range f.data.threads {
				out = append(out, *t)
			}
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /server/api/v1/threads/{owner}/{name}/{number}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		t := f.thread(w, r)
		if t == nil {
			return
		}
		writeJSON(w, 200, map[string]any{"thread": t, "lines": f.data.lines[t.Number]})
	})
	mux.HandleFunc("GET /server/api/v1/threads/{owner}/{name}/{number}/log", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		t := f.thread(w, r)
		if t == nil {
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		for _, l := range f.data.lines[t.Number] {
			fmt.Fprintf(w, "[%s] %s\n\n", l.Kind, l.Text)
		}
	})
	mux.HandleFunc("POST /server/api/v1/threads/{owner}/{name}/{number}/stop", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		t := f.thread(w, r)
		if t == nil {
			return
		}
		if t.State != "running" {
			http.Error(w, "the thread has no running turn", http.StatusConflict)
			return
		}
		t.State = "idle"
		f.add(t, "error", "The turn was stopped.")
		writeJSON(w, 200, map[string]any{})
	})
	mux.HandleFunc("POST /server/api/v1/threads/{owner}/{name}/{number}/retry", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		t := f.thread(w, r)
		if t == nil {
			return
		}
		if t.State == "running" {
			http.Error(w, "the thread's turn is running", http.StatusConflict)
			return
		}
		f.turn(t, "You asked for another turn.")
		writeJSON(w, 200, map[string]any{})
	})
	// The live stream: every line as the bots write it, as server-sent events.
	mux.HandleFunc("GET /server/api/v1/live", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no streaming", 500)
			return
		}
		ch := make(chan line, 64)
		f.mu.Lock()
		f.live[ch] = true
		f.mu.Unlock()
		defer func() {
			f.mu.Lock()
			delete(f.live, ch)
			f.mu.Unlock()
		}()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case l := <-ch:
				b, _ := json.Marshal(l)
				fmt.Fprintf(w, "data: %s\n\n", b)
				fl.Flush()
			}
		}
	})
}

// thread is the request's thread, or nil after answering 404. f.mu is held.
func (f *Fixture) thread(w http.ResponseWriter, r *http.Request) *thread {
	n, _ := strconv.Atoi(r.PathValue("number"))
	if r.PathValue("owner")+"/"+r.PathValue("name") == Repo {
		for _, t := range f.data.threads {
			if t.Number == n {
				return t
			}
		}
	}
	http.Error(w, "no such thread", http.StatusNotFound)
	return nil
}

// add writes a line to a thread, and to the live stream. f.mu is held.
func (f *Fixture) add(t *thread, kind, text string) {
	l := line{Repo: t.Repo, Number: t.Number, Time: time.Now(), Kind: kind, Text: text}
	f.data.lines[t.Number] = append(f.data.lines[t.Number], l)
	for ch := range f.live {
		select {
		case ch <- l:
		default:
		}
	}
}

// turn gives a thread a short turn: it runs for a few seconds, as a bot
// would, then the thread is idle. f.mu is held.
func (f *Fixture) turn(t *thread, why string) {
	// started is the latest turn's start, as the server has it.
	t.State, t.Waiting, t.Started = "running", 0, time.Now()
	f.add(t, "prompt", fmt.Sprintf("=== %s %s session fixture\nNew activity on %s issue #%d: %s\n\nStart with `harness status`.",
		time.Now().Format(time.RFC3339), t.Agent, t.Repo, t.Number, why))
	f.add(t, "tool", `bash: {"command":"harness status"}`)
	f.timers = append(f.timers, time.AfterFunc(3*time.Second, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if t.State != "running" {
			return
		}
		f.add(t, "result", fmt.Sprintf("bash: harness status\nIssue #%d\nNothing new since your last turn.", t.Number))
		f.add(t, "text", "Nothing has changed since my last turn, so there is nothing to do.")
		t.State = "idle"
	}))
}

// GitHub's API: the GraphQL queries and REST calls the app makes.
func (f *Fixture) github(mux *http.ServeMux) {
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+Token {
				writeJSON(w, 401, map[string]any{"message": "Bad credentials"})
				return
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			h(w, r)
		}
	}
	mux.HandleFunc("GET /github/user", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"login": Login})
	}))
	mux.HandleFunc("POST /github/graphql", auth(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			writeJSON(w, 400, map[string]any{"message": "Problems parsing JSON"})
			return
		}
		data, err := f.data.graphql(q.Query, q.Variables)
		if err != nil {
			writeJSON(w, 200, map[string]any{"errors": []map[string]any{{"message": err.Error()}}})
			return
		}
		writeJSON(w, 200, map[string]any{"data": data})
	}))
	mux.HandleFunc("POST /github/repos/{owner}/{name}/issues/{number}/comments", auth(func(w http.ResponseWriter, r *http.Request) {
		i := f.issue(w, r)
		if i == nil {
			return
		}
		var v struct {
			Body string `json:"body"`
		}
		json.NewDecoder(r.Body).Decode(&v)
		c := comment{Author: user(Login), Body: v.Body, Created: time.Now()}
		c.URL = fmt.Sprintf("%s#issuecomment-%d", i.url(), len(i.Comments)+1)
		i.Comments = append(i.Comments, c)
		i.Updated = c.Created
		writeJSON(w, 201, map[string]any{"html_url": c.URL, "body": c.Body})
		// A comment on a thread's issue is what starts a bot's turn.
		for _, t := range f.data.threads {
			if t.Number == i.Number || (i.PR != nil && t.PR == i.Number) {
				if t.State != "running" {
					f.turn(t, "@"+Login+" commented.")
				}
			}
		}
	}))
	mux.HandleFunc("POST /github/repos/{owner}/{name}/labels", auth(func(w http.ResponseWriter, r *http.Request) {
		if !f.repo(w, r) {
			return
		}
		var v struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&v)
		if f.data.labels[v.Name] {
			writeJSON(w, 422, map[string]any{"message": "Validation Failed",
				"errors": []map[string]any{{"resource": "Label", "code": "already_exists", "field": "name"}}})
			return
		}
		f.data.labels[v.Name] = true
		writeJSON(w, 201, map[string]any{"name": v.Name})
	}))
	mux.HandleFunc("POST /github/repos/{owner}/{name}/issues", auth(func(w http.ResponseWriter, r *http.Request) {
		if !f.repo(w, r) {
			return
		}
		var v struct {
			Title  string   `json:"title"`
			Body   string   `json:"body"`
			Labels []string `json:"labels"`
		}
		json.NewDecoder(r.Body).Decode(&v)
		for _, l := range v.Labels {
			if !f.data.labels[l] {
				writeJSON(w, 422, map[string]any{"message": "Validation Failed",
					"errors": []map[string]any{{"resource": "Label", "code": "invalid", "field": "labels"}}})
				return
			}
		}
		now := time.Now()
		i := &issue{Number: f.data.next(), Title: v.Title, Body: v.Body, Author: user(Login), Labels: append([]string{}, v.Labels...),
			Created: now, Updated: now}
		f.data.issues = append(f.data.issues, i)
		writeJSON(w, 201, map[string]any{"number": i.Number, "html_url": i.url()})
	}))
	// The app's releases: none, so a dev build has nothing to update to.
	mux.HandleFunc("GET /github/repos/{owner}/{name}/releases", auth(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, []any{})
	}))
}

// repo is whether the request is for the made-up monorepo, after answering
// 404 if not, as GitHub does for a repository a token cannot see.
func (f *Fixture) repo(w http.ResponseWriter, r *http.Request) bool {
	if r.PathValue("owner")+"/"+r.PathValue("name") != Repo {
		writeJSON(w, 404, map[string]any{"message": "Not Found"})
		return false
	}
	return true
}

func (f *Fixture) issue(w http.ResponseWriter, r *http.Request) *issue {
	if !f.repo(w, r) {
		return nil
	}
	n, _ := strconv.Atoi(r.PathValue("number"))
	if i := f.data.find(n); i != nil {
		return i
	}
	writeJSON(w, 404, map[string]any{"message": "Not Found"})
	return nil
}

var errQuery = errors.New("the fixtures do not know this query")

// graphql answers the app's three queries, by their names.
func (d *data) graphql(query string, vars map[string]any) (map[string]any, error) {
	name := strings.Fields(strings.TrimPrefix(strings.TrimSpace(query), "query"))
	if len(name) == 0 {
		return nil, errQuery
	}
	op, _, _ := strings.Cut(name[0], "(")
	if vars["owner"] != "example" || vars["name"] != "monorepo" {
		return map[string]any{"repository": nil}, nil
	}
	switch op {
	case "Issues", "PullRequests":
		nodes := []any{}
		for _, i := range d.sorted() {
			if (i.PR != nil) == (op == "PullRequests") {
				nodes = append(nodes, i.node(false))
			}
		}
		return map[string]any{"repository": map[string]any{"list": map[string]any{"nodes": nodes}}}, nil
	case "Conversation", "Rendered":
		n, _ := vars["number"].(float64)
		i := d.find(int(n))
		if i == nil {
			return map[string]any{"repository": map[string]any{"item": nil}}, nil
		}
		return map[string]any{"repository": map[string]any{"item": i.node(true)}}, nil
	}
	return nil, errQuery
}
