// Package shop is Parcel: an online shop's order history.
//
// Scenarios: the first orders embedded in the page as JSON (D6), more on
// scroll (D13), a list of orders then each order's details (D14); a
// readable XSRF-TOKEN cookie that the page copies into a header (S2, V7); a
// form with CSRF and hidden fields (A1, A2), then a code sent by email (A7).
package shop

import (
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/mail"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data templates icons
var files embed.FS

type Item struct {
	SKU      string `json:"sku"`
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Price    int64  `json:"priceCents"`
}

type Order struct {
	ID       string          `json:"id"`
	Placed   string          `json:"placed"`
	Status   string          `json:"status"`
	Items    []Item          `json:"items"`
	Subtotal int64           `json:"subtotalCents"`
	Shipping int64           `json:"shippingCents"`
	Tax      int64           `json:"taxCents"`
	Total    int64           `json:"totalCents"`
	ShipTo   string          `json:"shipTo"`
	Tracking json.RawMessage `json:"tracking,omitempty"`
	Returns  []Return        `json:"returns,omitempty"`
}

type Return struct {
	ID      string   `json:"id"`
	SKUs    []string `json:"skus"`
	Reason  string   `json:"reason"`
	Created string   `json:"created"`
	Label   string   `json:"labelUrl"`
}

// Summary is an order in the list: no items or tracking.
type Summary struct {
	ID        string `json:"id"`
	Placed    string `json:"placed"`
	Status    string `json:"status"`
	Total     int64  `json:"totalCents"`
	ItemCount int    `json:"itemCount"`
	FirstItem string `json:"firstItem"`
}

type data struct {
	Customer struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"customer"`
	Orders []*Order `json:"orders"`
}

type login struct {
	CSRF     string
	ReturnTo string
	Email    string
	Code     string // the emailed code, once the password was right
	Tries    int
}

const pageSize = 10

type shop struct {
	pages    *site.Pages
	mail     *mail.Box
	logins   *site.Sessions[*login]
	sessions *site.Sessions[string] // the email signed in

	mu   sync.Mutex
	data data
}

func New(store *site.Store, box *mail.Box) http.Handler {
	s := &shop{
		pages:    site.NewPages(site.Brand{Name: "Parcel", Color: "#7c3aed", Footer: "Parcel: everything, delivered."}, files, nil),
		mail:     box,
		logins:   site.NewSessions[*login](store, site.SessionOptions{Cookie: "_parcel_login", Absolute: 15 * time.Minute}),
		sessions: site.NewSessions[string](store, site.SessionOptions{Cookie: "_parcel_session", Idle: 2 * time.Hour}),
	}
	site.Load(files, "data/orders.json", &s.data)

	mux := http.NewServeMux()
	// Declared nowhere: browsers ask for /favicon.ico themselves.
	site.Files(mux, files, "icons", "/")
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		s.pages.Render(w, 200, "home", "", map[string]any{"SignedIn": s.signedIn(r)})
	})
	mux.HandleFunc("GET /signin", s.signinPage)
	mux.HandleFunc("POST /signin", s.signin)
	mux.HandleFunc("GET /signin/verify", s.verifyPage)
	mux.HandleFunc("POST /signin/verify", s.verify)
	mux.HandleFunc("POST /signout", func(w http.ResponseWriter, r *http.Request) {
		s.sessions.End(w, r)
		http.SetCookie(w, &http.Cookie{Name: "XSRF-TOKEN", Path: "/", MaxAge: -1, Secure: true})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /orders", s.page(s.orders))
	mux.HandleFunc("GET /orders/{id}", s.page(func(w http.ResponseWriter, r *http.Request) {
		s.pages.Render(w, 200, "order", "Order "+r.PathValue("id"), map[string]any{"ID": r.PathValue("id")})
	}))
	mux.HandleFunc("GET /returns/{id}/label.pdf", s.page(s.label))
	mux.HandleFunc("GET /api/orders", s.api(s.listOrders))
	mux.HandleFunc("GET /api/orders/{id}", s.api(s.order))
	mux.HandleFunc("POST /api/orders/{id}/returns", s.api(s.startReturn))
	return mux
}

func (s *shop) signedIn(r *http.Request) bool {
	sess, _ := s.sessions.Get(r)
	return sess != nil
}

// page sends a signed-out browser to sign in, and back.
func (s *shop) page(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.signedIn(r) {
			http.Redirect(w, r, "/signin?return_to="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

// api answers a signed-out request with 401, and checks the XSRF header on
// writes: the page copies the XSRF-TOKEN cookie into X-XSRF-TOKEN (V7).
func (s *shop) api(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.signedIn(r) {
			site.Fail(w, http.StatusUnauthorized, "unauthenticated", "Sign in at "+hosts.URL(hosts.Shop)+"/signin.")
			return
		}
		if r.Method != http.MethodGet {
			c, err := r.Cookie("XSRF-TOKEN")
			if err != nil || c.Value == "" || r.Header.Get("X-XSRF-TOKEN") != c.Value {
				site.Fail(w, http.StatusForbidden, "invalid_csrf_token", "The X-XSRF-TOKEN header must match the XSRF-TOKEN cookie.")
				return
			}
		}
		next(w, r)
	}
}

// ---- Signing in

func (s *shop) signinPage(w http.ResponseWriter, r *http.Request) {
	l := &login{CSRF: site.Token(), ReturnTo: site.Next(r.URL.Query().Get("return_to"), "/orders")}
	s.logins.Start(w, l)
	s.pages.Render(w, 200, "signin", "Sign in", map[string]any{"Login": l})
}

// flow returns the sign-in in progress, after checking its CSRF token.
// Save it after changing it.
func (s *shop) flow(w http.ResponseWriter, r *http.Request) *site.Session[*login] {
	sess, _ := s.logins.Get(r)
	if sess == nil || !site.SameOrigin(r, hosts.Shop) {
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return nil
	}
	if r.PostFormValue("authenticity_token") != sess.Data.CSRF {
		http.Error(w, "Invalid authenticity token. Reload the page and try again.", http.StatusUnprocessableEntity)
		return nil
	}
	return sess
}

func (s *shop) signin(w http.ResponseWriter, r *http.Request) {
	sess := s.flow(w, r)
	if sess == nil {
		return
	}
	l := sess.Data
	if r.PostFormValue("utf8") != "✓" || r.PostFormValue("commit") != "Sign in" {
		// Parcel's servers reject a form that is not the one its page sends.
		http.Error(w, "Bad request.", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("session[email]")))
	if email != s.data.Customer.Email || r.PostFormValue("session[password]") != site.Password {
		s.pages.Render(w, 401, "signin", "Sign in", map[string]any{"Login": l, "Error": "Your email or password is incorrect."})
		return
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(1_000_000))
	l.Email, l.Code, l.Tries = email, fmt.Sprintf("%06d", n.Int64()), 0
	s.logins.Save(sess)
	s.mail.Send("account-update@parcel.localhost", email, "Your Parcel sign-in code: "+l.Code,
		"Your one-time code is "+l.Code+". It expires in 15 minutes.\n\nParcel will never ask you for this code by phone.")
	http.Redirect(w, r, "/signin/verify", http.StatusSeeOther)
}

func (s *shop) verifyPage(w http.ResponseWriter, r *http.Request) {
	sess, _ := s.logins.Get(r)
	if sess == nil || sess.Data.Code == "" {
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return
	}
	s.pages.Render(w, 200, "verify", "Verify", map[string]any{"Login": sess.Data})
}

func (s *shop) verify(w http.ResponseWriter, r *http.Request) {
	sess := s.flow(w, r)
	if sess == nil || sess.Data.Code == "" {
		return
	}
	l := sess.Data
	if strings.TrimSpace(r.PostFormValue("otp")) != l.Code {
		l.Tries++
		if l.Tries >= 3 {
			s.logins.End(w, r)
			s.pages.Render(w, 401, "signin", "Sign in", map[string]any{"Login": &login{CSRF: "", ReturnTo: l.ReturnTo},
				"Error": "Too many wrong codes. Sign in again for a new one."})
			return
		}
		s.logins.Save(sess)
		s.pages.Render(w, 401, "verify", "Verify", map[string]any{"Login": l, "Error": "That code is not right."})
		return
	}
	s.logins.End(w, r)
	s.sessions.Start(w, l.Email)
	// Readable by the page's JavaScript, which sends it back as a header.
	http.SetCookie(w, &http.Cookie{Name: "XSRF-TOKEN", Value: site.Token(), Path: "/", Secure: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, l.ReturnTo, http.StatusSeeOther)
}

// ---- Orders

func summarize(o *Order) Summary {
	n := 0
	for _, it := range o.Items {
		n += it.Quantity
	}
	return Summary{ID: o.ID, Placed: o.Placed, Status: o.Status, Total: o.Total, ItemCount: n, FirstItem: o.Items[0].Name}
}

// after returns a page of orders after a cursor, and the next cursor ("" at
// the end). Cursors are opaque to the page.
func (s *shop) after(cursor string, limit int) ([]Summary, string, error) {
	start := 0
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || !strings.HasPrefix(string(raw), "after:") {
			return nil, "", fmt.Errorf("bad cursor")
		}
		start = -1
		for i, o := range s.data.Orders {
			if o.ID == strings.TrimPrefix(string(raw), "after:") {
				start = i + 1
			}
		}
		if start < 0 {
			return nil, "", fmt.Errorf("bad cursor")
		}
	}
	end := min(start+limit, len(s.data.Orders))
	out := []Summary{}
	for _, o := range s.data.Orders[start:end] {
		out = append(out, summarize(o))
	}
	next := ""
	if end < len(s.data.Orders) {
		next = base64.RawURLEncoding.EncodeToString([]byte("after:" + s.data.Orders[end-1].ID))
	}
	return out, next, nil
}

func (s *shop) orders(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	first, next, _ := s.after("", pageSize)
	s.mu.Unlock()
	state, _ := json.Marshal(map[string]any{"orders": first, "nextCursor": next, "customer": s.data.Customer.Name})
	s.pages.Render(w, 200, "orders", "Your orders", map[string]any{"State": template.JS(state)})
}

func (s *shop) listOrders(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50 {
		limit = pageSize
	}
	s.mu.Lock()
	out, next, err := s.after(r.URL.Query().Get("cursor"), limit)
	s.mu.Unlock()
	if err != nil {
		site.Fail(w, 400, "bad_cursor", "That cursor is not one this API gave out.")
		return
	}
	site.JSON(w, 200, map[string]any{"orders": out, "nextCursor": next})
}

func (s *shop) find(id string) *Order {
	for _, o := range s.data.Orders {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func (s *shop) order(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o := s.find(r.PathValue("id")); o != nil {
		site.JSON(w, 200, o)
		return
	}
	site.Fail(w, 404, "not_found", "No order "+r.PathValue("id")+".")
}

func (s *shop) startReturn(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SKUs   []string `json:"skus"`
		Reason string   `json:"reason"`
	}
	if !site.Read(w, r, &in) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.find(r.PathValue("id"))
	if o == nil {
		site.Fail(w, 404, "not_found", "No order "+r.PathValue("id")+".")
		return
	}
	if o.Status != "delivered" {
		site.Fail(w, 422, "not_returnable", "Only delivered orders can be returned. This one is "+o.Status+".")
		return
	}
	if len(in.SKUs) == 0 || strings.TrimSpace(in.Reason) == "" {
		site.Fail(w, 422, "invalid", "Send skus (at least one) and a reason.")
		return
	}
	for _, sku := range in.SKUs {
		found := false
		for _, it := range o.Items {
			found = found || it.SKU == sku
		}
		if !found {
			site.Fail(w, 422, "invalid", "This order has no item "+sku+".")
			return
		}
	}
	id := fmt.Sprintf("R%s-%d", o.ID[1:8], len(o.Returns)+1)
	ret := Return{ID: id, SKUs: in.SKUs, Reason: in.Reason, Created: time.Now().UTC().Format(time.RFC3339),
		Label: hosts.URL(hosts.Shop) + "/returns/" + id + "/label.pdf"}
	o.Returns = append(o.Returns, ret)
	site.JSON(w, 201, ret)
}

func (s *shop) label(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.data.Orders {
		for _, ret := range o.Returns {
			if ret.ID == r.PathValue("id") {
				w.Header().Set("Content-Type", "application/pdf")
				w.Write(site.PDF("Parcel return label", []string{
					"Return " + ret.ID + " for order " + o.ID, "Items: " + strings.Join(ret.SKUs, ", "), "Reason: " + ret.Reason, "",
					"Drop this package at any Parcel drop-off point.",
				}))
				return
			}
		}
	}
	http.NotFound(w, r)
}
