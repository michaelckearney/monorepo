// Package chat is Huddle, a team chat.
//
// Scenarios: new messages over server-sent events (D17), and a search
// that is rate-limited (D22); a token kept in IndexedDB (S7); "sign out
// everywhere", which ends every session (V14); signing in in a popup (A17),
// by a JavaScript call that returns the token (A4).
package chat

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data static
var files embed.FS

const (
	searchLimit  = 5
	searchWindow = 10 * time.Second
)

type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

type Channel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Topic string `json:"topic"`
}

type Message struct {
	ID         string `json:"id"`
	Channel    string `json:"channel"`
	User       string `json:"user"`
	Text       string `json:"text"`
	Sent       string `json:"sent"`
	MentionsMe bool   `json:"mentionsMe"`
	Read       bool   `json:"read"`
}

type data struct {
	Me struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Name     string `json:"name"`
	} `json:"me"`
	Users    []User     `json:"users"`
	Channels []Channel  `json:"channels"`
	Messages []*Message `json:"messages"`
}

type chat struct {
	tokens *site.Table[bool] // token: revoked

	mu       sync.Mutex
	data     data
	searches map[string][]time.Time // by token
	watchers map[chan []byte]bool
}

func New(store *site.Store) http.Handler {
	c := &chat{tokens: site.NewTable[bool](store, "huddle tokens"), searches: map[string][]time.Time{}, watchers: map[chan []byte]bool{}}
	site.Load(files, "data/chat.json", &c.data)
	mux := http.NewServeMux()
	page := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			b, _ := files.ReadFile("static/" + name)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(b)
		}
	}
	mux.HandleFunc("GET /{$}", page("index.html"))
	mux.HandleFunc("GET /auth/popup", page("popup.html"))
	mux.Handle("GET /static/", http.FileServerFS(files))
	mux.HandleFunc("POST /api/auth/login", c.login)
	mux.HandleFunc("POST /api/auth/logout-everywhere", c.auth(c.logoutEverywhere))
	mux.HandleFunc("GET /api/me", c.auth(c.me))
	mux.HandleFunc("GET /api/channels", c.auth(c.channels))
	mux.HandleFunc("GET /api/channels/{id}/messages", c.auth(c.messages))
	mux.HandleFunc("POST /api/channels/{id}/messages", c.auth(c.post))
	mux.HandleFunc("GET /api/mentions", c.auth(c.mentions))
	mux.HandleFunc("POST /api/mentions/read", c.auth(c.markRead))
	mux.HandleFunc("GET /api/search", c.auth(c.search))
	mux.HandleFunc("GET /api/events", c.events)
	return mux
}

func (c *chat) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(in.Email), c.data.Me.Email) || in.Password != site.Password {
		site.Fail(w, http.StatusUnauthorized, "invalid_credentials", "That email and password don't match.")
		return
	}
	token := "hd_" + site.Token()
	c.tokens.Put(token, false)
	site.JSON(w, 200, map[string]any{"token": token, "user": c.user(c.data.Me.Username)})
}

// check returns "" for a live token, or why it is not.
func (c *chat) check(token string) string {
	revoked, ok := c.tokens.Get(token)
	switch {
	case !ok:
		return "invalid_token"
	case revoked:
		return "session_revoked"
	}
	return ""
}

func (c *chat) auth(next func(w http.ResponseWriter, r *http.Request, token string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		switch c.check(token) {
		case "invalid_token":
			site.Fail(w, http.StatusUnauthorized, "invalid_token", "Send Authorization: Bearer with a token from signing in.")
		case "session_revoked":
			site.Fail(w, http.StatusUnauthorized, "session_revoked", "You were signed out on all devices. Sign in again.")
		default:
			next(w, r, token)
		}
	}
}

func (c *chat) logoutEverywhere(w http.ResponseWriter, r *http.Request, _ string) {
	c.tokens.Each(func(t string, _ bool) { c.tokens.Put(t, true) })
	site.JSON(w, 200, map[string]bool{"ok": true})
}

func (c *chat) user(username string) User {
	for _, u := range c.data.Users {
		if u.Username == username {
			return u
		}
	}
	return User{Username: username, Name: username}
}

func (c *chat) me(w http.ResponseWriter, r *http.Request, _ string) {
	site.JSON(w, 200, c.user(c.data.Me.Username))
}

func (c *chat) channels(w http.ResponseWriter, r *http.Request, _ string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	type withCount struct {
		Channel
		Unread int `json:"unreadMentions"`
	}
	out := []withCount{}
	for _, ch := range c.data.Channels {
		n := 0
		for _, m := range c.data.Messages {
			if m.Channel == ch.ID && m.MentionsMe && !m.Read {
				n++
			}
		}
		out = append(out, withCount{ch, n})
	}
	site.JSON(w, 200, map[string]any{"channels": out})
}

// view is a message as the API returns it, with its author.
type view struct {
	Message
	Author User `json:"author"`
}

func (c *chat) view(m *Message) view { return view{*m, c.user(m.User)} }

// messages returns a channel's messages, oldest first: the newest 30, or
// the 30 before ?before=<id>.
func (c *chat) messages(w http.ResponseWriter, r *http.Request, _ string) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	before := r.URL.Query().Get("before")
	c.mu.Lock()
	defer c.mu.Unlock()
	var in []*Message
	for _, m := range c.data.Messages {
		if m.Channel == r.PathValue("id") {
			if before != "" && m.ID >= before {
				break
			}
			in = append(in, m)
		}
	}
	more := len(in) > limit
	in = in[max(0, len(in)-limit):]
	out := []view{}
	for _, m := range in {
		out = append(out, c.view(m))
	}
	site.JSON(w, 200, map[string]any{"messages": out, "hasMore": more})
}

func (c *chat) post(w http.ResponseWriter, r *http.Request, _ string) {
	var in struct {
		Text string `json:"text"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Text) == "" {
		site.Fail(w, 400, "empty", "A message needs text.")
		return
	}
	c.mu.Lock()
	known := slices.ContainsFunc(c.data.Channels, func(ch Channel) bool { return ch.ID == r.PathValue("id") })
	if !known {
		c.mu.Unlock()
		site.Fail(w, 404, "not_found", "There is no channel "+r.PathValue("id")+".")
		return
	}
	m := &Message{ID: fmt.Sprintf("m%04d", len(c.data.Messages)+1), Channel: r.PathValue("id"), User: c.data.Me.Username,
		Text: in.Text, Sent: time.Now().UTC().Format(time.RFC3339), Read: true}
	c.data.Messages = append(c.data.Messages, m)
	v := c.view(m)
	c.mu.Unlock()
	c.broadcast(v)
	site.JSON(w, 201, v)
}

func (c *chat) mentions(w http.ResponseWriter, r *http.Request, _ string) {
	unread := r.URL.Query().Get("unread") == "1"
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []view{}
	for _, m := range slices.Backward(c.data.Messages) {
		if m.MentionsMe && (!unread || !m.Read) {
			out = append(out, c.view(m))
		}
	}
	site.JSON(w, 200, map[string]any{"mentions": out})
}

func (c *chat) markRead(w http.ResponseWriter, r *http.Request, _ string) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	c.mu.Lock()
	n := 0
	for _, m := range c.data.Messages {
		if slices.Contains(in.IDs, m.ID) && !m.Read {
			m.Read = true
			n++
		}
	}
	c.mu.Unlock()
	site.JSON(w, 200, map[string]int{"marked": n})
}

// search allows 5 searches in 10 seconds per token, then answers 429 with
// Retry-After.
func (c *chat) search(w http.ResponseWriter, r *http.Request, token string) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	now := time.Now()
	c.mu.Lock()
	recent := slices.DeleteFunc(c.searches[token], func(t time.Time) bool { return now.Sub(t) > searchWindow })
	if len(recent) >= searchLimit {
		wait := searchWindow - now.Sub(recent[0])
		c.searches[token] = recent
		c.mu.Unlock()
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		site.Fail(w, http.StatusTooManyRequests, "rate_limited", fmt.Sprintf("Too many searches. Try again in %d seconds.", int(wait.Seconds())+1))
		return
	}
	c.searches[token] = append(recent, now)
	out := []view{}
	if q != "" {
		for _, m := range slices.Backward(c.data.Messages) {
			if strings.Contains(strings.ToLower(m.Text), q) {
				out = append(out, c.view(m))
			}
		}
	}
	c.mu.Unlock()
	site.JSON(w, 200, map[string]any{"query": q, "results": out})
}

// events streams new messages as server-sent events. EventSource cannot
// set headers, so the token is in the URL.
func (c *chat) events(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("access_token")
	if why := c.check(token); why != "" {
		site.Fail(w, http.StatusUnauthorized, why, "Sign in again.")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flush := http.NewResponseController(w).Flush
	ch := make(chan []byte, 16)
	c.mu.Lock()
	c.watchers[ch] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.watchers, ch)
		c.mu.Unlock()
	}()
	fmt.Fprint(w, "retry: 3000\n\n")
	flush()
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case msg := <-ch:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
		case <-tick.C:
			if c.check(token) != "" {
				fmt.Fprint(w, "event: signed-out\ndata: {}\n\n")
				flush()
				return
			}
			fmt.Fprint(w, ": ping\n\n")
		case <-r.Context().Done():
			return
		}
		if flush() != nil {
			return
		}
	}
}

func (c *chat) broadcast(v view) {
	msg, _ := json.Marshal(v)
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.watchers {
		select {
		case ch <- msg:
		default:
		}
	}
}
