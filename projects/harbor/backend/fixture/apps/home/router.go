package home

import (
	"crypto/subtle"
	"net/http"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// Router returns the router's admin page: HTTP Basic, user alex@example.localhost.
func Router(store *site.Store) http.Handler {
	d := load()
	stoks := site.NewTable[bool](store, "homebase router tokens")

	basic := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok || u != "alex@example.localhost" || subtle.ConstantTimeCompare([]byte(p), []byte(site.Password)) != 1 {
				w.Header().Set("WWW-Authenticate", `Basic realm="Homebase router", charset="UTF-8"`)
				http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/admin/", http.StatusFound) })
	mux.HandleFunc("GET /admin/{$}", basic(func(w http.ResponseWriter, r *http.Request) {
		// The page's API calls need this token too; its cookie is only sent
		// under /admin/api.
		stok := site.Token()
		stoks.Put(stok, true)
		http.SetCookie(w, &http.Cookie{Name: "stok", Value: stok, Path: "/admin/api", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		page, _ := files.ReadFile("static/router.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	}))
	api := func(next http.HandlerFunc) http.HandlerFunc {
		return basic(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("stok")
			ok := false
			if err == nil {
				_, ok = stoks.Get(c.Value)
			}
			if !ok {
				site.Fail(w, http.StatusForbidden, "no_stok", "Open /admin/ first: the API needs its session token.")
				return
			}
			next(w, r)
		})
	}
	mux.HandleFunc("GET /admin/api/status", api(func(w http.ResponseWriter, r *http.Request) {
		online := 0
		for _, dev := range d.Devices {
			if dev.Online {
				online++
			}
		}
		site.JSON(w, 200, map[string]any{"model": "HB-AX3000", "firmware": "2.4.1", "wan": map[string]string{"ip": "203.0.113.47", "status": "connected"},
			"uptimeSeconds": 1_314_000, "clientsOnline": online})
	}))
	mux.HandleFunc("GET /admin/api/clients", api(func(w http.ResponseWriter, r *http.Request) {
		site.JSON(w, 200, map[string]any{"clients": d.Devices})
	}))
	return mux
}
