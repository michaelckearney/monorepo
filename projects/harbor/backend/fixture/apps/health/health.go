// Package health is Clearwater Health: a patient portal for a family.
//
// Scenarios: every JSON reply starts with an anti-hijacking prefix (D9),
// and the API answers only requests with its client header (D10); sessions
// last a fixed 30 minutes, used or not (V3); after the password, you choose
// whose record to see (A19) and accept updated terms (A20), and until then
// you are not fully signed in.
package health

import (
	"embed"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data templates icons
var files embed.FS

// Prefix starts every JSON reply, so a <script> tag on another site cannot
// run it. The portal's own code strips it before parsing.
const Prefix = ")]}'\n"

// Client is the header the API requires, and its value.
const (
	ClientHeader = "X-Clearwater-Client"
	ClientValue  = "portal-web/3.2"
)

type Patient struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Relationship string `json:"relationship"`
	Born         string `json:"born"`
	MRN          string `json:"mrn"`
}

type Message struct {
	ID      string `json:"id"`
	Patient string `json:"patient"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Sent    string `json:"sent"`
	Read    bool   `json:"read"`
	Body    string `json:"body"`
}

type data struct {
	User struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"user"`
	Patients     []Patient         `json:"patients"`
	Appointments []json.RawMessage `json:"appointments"`
	Results      []json.RawMessage `json:"results"`
	Messages     []*Message        `json:"messages"`
}

type session struct {
	Patient string // whose record; "" until chosen
}

type health struct {
	pages    *site.Pages
	sessions *site.Sessions[*session]

	mu       sync.Mutex
	data     data
	accepted bool // the terms, once per run
}

func New(store *site.Store) http.Handler {
	h := &health{
		pages: site.NewPages(site.Brand{Name: "Clearwater Health", Color: "#0f766e", Footer: "Clearwater Health System.",
			// One icon for a light browser, and one for a dark one.
			Icons: `<link rel="icon" href="/assets/icon-light.svg" media="(prefers-color-scheme: light)">
<link rel="icon" href="/assets/icon-dark.svg" media="(prefers-color-scheme: dark)">`}, files, nil),
		sessions: site.NewSessions[*session](store, site.SessionOptions{Cookie: "CWSESSIONID", Absolute: 30 * time.Minute}),
	}
	site.Load(files, "data/health.json", &h.data)

	mux := http.NewServeMux()
	site.Files(mux, files, "icons", "/assets/")
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if s, _ := h.sessions.Get(r); s != nil {
			http.Redirect(w, r, h.next(s), http.StatusSeeOther)
			return
		}
		h.pages.Render(w, 200, "login", "Sign in", map[string]any{"Ended": r.URL.Query().Get("ended") != ""})
	})
	mux.HandleFunc("POST /login", h.login)
	mux.HandleFunc("GET /choose", h.page(h.choosePage, false))
	mux.HandleFunc("POST /choose", h.page(h.choose, false))
	mux.HandleFunc("GET /terms", h.page(h.termsPage, false))
	mux.HandleFunc("POST /terms", h.page(h.accept, false))
	mux.HandleFunc("GET /app/", h.page(func(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
		h.pages.Render(w, 200, "app", "", nil)
	}, true))
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		h.sessions.End(w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("GET /api/v2/me", h.api(h.me))
	mux.HandleFunc("POST /api/v2/patient", h.api(h.switchPatient))
	mux.HandleFunc("GET /api/v2/appointments", h.api(h.list("appointments")))
	mux.HandleFunc("GET /api/v2/results", h.api(h.list("results")))
	mux.HandleFunc("GET /api/v2/results/{id}", h.api(h.result))
	mux.HandleFunc("GET /api/v2/messages", h.api(h.messages))
	mux.HandleFunc("POST /api/v2/messages/{id}/read", h.api(h.markRead))
	return mux
}

// next is where a session goes: choose a person, accept the terms, or the app.
func (h *health) next(s *site.Session[*session]) string {
	chosen := s.Data.Patient != ""
	h.mu.Lock()
	accepted := h.accepted
	h.mu.Unlock()
	switch {
	case !chosen:
		return "/choose"
	case !accepted:
		return "/terms"
	}
	return "/app/"
}

type handler func(w http.ResponseWriter, r *http.Request, s *site.Session[*session])

// page needs a session. When ready, it also needs the person chosen and the
// terms accepted.
func (h *health) page(next handler, ready bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, why := h.sessions.Get(r)
		if s == nil {
			to := "/"
			if why != "" {
				to = "/?ended=1"
			}
			http.Redirect(w, r, to, http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost && !site.SameOrigin(r, hosts.Health) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if to := h.next(s); ready && to != "/app/" {
			http.Redirect(w, r, to, http.StatusSeeOther)
			return
		}
		next(w, r, s)
	}
}

func (h *health) login(w http.ResponseWriter, r *http.Request) {
	if !site.SameOrigin(r, hosts.Health) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.PostFormValue("username") != h.data.User.Username || r.PostFormValue("password") != site.Password {
		h.pages.Render(w, 401, "login", "Sign in", map[string]any{"Error": "The username or password you entered is incorrect."})
		return
	}
	s := h.sessions.Start(w, &session{})
	http.Redirect(w, r, h.next(s), http.StatusSeeOther)
}

func (h *health) choosePage(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	h.pages.Render(w, 200, "choose", "Choose a record", map[string]any{"Patients": h.data.Patients})
}

func (h *health) patient(id string) *Patient {
	for i := range h.data.Patients {
		if h.data.Patients[i].ID == id {
			return &h.data.Patients[i]
		}
	}
	return nil
}

func (h *health) choose(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	if h.patient(r.PostFormValue("patient")) == nil {
		http.Redirect(w, r, "/choose", http.StatusSeeOther)
		return
	}
	s.Data.Patient = r.PostFormValue("patient")
	h.sessions.Save(s)
	http.Redirect(w, r, h.next(s), http.StatusSeeOther)
}

func (h *health) termsPage(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	h.pages.Render(w, 200, "terms", "Updated terms", nil)
}

func (h *health) accept(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	if r.PostFormValue("agree") != "yes" {
		h.pages.Render(w, 422, "terms", "Updated terms", map[string]any{"Error": "Check the box to agree before you continue."})
		return
	}
	h.mu.Lock()
	h.accepted = true
	h.mu.Unlock()
	http.Redirect(w, r, h.next(s), http.StatusSeeOther)
}

// ---- The API

// reply writes v as JSON after the prefix.
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(Prefix))
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	reply(w, status, map[string]string{"error": code, "message": message})
}

type apiHandler func(w http.ResponseWriter, r *http.Request, s *site.Session[*session])

// patientOf returns whose record a session shows.
func patientOf(s *site.Session[*session]) string {
	return s.Data.Patient
}

func (h *health) api(next apiHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ClientHeader) != ClientValue {
			fail(w, http.StatusBadRequest, "unsupported_client", "This API answers Clearwater's own apps only.")
			return
		}
		s, _ := h.sessions.Get(r)
		if s == nil {
			fail(w, http.StatusUnauthorized, "session_expired", "Your session has ended. Sign in again.")
			return
		}
		if to := h.next(s); to != "/app/" {
			fail(w, http.StatusForbidden, "setup_incomplete", "Finish signing in at "+hosts.URL(hosts.Health)+to+".")
			return
		}
		next(w, r, s)
	}
}

func (h *health) me(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	reply(w, 200, map[string]any{"user": h.data.User.Name, "patient": h.patient(patientOf(s)), "patients": h.data.Patients})
}

// switchPatient changes whose record the session shows.
func (h *health) switchPatient(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	var in struct {
		Patient string `json:"patient"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || h.patient(in.Patient) == nil {
		fail(w, http.StatusBadRequest, "bad_request", `Send {"patient": id}, with an id from /api/v2/me.`)
		return
	}
	s.Data.Patient = in.Patient
	h.sessions.Save(s)
	reply(w, 200, map[string]any{"patient": h.patient(in.Patient)})
}

// list returns a kind of record for the session's patient.
func (h *health) list(kind string) apiHandler {
	return func(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
		patient := patientOf(s)
		all := h.data.Appointments
		if kind == "results" {
			all = h.data.Results
		}
		out := []json.RawMessage{}
		for _, raw := range all {
			var who struct{ Patient string }
			json.Unmarshal(raw, &who)
			if who.Patient == patient {
				out = append(out, raw)
			}
		}
		reply(w, 200, map[string]any{kind: out})
	}
}

func (h *health) result(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	patient := patientOf(s)
	for _, raw := range h.data.Results {
		var res struct{ ID, Patient string }
		json.Unmarshal(raw, &res)
		if res.ID == r.PathValue("id") && res.Patient == patient {
			reply(w, 200, raw)
			return
		}
	}
	fail(w, 404, "not_found", "No result "+r.PathValue("id")+" for this patient.")
}

func (h *health) messages(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	patient := patientOf(s)
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Message{}
	for _, m := range h.data.Messages {
		if m.Patient == patient {
			out = append(out, *m)
		}
	}
	slices.SortFunc(out, func(a, b Message) int { return strings.Compare(b.Sent, a.Sent) })
	reply(w, 200, map[string]any{"messages": out})
}

func (h *health) markRead(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	patient := patientOf(s)
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, m := range h.data.Messages {
		if m.ID == r.PathValue("id") && m.Patient == patient {
			m.Read = true
			reply(w, 200, m)
			return
		}
	}
	fail(w, 404, "not_found", "No message "+r.PathValue("id")+" for this patient.")
}
