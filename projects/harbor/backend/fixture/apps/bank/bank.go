// Package bank is Tidewater Bank: a classic server-rendered bank.
//
// Scenarios: pages with no API (D1), offset pages (D11), CSV and PDF
// statements (D18), formatted money and dates (D23); an HttpOnly __Host-
// cookie (S1, S10); a 5-minute idle timeout (V2), CSRF tokens in forms
// (V6), an Origin check (V8), one session per user (V10), the password
// again before a transfer (V13); username then password (A3), a TOTP code
// (A6), "remember this device" (A9), a new-device page (A20), a lockout
// after 5 wrong passwords (A21), and back to the first page after sign-in
// (A22).
package bank

import (
	"embed"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data templates icons
var files embed.FS

const (
	idle        = 5 * time.Minute
	confirmFor  = 5 * time.Minute
	maxFailures = 5
	lockout     = 15 * time.Minute
	pageSize    = 20
)

type Account struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Number         string `json:"number"`
	Kind           string `json:"kind"`
	OpeningBalance int64  `json:"openingBalanceCents"`
	Balance        int64  `json:"-"`
}

type Transaction struct {
	ID          string `json:"id"`
	Account     string `json:"account"`
	Date        string `json:"date"`
	Description string `json:"description"`
	Amount      int64  `json:"amountCents"`
	Category    string `json:"category"`
	Balance     int64  `json:"-"`
}

type data struct {
	User struct {
		Username   string `json:"username"`
		Name       string `json:"name"`
		TOTPSecret string `json:"totpSecret"`
	} `json:"user"`
	Accounts     []*Account     `json:"accounts"`
	OpeningDate  string         `json:"openingDate"`
	Transactions []*Transaction `json:"transactions"`
}

// login is a sign-in in progress, kept until it finishes.
type login struct {
	CSRF     string
	Username string
	Next     string
	Password bool // the password was right
	Code     bool // the code was right, or the device was remembered
	Remember bool
}

type session struct {
	Username  string
	CSRF      string
	Confirmed time.Time // when the password was last typed
}

type bank struct {
	pages    *site.Pages
	logins   *site.Sessions[*login]
	sessions *site.Sessions[*session]

	mu       sync.Mutex
	data     data
	failures []time.Time            // wrong passwords, for the lockout
	devices  *site.Table[time.Time] // remembered device tokens, and when they expire
	flash    map[string]string      // a message for a session's next page
}

func New(store *site.Store) http.Handler {
	b := &bank{
		pages: site.NewPages(site.Brand{Name: "Tidewater Bank", Color: "#0b5c74", Footer: "Member FDIC. Equal Housing Lender.",
			// An ICO for old browsers, and an SVG for the rest, as most sites now do.
			Icons: `<link rel="icon" href="/favicon.ico" sizes="32x32">
<link rel="icon" href="/favicon.svg" type="image/svg+xml">`}, files, nil),
		logins: site.NewSessions[*login](store, site.SessionOptions{
			Cookie: "__Host-tw_login", Absolute: 10 * time.Minute, SameSite: http.SameSiteStrictMode,
		}),
		sessions: site.NewSessions[*session](store, site.SessionOptions{
			Cookie: "__Host-tw_session", Idle: idle, SameSite: http.SameSiteStrictMode,
		}),
		devices: site.NewTable[time.Time](store, "tidewater devices"),
		flash:   map[string]string{},
	}
	site.Load(files, "data/bank.json", &b.data)
	b.balance()

	mux := http.NewServeMux()
	site.Files(mux, files, "icons", "/")
	mux.HandleFunc("GET /{$}", b.home)
	mux.HandleFunc("GET /login", b.loginPage)
	mux.HandleFunc("POST /login", b.post(b.username))
	mux.HandleFunc("GET /login/password", b.loginStep("password"))
	mux.HandleFunc("POST /login/password", b.post(b.password))
	mux.HandleFunc("GET /login/code", b.loginStep("code"))
	mux.HandleFunc("POST /login/code", b.post(b.code))
	mux.HandleFunc("GET /login/new-device", b.loginStep("new-device"))
	mux.HandleFunc("POST /login/new-device", b.post(b.newDevice))
	mux.HandleFunc("POST /logout", b.post(b.logout))

	mux.HandleFunc("GET /accounts", b.auth(b.accounts))
	mux.HandleFunc("GET /accounts/{id}", b.auth(b.account))
	mux.HandleFunc("GET /statements", b.auth(b.statements))
	mux.HandleFunc("GET /statements/{file}", b.auth(b.statement))
	mux.HandleFunc("GET /transfer", b.auth(b.transferPage))
	mux.HandleFunc("POST /transfer", b.post(b.auth(b.transfer)))
	mux.HandleFunc("GET /confirm-password", b.auth(b.confirmPage))
	mux.HandleFunc("POST /confirm-password", b.post(b.auth(b.confirm)))
	return mux
}

// balance works out each account's balance, and each transaction's running
// balance. Call it with mu held, or before serving.
func (b *bank) balance() {
	for _, a := range b.data.Accounts {
		a.Balance = a.OpeningBalance
		for _, t := range b.data.Transactions {
			if t.Account == a.ID {
				a.Balance += t.Amount
				t.Balance = a.Balance
			}
		}
	}
}

func (b *bank) account_(id string) *Account {
	for _, a := range b.data.Accounts {
		if a.ID == id {
			return a
		}
	}
	return nil
}

// post rejects a POST from anywhere but the bank's own pages (V8).
func (b *bank) post(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !site.SameOrigin(r, hosts.Bank) {
			http.Error(w, "This request did not come from a Tidewater Bank page, so it was blocked.", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

type authed func(w http.ResponseWriter, r *http.Request, s *site.Session[*session])

// auth sends a request with no session to sign in, and back here after.
func (b *bank) auth(next authed) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, why := b.sessions.Get(r)
		if s == nil {
			to := site.LoginURL("/login", r)
			if r.Method != http.MethodGet {
				to = "/login"
			}
			if why != "" {
				to += sep(to) + "ended=" + url.QueryEscape(why)
			}
			http.Redirect(w, r, to, http.StatusSeeOther)
			return
		}
		next(w, r, s)
	}
}

func sep(u string) string {
	if strings.Contains(u, "?") {
		return "&"
	}
	return "?"
}

func (b *bank) home(w http.ResponseWriter, r *http.Request) {
	if s, _ := b.sessions.Get(r); s != nil {
		http.Redirect(w, r, "/accounts", http.StatusSeeOther)
		return
	}
	b.pages.Render(w, 200, "home", "", nil)
}

// ---- Signing in

func (b *bank) loginPage(w http.ResponseWriter, r *http.Request) {
	l := &login{CSRF: site.Token(), Next: site.Next(r.URL.Query().Get("next"), "/accounts")}
	b.logins.Start(w, l)
	b.pages.Render(w, 200, "login", "Sign in", map[string]any{"Login": l, "Notice": r.URL.Query().Get("ended")})
}

// loginStep shows a later step of signing in, if the sign-in has got that far.
func (b *bank) loginStep(step string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, _ := b.logins.Get(r)
		if s == nil || !b.reached(s.Data, step) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		b.pages.Render(w, 200, "login-"+step, "Sign in", map[string]any{"Login": s.Data})
	}
}

func (b *bank) reached(l *login, step string) bool {
	switch step {
	case "password":
		return l.Username != ""
	case "code":
		return l.Password
	case "new-device":
		return l.Password && l.Code
	}
	return false
}

// flow returns the sign-in in progress, after checking its CSRF token.
// Save it after changing it.
func (b *bank) flow(w http.ResponseWriter, r *http.Request) *site.Session[*login] {
	s, _ := b.logins.Get(r)
	if s == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return nil
	}
	if r.PostFormValue("authenticity_token") != s.Data.CSRF {
		http.Error(w, "This form expired. Go back, reload the page, and try again.", http.StatusForbidden)
		return nil
	}
	return s
}

func (b *bank) username(w http.ResponseWriter, r *http.Request) {
	s := b.flow(w, r)
	if s == nil {
		return
	}
	l := s.Data
	// Any username goes on to the password, so the form does not say which
	// usernames exist.
	l.Username = strings.TrimSpace(r.PostFormValue("username"))
	if l.Username == "" {
		b.pages.Render(w, 422, "login", "Sign in", map[string]any{"Login": l, "Error": "Enter your username."})
		return
	}
	b.logins.Save(s)
	http.Redirect(w, r, "/login/password", http.StatusSeeOther)
}

func (b *bank) password(w http.ResponseWriter, r *http.Request) {
	s := b.flow(w, r)
	if s == nil || !b.reached(s.Data, "password") {
		return
	}
	l := s.Data
	b.mu.Lock()
	now := time.Now()
	b.failures = slices.DeleteFunc(b.failures, func(t time.Time) bool { return now.Sub(t) > lockout })
	locked := len(b.failures) >= maxFailures
	right := l.Username == b.data.User.Username && r.PostFormValue("password") == site.Password
	if !locked && !right {
		b.failures = append(b.failures, now)
	}
	left := maxFailures - len(b.failures)
	b.mu.Unlock()

	switch {
	case locked:
		b.pages.Render(w, 423, "locked", "Account locked", nil)
		return
	case !right && left <= 0:
		b.pages.Render(w, 423, "locked", "Account locked", nil)
		return
	case !right:
		b.pages.Render(w, 401, "login-password", "Sign in", map[string]any{"Login": l,
			"Error": fmt.Sprintf("That username and password do not match. %d attempts left before your account is locked.", left)})
		return
	}
	b.mu.Lock()
	b.failures = nil
	b.mu.Unlock()
	l.Password = true
	if b.remembered(r) {
		l.Code = true
		b.finish(w, r, l)
		return
	}
	b.logins.Save(s)
	http.Redirect(w, r, "/login/code", http.StatusSeeOther)
}

func (b *bank) remembered(r *http.Request) bool {
	c, err := r.Cookie("__Host-tw_device")
	if err != nil {
		return false
	}
	expires, _ := b.devices.Get(c.Value)
	return time.Now().Before(expires)
}

func (b *bank) code(w http.ResponseWriter, r *http.Request) {
	s := b.flow(w, r)
	if s == nil || !b.reached(s.Data, "code") {
		return
	}
	l := s.Data
	code := strings.ReplaceAll(r.PostFormValue("code"), " ", "")
	if !site.TOTPValid(b.data.User.TOTPSecret, code, time.Now()) {
		b.pages.Render(w, 401, "login-code", "Sign in", map[string]any{"Login": l, "Error": "That code is not right. Codes change every 30 seconds."})
		return
	}
	l.Code = true
	l.Remember = r.PostFormValue("remember") == "on"
	b.logins.Save(s)
	http.Redirect(w, r, "/login/new-device", http.StatusSeeOther)
}

// newDevice is the "Is this your device?" page, after a code.
func (b *bank) newDevice(w http.ResponseWriter, r *http.Request) {
	s := b.flow(w, r)
	if s == nil || !b.reached(s.Data, "new-device") {
		return
	}
	l := s.Data
	if l.Remember {
		token := site.Token()
		b.devices.Put(token, time.Now().Add(30*24*time.Hour))
		http.SetCookie(w, &http.Cookie{Name: "__Host-tw_device", Value: token, Path: "/", MaxAge: 30 * 24 * 3600,
			Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	}
	b.finish(w, r, l)
}

// finish signs in, ending the user's other sessions (V10).
func (b *bank) finish(w http.ResponseWriter, r *http.Request, l *login) {
	b.logins.End(w, r)
	b.sessions.EndWhere(func(s *session) bool { return s.Username == l.Username },
		"You signed in on another device, so this session ended.")
	b.sessions.Start(w, &session{Username: l.Username, CSRF: site.Token(), Confirmed: time.Now()})
	http.Redirect(w, r, l.Next, http.StatusSeeOther)
}

func (b *bank) logout(w http.ResponseWriter, r *http.Request) {
	b.sessions.End(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ---- Signed in

// view is what every signed-in page gets.
func (b *bank) view(s *site.Session[*session], extra map[string]any) map[string]any {
	csrf := s.Data.CSRF
	b.mu.Lock()
	flash := b.flash[s.ID]
	delete(b.flash, s.ID)
	b.mu.Unlock()
	v := map[string]any{"CSRF": csrf, "Name": b.data.User.Name, "Flash": flash}
	for k, val := range extra {
		v[k] = val
	}
	return v
}

func (b *bank) accounts(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	b.mu.Lock()
	accounts := make([]Account, len(b.data.Accounts))
	for i, a := range b.data.Accounts {
		accounts[i] = *a
	}
	b.mu.Unlock()
	b.pages.Render(w, 200, "accounts", "Accounts", b.view(s, map[string]any{"Accounts": accounts}))
}

func (b *bank) account(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	b.mu.Lock()
	a := b.account_(r.PathValue("id"))
	var txs []Transaction
	if a != nil {
		for _, t := range slices.Backward(b.data.Transactions) {
			if t.Account == a.ID {
				txs = append(txs, *t)
			}
		}
	}
	var acct Account
	if a != nil {
		acct = *a
	}
	b.mu.Unlock()
	if a == nil {
		http.NotFound(w, r)
		return
	}
	pages := max(1, (len(txs)+pageSize-1)/pageSize)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	page = min(max(page, 1), pages)
	start := (page - 1) * pageSize
	end := min(start+pageSize, len(txs))
	b.pages.Render(w, 200, "account", acct.Name, b.view(s, map[string]any{
		"Account": acct, "Transactions": txs[start:end], "Page": page, "Pages": pages,
		"Prev": page - 1, "Next": page + 1, "From": start + 1, "To": end, "Total": len(txs),
	}))
}

// month is one month of an account's transactions.
type month struct {
	ID      string // chk-4821-2026-08
	Account Account
	Month   time.Time
	Opening int64
	Closing int64
	Txs     []Transaction
}

func (b *bank) months() []month {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []month
	for _, a := range b.data.Accounts {
		bal := a.OpeningBalance
		byMonth := map[string]*month{}
		var order []string
		for _, t := range b.data.Transactions {
			if t.Account != a.ID {
				continue
			}
			key := t.Date[:7]
			m := byMonth[key]
			if m == nil {
				when, _ := time.Parse("2006-01", key)
				m = &month{ID: a.ID + "-" + key, Account: *a, Month: when, Opening: bal}
				byMonth[key] = m
				order = append(order, key)
			}
			bal += t.Amount
			m.Closing = bal
			m.Txs = append(m.Txs, *t)
		}
		for _, k := range slices.Backward(order) {
			out = append(out, *byMonth[k])
		}
	}
	return out
}

func (b *bank) statements(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	b.pages.Render(w, 200, "statements", "Statements", b.view(s, map[string]any{"Months": b.months()}))
}

func (b *bank) statement(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	file := r.PathValue("file")
	id, ext, _ := strings.Cut(file, ".")
	for _, m := range b.months() {
		if m.ID != id {
			continue
		}
		name := fmt.Sprintf("Tidewater-%s-%s.%s", strings.TrimPrefix(m.Account.Number, "••••"), m.Month.Format("2006-01"), ext)
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		switch ext {
		case "csv":
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			fmt.Fprintln(w, "Date,Description,Category,Amount,Balance")
			bal := m.Opening
			for _, t := range m.Txs {
				bal += t.Amount
				fmt.Fprintf(w, "%s,%q,%s,%.2f,%.2f\n", t.Date, t.Description, t.Category, float64(t.Amount)/100, float64(bal)/100)
			}
		case "pdf":
			w.Header().Set("Content-Type", "application/pdf")
			lines := []string{
				m.Account.Name + " " + m.Account.Number, "Statement for " + m.Month.Format("January 2006"), "",
				"Opening balance: " + site.Money(m.Opening), "Closing balance: " + site.Money(m.Closing), "",
			}
			for _, t := range m.Txs {
				d, _ := time.Parse("2006-01-02", t.Date)
				lines = append(lines, fmt.Sprintf("%s   %-40s %12s", d.Format("Jan 2"), t.Description, site.Money(t.Amount)))
			}
			w.Write(site.PDF("Tidewater Bank statement", lines))
		default:
			http.NotFound(w, r)
		}
		return
	}
	http.NotFound(w, r)
}

// ---- Transfers, which need the password again (V13)

func (b *bank) recent(s *site.Session[*session]) bool {
	return time.Since(s.Data.Confirmed) < confirmFor
}

func (b *bank) transferPage(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	if !b.recent(s) {
		http.Redirect(w, r, "/confirm-password?next=/transfer", http.StatusSeeOther)
		return
	}
	b.mu.Lock()
	accounts := make([]Account, len(b.data.Accounts))
	for i, a := range b.data.Accounts {
		accounts[i] = *a
	}
	b.mu.Unlock()
	b.pages.Render(w, 200, "transfer", "Transfer", b.view(s, map[string]any{"Accounts": accounts}))
}

// csrf checks a signed-in form's token (V6).
func (b *bank) csrf(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) bool {
	ok := r.PostFormValue("authenticity_token") == s.Data.CSRF
	if !ok {
		http.Error(w, "This form expired. Go back, reload the page, and try again.", http.StatusForbidden)
	}
	return ok
}

func (b *bank) transfer(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	if !b.csrf(w, r, s) {
		return
	}
	if !b.recent(s) {
		http.Redirect(w, r, "/confirm-password?next=/transfer", http.StatusSeeOther)
		return
	}
	from, to := r.PostFormValue("from"), r.PostFormValue("to")
	amount, err := parseDollars(r.PostFormValue("amount"))
	memo := strings.TrimSpace(r.PostFormValue("memo"))
	fail := func(msg string) {
		b.mu.Lock()
		accounts := make([]Account, len(b.data.Accounts))
		for i, a := range b.data.Accounts {
			accounts[i] = *a
		}
		b.mu.Unlock()
		b.pages.Render(w, 422, "transfer", "Transfer", b.view(s, map[string]any{"Accounts": accounts, "Error": msg}))
	}
	b.mu.Lock()
	src, dst := b.account_(from), b.account_(to)
	b.mu.Unlock()
	switch {
	case src == nil || dst == nil:
		fail("Choose an account to move money from, and one to move it to.")
		return
	case src == dst:
		fail("Choose two different accounts.")
		return
	case err != nil || amount <= 0:
		fail("Enter an amount, such as 125.00.")
		return
	}
	b.mu.Lock()
	if src.Kind != "credit" && src.Balance < amount {
		b.mu.Unlock()
		fail("There is not enough money in " + src.Name + ".")
		return
	}
	today := time.Now().Format("2006-01-02")
	desc := func(a *Account, dir string) string {
		d := "ONLINE TRANSFER " + dir + " " + a.Number
		if memo != "" {
			d += " " + strings.ToUpper(memo)
		}
		return d
	}
	n := len(b.data.Transactions)
	b.data.Transactions = append(b.data.Transactions,
		&Transaction{ID: fmt.Sprintf("T%05d", n+1), Account: src.ID, Date: today, Description: desc(dst, "TO"), Amount: -amount, Category: "Transfer"},
		&Transaction{ID: fmt.Sprintf("T%05d", n+2), Account: dst.ID, Date: today, Description: desc(src, "FROM"), Amount: amount, Category: "Transfer"})
	b.balance()
	b.flash[s.ID] = fmt.Sprintf("Moved %s from %s to %s.", site.Money(amount), src.Name, dst.Name)
	b.mu.Unlock()
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func parseDollars(s string) (int64, error) {
	s = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(s), ",", ""), "$")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(f*100 + 0.5), nil
}

func (b *bank) confirmPage(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	b.pages.Render(w, 200, "confirm", "Confirm it's you", b.view(s, map[string]any{"Next": site.Next(r.URL.Query().Get("next"), "/accounts")}))
}

func (b *bank) confirm(w http.ResponseWriter, r *http.Request, s *site.Session[*session]) {
	if !b.csrf(w, r, s) {
		return
	}
	next := site.Next(r.PostFormValue("next"), "/accounts")
	if r.PostFormValue("password") != site.Password {
		b.pages.Render(w, 401, "confirm", "Confirm it's you", b.view(s, map[string]any{"Next": next, "Error": "That password is not right."}))
		return
	}
	s.Data.Confirmed = time.Now()
	b.sessions.Save(s)
	http.Redirect(w, r, next, http.StatusSeeOther)
}
