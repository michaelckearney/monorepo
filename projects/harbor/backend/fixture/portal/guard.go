package portal

import (
	"net/http"
	"strings"
)

// The headers Guard tells the app who is signed in with, as Authelia does.
var remote = []string{"Remote-User", "Remote-Name", "Remote-Email", "Remote-Groups"}

// Guard puts the portal in front of next, as a reverse proxy such as
// Traefik or Caddy asks Authelia about each request to a self-hosted app:
// a signed-in request reaches next with the user in the Remote-* headers.
// Otherwise a browser opening a page goes to the login page, and anything
// else, such as a fetch, gets a 401.
func (p *Portal) Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only the portal says who the user is.
		for _, h := range remote {
			r.Header.Del(h)
		}
		if s := p.session(r); s != nil {
			r.Header.Set("Remote-User", s.user.Username)
			r.Header.Set("Remote-Name", s.user.Name)
			r.Header.Set("Remote-Email", s.user.Email)
			r.Header.Set("Remote-Groups", strings.Join(s.user.Groups, ","))
			next.ServeHTTP(w, r)
			return
		}
		page := (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			r.Header.Get("X-Requested-With") != "XMLHttpRequest" &&
			strings.Contains(r.Header.Get("Accept"), "text/html")
		if page {
			http.Redirect(w, r, p.LoginURL("https://"+r.Host+r.URL.RequestURI(), false), http.StatusFound)
			return
		}
		http.Error(w, "401 Unauthorized", http.StatusUnauthorized)
	})
}
