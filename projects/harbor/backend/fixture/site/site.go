package site

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
)

// Password is every account's password, on every site.
const Password = "password123"

// Token returns a random token, safe in URLs and cookies.
func Token() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// JSON writes v as JSON.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Fail writes a JSON error: {"error": code, "message": message}.
func Fail(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, map[string]string{"error": code, "message": message})
}

// Read decodes a JSON request body into v, and writes a 400 if it cannot.
func Read(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		Fail(w, http.StatusBadRequest, "bad_request", "The body is not the JSON this endpoint takes: "+err.Error())
		return false
	}
	return true
}

// Load reads a JSON data file into v. Data files are the fixtures' seed
// data; a broken one is a bug in this repository.
func Load(fsys fs.FS, name string, v any) {
	b, err := fs.ReadFile(fsys, name)
	if err == nil {
		err = json.Unmarshal(b, v)
	}
	if err != nil {
		panic(fmt.Sprintf("data file %s: %v", name, err))
	}
}

// Next returns a "next" parameter if it is a path on this site, or def.
// Anything else would let a link send people to another site after sign-in.
func Next(next, def string) string {
	if strings.HasPrefix(next, "/") && !strings.HasPrefix(next, "//") && !strings.HasPrefix(next, "/\\") {
		return next
	}
	return def
}

// LoginURL is a path to sign in at, that comes back to r's page.
func LoginURL(login string, r *http.Request) string {
	return login + "?next=" + url.QueryEscape(r.URL.RequestURI())
}

// SameOrigin reports whether a request came from a page on host. Browsers
// send Origin with every POST; a request without one did not come from a
// page.
func SameOrigin(r *http.Request, host string) bool {
	return r.Header.Get("Origin") == hosts.URL(host)
}

// Brand is how a site looks.
type Brand struct {
	Name   string
	Color  string // the accent colour
	Dark   bool
	Footer string
	// Icons is the <link> tags in each page's <head> that declare the
	// site's favicons. Sites declare theirs as differently as real ones do.
	Icons template.HTML
}

// Files serves each file in dir of fsys at prefix and its name, such as a
// site's favicons at /favicon.ico.
func Files(mux *http.ServeMux, fsys fs.FS, dir, prefix string) {
	names, err := fs.ReadDir(fsys, dir)
	if err != nil {
		panic(err)
	}
	for _, n := range names {
		name := n.Name()
		mux.HandleFunc("GET "+prefix+name, func(w http.ResponseWriter, r *http.Request) {
			http.ServeFileFS(w, r, fsys, path.Join(dir, name))
		})
	}
}

// View is what a page template gets.
type View struct {
	Brand Brand
	Title string
	Data  any
}

//go:embed layout.html
var layout embed.FS

// Pages parses each page in an app's templates folder with the shared
// layout. A page defines "content", and may define "head" and "header".
// Files in templates/parts are parts every page can use.
type Pages struct {
	brand Brand
	pages map[string]*template.Template
}

func NewPages(brand Brand, fsys fs.FS, funcs template.FuncMap) *Pages {
	p := &Pages{brand: brand, pages: map[string]*template.Template{}}
	names, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		panic(err)
	}
	parts, _ := fs.Glob(fsys, "templates/parts/*.html")
	for _, name := range names {
		t := template.New("layout.html").Funcs(Funcs).Funcs(funcs)
		t = template.Must(t.ParseFS(layout, "layout.html"))
		if len(parts) > 0 {
			t = template.Must(t.ParseFS(fsys, parts...))
		}
		p.pages[strings.TrimSuffix(path.Base(name), ".html")] = template.Must(t.ParseFS(fsys, name))
	}
	return p
}

// Render writes a page.
func (p *Pages) Render(w http.ResponseWriter, status int, page, title string, data any) {
	t := p.pages[page]
	if t == nil {
		panic("no page " + page)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.Execute(w, View{Brand: p.brand, Title: title, Data: data}); err != nil {
		log.Printf("page %s: %v", page, err)
	}
}

// Funcs are the template functions every page has.
var Funcs = template.FuncMap{
	"money": Money,
	"url":   hosts.URL,
	"date":  Date,
}

// Date formats an ISO date (2026-09-03, or a time) as "Sep 3, 2026".
func Date(iso string) string {
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if t, err := time.Parse(layout, iso); err == nil {
			return t.Format("Jan 2, 2006")
		}
	}
	return iso
}

// Money formats cents as US dollars: $1,234.56, or -$12.00.
func Money(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	whole := fmt.Sprint(cents / 100)
	var b strings.Builder
	for i, c := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return fmt.Sprintf("%s$%s.%02d", sign, b.String(), cents%100)
}
