package school

import (
	"net/http"
	"slices"
	"time"

	"github.com/crewjam/saml/samlsp"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

type portal struct {
	pages *site.Pages
	sp    *samlsp.Middleware
	data  data
	key   []byte
}

func newPortal(d data, sp *samlsp.Middleware, key []byte) http.Handler {
	p := &portal{
		pages: site.NewPages(site.Brand{Name: "Maple Grove Schools", Color: "#15803d", Footer: "Maple Grove School District 41 · Parent portal.",
			// As an old district site has it: "shortcut icon", a GIF, and a relative path.
			Icons: `<link rel="shortcut icon" href="images/favicon.gif" type="image/gif">`}, sub("portal"), nil),
		sp: sp, data: d, key: key,
	}
	mux := http.NewServeMux()
	site.Files(mux, files, "portal/icons", "/images/")
	mux.Handle("/saml/", sp)
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := sp.Session.GetSession(r); err == nil {
			http.Redirect(w, r, "/students", http.StatusSeeOther)
			return
		}
		p.pages.Render(w, 200, "home", "", nil)
	})
	mux.Handle("GET /students", sp.RequireAccount(http.HandlerFunc(p.students)))
	mux.Handle("GET /students/{id}", sp.RequireAccount(http.HandlerFunc(p.student)))
	mux.Handle("GET /events", sp.RequireAccount(http.HandlerFunc(p.events)))
	mux.HandleFunc("POST /logout", func(w http.ResponseWriter, r *http.Request) {
		sp.Session.DeleteSession(w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	return mux
}

func (p *portal) students(w http.ResponseWriter, r *http.Request) {
	p.pages.Render(w, 200, "students", "Your students", map[string]any{"Name": p.data.Parent.Name, "Students": p.data.Students})
}

func (p *portal) student(w http.ResponseWriter, r *http.Request) {
	var s *Student
	for i := range p.data.Students {
		if p.data.Students[i].ID == r.PathValue("id") {
			s = &p.data.Students[i]
		}
	}
	if s == nil {
		http.NotFound(w, r)
		return
	}
	var work []Assignment
	for _, a := range p.data.Assignments {
		if a.Student == s.ID {
			work = append(work, a)
		}
	}
	slices.SortFunc(work, func(a, b Assignment) int { return compare(a.Due, b.Due) })
	// The launch link signs the parent in to the gradebook, in the iframe.
	launch := hosts.URL(hosts.Gradebook) + "/launch?token=" + site.SignJWT(p.key, launchClaims{
		Claims:  site.Claims{Subject: p.data.Parent.Username, ID: site.Token(), Issued: time.Now().Unix(), Expires: time.Now().Add(time.Minute).Unix()},
		Student: s.ID,
	})
	p.pages.Render(w, 200, "student", s.Name, map[string]any{
		"Name": p.data.Parent.Name, "Student": s, "Students": p.data.Students, "Assignments": work, "Gradebook": launch,
	})
}

func (p *portal) events(w http.ResponseWriter, r *http.Request) {
	p.pages.Render(w, 200, "events", "Events", map[string]any{"Name": p.data.Parent.Name, "Events": p.data.Events})
}

func compare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

type launchClaims struct {
	site.Claims
	Student string `json:"student"`
}
