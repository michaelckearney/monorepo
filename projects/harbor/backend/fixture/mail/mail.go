// Package mail is the fixtures' mail catcher: sites "send" mail to it, and
// it shows every message at mail.localhost, with no sign-in. Magic links and
// emailed codes are read there, by a person or by a test.
package mail

import (
	"embed"
	"html"
	"html/template"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

type Message struct {
	ID      int       `json:"id"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Subject string    `json:"subject"`
	Text    string    `json:"text"`
	Sent    time.Time `json:"sent"`
}

type Box struct {
	mu       sync.Mutex
	messages []Message
}

// Send delivers a message.
func (b *Box) Send(from, to, subject, text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.messages = append(b.messages, Message{
		ID: len(b.messages) + 1, From: from, To: to, Subject: subject, Text: text, Sent: time.Now(),
	})
}

// List returns the messages to an address ("" for all), newest first.
func (b *Box) List(to string) []Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []Message
	for _, m := range slices.Backward(b.messages) {
		if to == "" || strings.EqualFold(m.To, to) {
			out = append(out, m)
		}
	}
	return out
}

//go:embed templates
var files embed.FS

// Handler serves the mail catcher:
//
//	GET /                 every message, newest first (?to= filters)
//	GET /messages/{id}    one message
//	GET /api/messages     the same as JSON (?to= filters)
func (b *Box) Handler() http.Handler {
	pages := site.NewPages(site.Brand{Name: "Mail catcher", Color: "#4b5563"}, files, template.FuncMap{"linkify": linkify})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		pages.Render(w, 200, "list", "Mail", map[string]any{"Messages": b.List(r.URL.Query().Get("to")), "To": r.URL.Query().Get("to")})
	})
	mux.HandleFunc("GET /messages/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		for _, m := range b.List("") {
			if m.ID == id {
				pages.Render(w, 200, "message", m.Subject, m)
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("GET /api/messages", func(w http.ResponseWriter, r *http.Request) {
		msgs := b.List(r.URL.Query().Get("to"))
		if msgs == nil {
			msgs = []Message{}
		}
		site.JSON(w, 200, msgs)
	})
	return mux
}

var links = regexp.MustCompile(`https://[^\s<>"]+`)

// linkify escapes text, and makes its https links clickable.
func linkify(text string) template.HTML {
	escaped := html.EscapeString(text)
	return template.HTML(links.ReplaceAllStringFunc(escaped, func(u string) string {
		return `<a href="` + u + `">` + u + `</a>`
	}))
}
