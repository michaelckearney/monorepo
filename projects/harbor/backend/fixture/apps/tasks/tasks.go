// Package tasks is Tasklane, Northwind's issue tracker: a single-page app.
//
// Scenarios: GraphQL (D4) that takes persisted queries only, by hash (D5),
// with cursor pages (D12) and live updates over a WebSocket (D16); an
// access token in localStorage (S4) that lasts 5 minutes (V4), and a
// refresh token that works once: using one twice signs the user out (V5);
// Northwind ID's sign-in from the browser with PKCE (A11), with a consent
// page (A12), and single logout with Payday (A14).
package tasks

import (
	"context"
	"embed"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	graphql "github.com/graph-gophers/graphql-go"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/northwind"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data static
var files embed.FS

const (
	accessFor  = 5 * time.Minute
	refreshFor = 7 * 24 * time.Hour
)

type refresh struct {
	Username string
	Family   string // every token since one sign-in
	Used     bool
	Expires  time.Time
}

type tasks struct {
	key []byte
	// Northwind ID: is the browser signed in, and who does a code sign in.
	signedIn     func(context.Context, *http.Request) (bool, error)
	exchangeCode func(context.Context, url.Values, string, string) (northwind.User, error)
	data         *store
	gql          *graphql.Schema
	page         []byte // index.html
	app          []byte // app.js, with the persisted queries' hashes

	refresh *site.Table[refresh]

	// mu guards watchers, and is held from reading a refresh token to
	// marking it used, so it works once.
	mu       sync.Mutex
	watchers map[chan []byte]bool
}

func New(store *site.Store, client *http.Client) http.Handler {
	return newTasks(store, client).handler()
}

func newTasks(store *site.Store, client *http.Client) *tasks {
	idp := northwind.IdP{Client: client}
	t := &tasks{
		key: store.Key("tasklane access tokens"), signedIn: idp.SignedIn, exchangeCode: idp.Exchange,
		refresh: site.NewTable[refresh](store, "tasklane refresh tokens"), watchers: map[chan []byte]bool{},
	}
	t.data = newStore()
	t.gql = graphql.MustParseSchema(schema, &resolver{t}, graphql.UseFieldResolvers())
	t.page, _ = files.ReadFile("static/index.html")
	app, _ := files.ReadFile("static/app.js")
	hashes, _ := json.Marshal(operationHashes())
	t.app = []byte(strings.Replace(string(app), "__OPERATIONS__", string(hashes), 1))
	return t
}

func (t *tasks) handler() http.Handler {
	mux := http.NewServeMux()
	site.Files(mux, files, "static/icons", "/icons/")
	mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(t.app)
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// Every page is the app; it reads the path itself. So is
		// /favicon.ico: the site declares its icon instead.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(t.page)
	})
	mux.HandleFunc("GET /api/auth/config", func(w http.ResponseWriter, r *http.Request) {
		site.JSON(w, 200, map[string]string{
			"authorizeUrl": northwind.AuthorizeURL(map[string][]string{"client_id": {"tasklane"}, "redirect_uri": {redirectURI}}),
			"logoutUrl":    northwind.LogoutURL(hosts.URL(hosts.Tasks) + "/"),
		})
	})
	mux.HandleFunc("POST /api/auth/exchange", t.exchange)
	mux.HandleFunc("POST /api/auth/refresh", t.refreshToken)
	mux.HandleFunc("POST /api/auth/logout", t.logout)
	mux.HandleFunc("POST /graphql", t.graphql)
	mux.HandleFunc("GET /live", t.live)
	return mux
}

var redirectURI = hosts.URL(hosts.Tasks) + "/callback"

type tokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
}

// issue makes a new access token, and a refresh token in a family.
func (t *tasks) issue(username, family string) tokens {
	now := time.Now()
	access := site.SignJWT(t.key, site.Claims{Subject: username, ID: site.Token(), Issued: now.Unix(), Expires: now.Add(accessFor).Unix()})
	rt := "rt_" + site.Token()
	t.refresh.Put(rt, refresh{Username: username, Family: family, Expires: now.Add(refreshFor)})
	return tokens{AccessToken: access, RefreshToken: rt, ExpiresIn: int(accessFor.Seconds())}
}

// exchange is the app's half of PKCE: the page sends the code and its
// verifier, and Tasklane trades them with Northwind ID for who signed in.
func (t *tasks) exchange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code     string `json:"code"`
		Verifier string `json:"codeVerifier"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	u, err := t.exchangeCode(r.Context(), url.Values{
		"code": {in.Code}, "code_verifier": {in.Verifier}, "redirect_uri": {redirectURI},
	}, "tasklane", "")
	if err != nil {
		site.Fail(w, http.StatusUnauthorized, "sign_in_failed", err.Error())
		return
	}
	if t.data.user(u.Username) == nil {
		site.Fail(w, http.StatusForbidden, "no_account", u.Username+" has no Tasklane account.")
		return
	}
	site.JSON(w, 200, t.issue(u.Username, site.Token()))
}

// refreshToken trades a refresh token for new tokens. Each refresh token
// works once; a second use means it leaked, so its whole family ends. It
// also ends when the browser has signed out of Northwind ID.
func (t *tasks) refreshToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refreshToken"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	t.mu.Lock()
	rt, ok := t.refresh.Get(in.RefreshToken)
	switch {
	case !ok || time.Now().After(rt.Expires):
		t.mu.Unlock()
		site.Fail(w, http.StatusUnauthorized, "invalid_grant", "That refresh token is not valid. Sign in again.")
		return
	case rt.Used:
		t.endFamily(rt.Family)
		t.mu.Unlock()
		site.Fail(w, http.StatusUnauthorized, "refresh_token_reused", "That refresh token was already used, so every session from that sign-in has ended. Sign in again.")
		return
	}
	rt.Used = true
	t.refresh.Put(in.RefreshToken, rt)
	t.mu.Unlock()

	if ok, err := t.signedIn(r.Context(), r); err != nil {
		site.Fail(w, http.StatusBadGateway, "idp_unavailable", err.Error())
		return
	} else if !ok {
		t.endFamily(rt.Family)
		site.Fail(w, http.StatusUnauthorized, "signed_out", "You signed out of Northwind ID. Sign in again.")
		return
	}
	site.JSON(w, 200, t.issue(rt.Username, rt.Family))
}

func (t *tasks) endFamily(family string) {
	t.refresh.Each(func(id string, v refresh) {
		if v.Family == family {
			t.refresh.Delete(id)
		}
	})
}

func (t *tasks) logout(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refreshToken"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	if rt, ok := t.refresh.Get(in.RefreshToken); ok {
		t.endFamily(rt.Family)
	}
	site.JSON(w, 200, map[string]string{"logoutUrl": northwind.LogoutURL(hosts.URL(hosts.Tasks) + "/")})
}

// user returns the access token's user, or writes a GraphQL-style 401.
func (t *tasks) user(w http.ResponseWriter, token string) string {
	var c site.Claims
	err := site.ParseJWT(t.key, token, &c)
	if err == nil {
		return c.Subject
	}
	code := "UNAUTHENTICATED"
	if err == site.ErrExpired {
		code = "TOKEN_EXPIRED"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"message": err.Error(), "extensions": map[string]string{"code": code}}}})
	return ""
}

// live sends every change to an issue to the browser, over a WebSocket.
// Browsers cannot set headers on a WebSocket, so the token is in the URL.
func (t *tasks) live(w http.ResponseWriter, r *http.Request) {
	username := t.user(w, r.URL.Query().Get("access_token"))
	if username == "" {
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{hosts.Tasks + ":" + hosts.Port}})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ch := make(chan []byte, 16)
	t.mu.Lock()
	t.watchers[ch] = true
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.watchers, ch)
		t.mu.Unlock()
	}()
	ctx := conn.CloseRead(r.Context())
	conn.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","user":"`+username+`"}`))
	for {
		select {
		case msg := <-ch:
			if err := conn.Write(ctx, websocket.MessageText, msg); err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// broadcast tells every open WebSocket about a changed issue.
func (t *tasks) broadcast(kind string, issue *Issue) {
	msg, _ := json.Marshal(map[string]any{"type": kind, "issue": issue})
	t.mu.Lock()
	defer t.mu.Unlock()
	for ch := range t.watchers {
		select {
		case ch <- msg:
		default:
		}
	}
}
