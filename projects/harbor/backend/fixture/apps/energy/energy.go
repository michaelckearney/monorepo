// Package energy is Brightline Energy: a utility's account site. The pages
// are on brightline.localhost and the data comes from api.brightline.localhost.
//
// Scenarios: a JSON API on another subdomain, with CORS and credentials
// (D3), an export job to start, poll, and download (D15), usage drawn only
// in a canvas (D21); a session cookie on the parent domain, sent to both
// hosts (S8); sign-in by an emailed link (A7).
package energy

import (
	"embed"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/mail"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data templates icons
var files embed.FS

type Bill struct {
	ID     string `json:"id"`
	Period string `json:"period"`
	KWh    int    `json:"kwh"`
	Amount int64  `json:"amountCents"`
	Issued string `json:"issued"`
	Due    string `json:"due"`
	Status string `json:"status"`
}

type Day struct {
	Date string  `json:"date"`
	KWh  float64 `json:"kwh"`
}

type data struct {
	Email          string `json:"email"`
	AccountNumber  string `json:"accountNumber"`
	Name           string `json:"name"`
	ServiceAddress string `json:"serviceAddress"`
	Plan           string `json:"plan"`
	Autopay        bool   `json:"autopay"`
	Bills          []Bill `json:"bills"`
	Daily          []Day  `json:"daily"`
}

type export struct {
	ID      string
	From    string
	To      string
	Started time.Time
}

// exportTakes is how long an export job runs before its file is ready. A
// variable so the test waits a moment, not three seconds.
var exportTakes = 3 * time.Second

type energy struct {
	pages    *site.Pages
	mail     *mail.Box
	sessions *site.Sessions[string] // the email signed in

	mu      sync.Mutex
	data    data
	links   map[string]time.Time // sign-in link tokens, and when they expire
	exports map[string]*export
}

// New returns the site's pages and its API, for the two hosts.
func New(store *site.Store, box *mail.Box) (pages, api http.Handler) {
	e := &energy{
		pages: site.NewPages(site.Brand{Name: "Brightline Energy", Color: "#d97706", Footer: "Brightline Energy Co-op.",
			// A favicon generator's set: one PNG for each size, and one for iOS.
			Icons: `<link rel="apple-touch-icon" sizes="180x180" href="/apple-touch-icon.png">
<link rel="icon" type="image/png" sizes="32x32" href="/favicon-32x32.png">
<link rel="icon" type="image/png" sizes="16x16" href="/favicon-16x16.png">
<link rel="icon" type="image/png" sizes="192x192" href="/android-chrome-192x192.png">`}, files, nil),
		mail: box,
		sessions: site.NewSessions[string](store, site.SessionOptions{
			// On the parent domain, so the API on api.brightline.localhost gets it.
			Cookie: "bl_session", Domain: hosts.Energy, Idle: 30 * time.Minute,
		}),
		links:   map[string]time.Time{},
		exports: map[string]*export{},
	}
	site.Load(files, "data/account.json", &e.data)

	p := http.NewServeMux()
	site.Files(p, files, "icons", "/")
	p.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if s, _ := e.sessions.Get(r); s != nil {
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		e.pages.Render(w, 200, "home", "", nil)
	})
	p.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) { e.pages.Render(w, 200, "login", "Sign in", nil) })
	p.HandleFunc("POST /login", e.sendLink)
	p.HandleFunc("GET /login/verify", e.verify)
	p.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		e.sessions.End(w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	p.HandleFunc("GET /account", func(w http.ResponseWriter, r *http.Request) {
		if s, _ := e.sessions.Get(r); s == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		e.pages.Render(w, 200, "account", "Your account", map[string]any{"API": hosts.URL(hosts.EnergyAPI)})
	})

	a := http.NewServeMux()
	a.HandleFunc("GET /v1/account", e.auth(e.account))
	a.HandleFunc("GET /v1/bills", e.auth(e.bills))
	a.HandleFunc("GET /v1/bills/{id}", e.auth(e.bill))
	a.HandleFunc("GET /v1/usage", e.auth(e.usage))
	a.HandleFunc("PUT /v1/autopay", e.auth(e.autopay))
	a.HandleFunc("POST /v1/exports", e.auth(e.startExport))
	a.HandleFunc("GET /v1/exports/{id}", e.auth(e.exportStatus))
	a.HandleFunc("GET /v1/exports/{id}/file", e.auth(e.exportFile))
	return p, cors(a)
}

// cors lets brightline.localhost's pages call the API with their cookie.
func cors(next http.Handler) http.Handler {
	origin := hosts.URL(hosts.Energy)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == origin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (e *energy) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s, _ := e.sessions.Get(r); s == nil {
			site.Fail(w, http.StatusUnauthorized, "not_signed_in", "Sign in at "+hosts.URL(hosts.Energy)+"/login.")
			return
		}
		next(w, r)
	}
}

func (e *energy) sendLink(w http.ResponseWriter, r *http.Request) {
	if !site.SameOrigin(r, hosts.Energy) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	// The page is the same whether or not there is an account, so it does
	// not say which emails have one.
	if email == e.data.Email {
		token := site.Token()
		e.mu.Lock()
		e.links[token] = time.Now().Add(15 * time.Minute)
		e.mu.Unlock()
		link := hosts.URL(hosts.Energy) + "/login/verify?token=" + token
		e.mail.Send("no-reply@brightline.localhost", email, "Your Brightline sign-in link",
			"Hi "+e.data.Name+",\n\nClick this link to sign in to Brightline Energy. It works once, for 15 minutes.\n\n"+link+
				"\n\nIf you didn't ask for it, you can ignore this email.")
	}
	e.pages.Render(w, 200, "sent", "Check your email", map[string]any{"Email": email})
}

func (e *energy) verify(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	e.mu.Lock()
	expires, ok := e.links[token]
	delete(e.links, token)
	e.mu.Unlock()
	if !ok || time.Now().After(expires) {
		e.pages.Render(w, 410, "expired", "Link expired", nil)
		return
	}
	e.sessions.Start(w, e.data.Email)
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (e *energy) account(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	site.JSON(w, 200, map[string]any{
		"accountNumber": e.data.AccountNumber, "name": e.data.Name, "email": e.data.Email,
		"serviceAddress": e.data.ServiceAddress, "plan": e.data.Plan, "autopay": e.data.Autopay,
	})
}

func (e *energy) bills(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Bill, 0, len(e.data.Bills))
	for i := len(e.data.Bills) - 1; i >= 0; i-- {
		out = append(out, e.data.Bills[i])
	}
	site.JSON(w, 200, map[string]any{"bills": out})
}

func (e *energy) bill(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, b := range e.data.Bills {
		if b.ID == r.PathValue("id") {
			site.JSON(w, 200, b)
			return
		}
	}
	site.Fail(w, 404, "not_found", "There is no bill "+r.PathValue("id")+".")
}

// usage returns kWh by month, or by day with ?by=day, between from and to
// (YYYY-MM or YYYY-MM-DD, inclusive).
func (e *energy) usage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to, by := q.Get("from"), q.Get("to"), q.Get("by")
	if by == "" {
		by = "month"
	}
	if by != "month" && by != "day" {
		site.Fail(w, 400, "bad_request", "by is month or day.")
		return
	}
	type point struct {
		Period string  `json:"period"`
		KWh    float64 `json:"kwh"`
	}
	var points []point
	e.mu.Lock()
	for _, d := range e.data.Daily {
		if (from != "" && d.Date[:len(from)] < from) || (to != "" && d.Date[:min(len(to), 10)] > to) {
			continue
		}
		key := d.Date
		if by == "month" {
			key = d.Date[:7]
		}
		if n := len(points); n > 0 && points[n-1].Period == key {
			points[n-1].KWh += d.KWh
		} else {
			points = append(points, point{key, d.KWh})
		}
	}
	e.mu.Unlock()
	for i := range points {
		points[i].KWh = float64(int(points[i].KWh*10+0.5)) / 10
	}
	if points == nil {
		points = []point{}
	}
	site.JSON(w, 200, map[string]any{"unit": "kWh", "by": by, "points": points})
}

func (e *energy) autopay(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled *bool `json:"enabled"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	if in.Enabled == nil {
		site.Fail(w, 400, "bad_request", `Send {"enabled": true} or {"enabled": false}.`)
		return
	}
	e.mu.Lock()
	e.data.Autopay = *in.Enabled
	e.mu.Unlock()
	site.JSON(w, 200, map[string]bool{"autopay": *in.Enabled})
}

func (e *energy) startExport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	if _, err := time.Parse("2006-01-02", in.From); err != nil {
		site.Fail(w, 400, "bad_request", "from is a date: YYYY-MM-DD.")
		return
	}
	if _, err := time.Parse("2006-01-02", in.To); err != nil {
		site.Fail(w, 400, "bad_request", "to is a date: YYYY-MM-DD.")
		return
	}
	x := &export{ID: "exp_" + site.Token()[:12], From: in.From, To: in.To, Started: time.Now()}
	e.mu.Lock()
	e.exports[x.ID] = x
	e.mu.Unlock()
	site.JSON(w, http.StatusAccepted, e.status(x))
}

func (e *energy) status(x *export) map[string]any {
	s := map[string]any{"id": x.ID, "from": x.From, "to": x.To, "status": "pending"}
	if time.Since(x.Started) >= exportTakes {
		s["status"] = "ready"
		s["fileUrl"] = hosts.URL(hosts.EnergyAPI) + "/v1/exports/" + x.ID + "/file"
	}
	return s
}

func (e *energy) find(w http.ResponseWriter, r *http.Request) *export {
	e.mu.Lock()
	x := e.exports[r.PathValue("id")]
	e.mu.Unlock()
	if x == nil {
		site.Fail(w, 404, "not_found", "There is no export "+r.PathValue("id")+".")
	}
	return x
}

func (e *energy) exportStatus(w http.ResponseWriter, r *http.Request) {
	if x := e.find(w, r); x != nil {
		site.JSON(w, 200, e.status(x))
	}
}

func (e *energy) exportFile(w http.ResponseWriter, r *http.Request) {
	x := e.find(w, r)
	if x == nil {
		return
	}
	if time.Since(x.Started) < exportTakes {
		site.Fail(w, http.StatusConflict, "not_ready", "The export is still running. Check its status first.")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="brightline-usage-%s-to-%s.csv"`, x.From, x.To))
	fmt.Fprintln(w, "date,kwh")
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, d := range e.data.Daily {
		if d.Date >= x.From && d.Date <= x.To {
			fmt.Fprintf(w, "%s,%.1f\n", d.Date, d.KWh)
		}
	}
}
