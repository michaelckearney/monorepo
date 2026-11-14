package school

import (
	"net/http"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// gradebook is GradeBook Pro, another company, shown in the portal's
// iframe. Its cookie is partitioned: inside the portal it is one cookie, and
// gradebook.localhost on its own never sees it.
type gradebook struct {
	pages    *site.Pages
	sessions *site.Sessions[string] // the student
	data     data
	key      []byte

	mu   sync.Mutex
	used map[string]bool // launch tokens work once
}

func newGradebook(store *site.Store, d data, key []byte) http.Handler {
	g := &gradebook{
		pages: site.NewPages(site.Brand{Name: "GradeBook Pro", Color: "#9333ea", Footer: "GradeBook Pro, for schools."}, sub("gradebook"), nil),
		sessions: site.NewSessions[string](store, site.SessionOptions{
			Cookie: "gbp_sid", SameSite: http.SameSiteNoneMode, Partitioned: true, Absolute: time.Hour,
		}),
		data: d, key: key, used: map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /launch", g.launch)
	mux.HandleFunc("GET /embed/grades", g.grades)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { g.pages.Render(w, 200, "home", "", nil) })
	return frame(mux)
}

// frame lets only the portal show the gradebook in a frame.
func frame(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "frame-ancestors "+hosts.URL(hosts.School))
		next.ServeHTTP(w, r)
	})
}

func (g *gradebook) launch(w http.ResponseWriter, r *http.Request) {
	var c launchClaims
	if err := site.ParseJWT(g.key, r.URL.Query().Get("token"), &c); err != nil {
		g.pages.Render(w, 401, "expired", "Launch failed", nil)
		return
	}
	g.mu.Lock()
	used := g.used[c.ID]
	g.used[c.ID] = true
	g.mu.Unlock()
	if used {
		g.pages.Render(w, 401, "expired", "Launch failed", nil)
		return
	}
	g.sessions.Start(w, c.Student)
	http.Redirect(w, r, "/embed/grades", http.StatusSeeOther)
}

func (g *gradebook) grades(w http.ResponseWriter, r *http.Request) {
	s, _ := g.sessions.Get(r)
	if s == nil {
		g.pages.Render(w, 401, "expired", "Not signed in", nil)
		return
	}
	var student Student
	for _, st := range g.data.Students {
		if st.ID == s.Data {
			student = st
		}
	}
	g.pages.Render(w, 200, "grades", student.Name, map[string]any{"Student": student, "Courses": g.data.Grades[student.ID]})
}
