// Package home is Homebase, a self-hosted home server, and the router's
// admin page beside it.
//
// Scenarios: Homebase sits behind a login portal, in forward-auth
// mode (A16), and binds its API to the browser that opened it (V9);
// sensor readings stream as server-sent events (D17). The router uses HTTP
// Basic (A5, S12), and its API also needs a cookie scoped to /admin/api
// (S9).
package home

import (
	"crypto/subtle"
	"embed"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data static
var files embed.FS

type Device struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	IP     string `json:"ip"`
	MAC    string `json:"mac"`
	Online bool   `json:"online"`
	Band   string `json:"band"`
}

type Volume struct {
	Name   string `json:"name"`
	SizeGB int    `json:"sizeGB"`
	UsedGB int    `json:"usedGB"`
}

type Sensor struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Unit  string  `json:"unit"`
	Base  float64 `json:"base"`
	Swing float64 `json:"swing"`
}

type data struct {
	Devices []Device `json:"devices"`
	Volumes []Volume `json:"volumes"`
	Sensors []Sensor `json:"sensors"`
}

func load() data {
	var d data
	site.Load(files, "data/home.json", &d)
	return d
}

// New returns Homebase. The front end puts the portal in front of it, which
// sets Remote-User.
func New(store *site.Store) http.Handler {
	d := load()
	key := store.Key("homebase browser binding")
	bind := func(r *http.Request) string { return site.HMAC(key, r.UserAgent()) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "hb_bind", Value: bind(r), Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		page, _ := files.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	api := func(next func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("hb_bind")
			if err != nil || subtle.ConstantTimeCompare([]byte(c.Value), []byte(bind(r))) != 1 {
				site.Fail(w, http.StatusUnauthorized, "session_bound_to_another_browser",
					"This session belongs to another browser. Open Homebase's home page in this one.")
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /api/me", api(func(w http.ResponseWriter, r *http.Request) {
		site.JSON(w, 200, map[string]string{"username": r.Header.Get("Remote-User"), "name": r.Header.Get("Remote-Name")})
	}))
	mux.HandleFunc("GET /api/devices", api(func(w http.ResponseWriter, r *http.Request) {
		site.JSON(w, 200, map[string]any{"devices": d.Devices})
	}))
	mux.HandleFunc("GET /api/storage", api(func(w http.ResponseWriter, r *http.Request) {
		site.JSON(w, 200, map[string]any{"volumes": d.Volumes})
	}))
	mux.HandleFunc("GET /api/sensors", api(func(w http.ResponseWriter, r *http.Request) {
		site.JSON(w, 200, map[string]any{"sensors": readings(d.Sensors, time.Now())})
	}))
	mux.HandleFunc("GET /api/sensors/stream", api(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flush := http.NewResponseController(w).Flush
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			for _, rd := range readings(d.Sensors, time.Now()) {
				fmt.Fprintf(w, "event: reading\ndata: {\"id\":%q,\"value\":%g,\"unit\":%q,\"time\":%q}\n\n", rd.ID, rd.Value, rd.Unit, rd.Time)
			}
			if flush() != nil {
				return
			}
			select {
			case <-tick.C:
			case <-r.Context().Done():
				return
			}
		}
	}))
	return mux
}

type reading struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Time  string  `json:"time"`
}

// readings are each sensor's value at t: a slow wave around its base.
func readings(sensors []Sensor, t time.Time) []reading {
	out := make([]reading, len(sensors))
	for i, s := range sensors {
		v := s.Base + s.Swing*math.Sin(float64(t.Unix())/300+float64(i))
		out[i] = reading{ID: s.ID, Name: s.Name, Value: math.Round(v*10) / 10, Unit: s.Unit, Time: t.UTC().Format(time.RFC3339)}
	}
	return out
}
