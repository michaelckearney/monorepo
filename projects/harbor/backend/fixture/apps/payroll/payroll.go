// Package payroll is Payday, Northwind's HR and payroll.
//
// Scenarios: batched RPC (D7) whose replies are packed: gzip, then base64
// (D8); an access token the page keeps in memory only, got again on every
// load from a refresh cookie (S6); the password again, at Northwind ID,
// before changing bank details (V13); Northwind ID's sign-in on the server
// (A10), with no form if you are already signed in (A13), and single logout
// with Tasklane (A14).
package payroll

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/northwind"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data static
var files embed.FS

const (
	accessFor  = 5 * time.Minute
	freshFor   = 5 * time.Minute
	clientID   = "payday"
	secret     = "payday-client-secret"
	packedType = "application/x-payday-packed"
)

var redirectURI = hosts.URL(hosts.Payroll) + "/auth/callback"

type session struct {
	Username string
	Fresh    time.Time // when the password was last typed at Northwind ID
}

type login struct {
	Reauth   bool
	ReturnTo string
	Session  string // the session to make fresh, when Reauth
	Expires  time.Time
}

type payroll struct {
	key          []byte
	signedIn     func(context.Context, *http.Request) (bool, error)
	exchangeCode func(context.Context, url.Values, string, string) (northwind.User, error)

	sessions *site.Table[session] // by refresh cookie

	mu     sync.Mutex
	data   data
	logins map[string]*login // by OIDC state
}

func New(store *site.Store, client *http.Client) http.Handler {
	return newPayroll(store, client).handler()
}

func newPayroll(store *site.Store, client *http.Client) *payroll {
	idp := northwind.IdP{Client: client}
	p := &payroll{key: store.Key("payday access tokens"), signedIn: idp.SignedIn, exchangeCode: idp.Exchange,
		sessions: site.NewTable[session](store, "payday sessions"), logins: map[string]*login{}}
	site.Load(files, "data/payroll.json", &p.data)
	return p
}

func (p *payroll) handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(files))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		page, _ := files.ReadFile("static/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})
	mux.HandleFunc("GET /auth/login", p.login)
	mux.HandleFunc("GET /auth/callback", p.callback)
	mux.HandleFunc("POST /api/auth/refresh", p.refresh)
	mux.HandleFunc("POST /api/auth/logout", p.logout)
	mux.HandleFunc("POST /rpc", p.rpc)
	mux.HandleFunc("GET /payslips/{file}", p.payslipPDF)
	return mux
}

// ---- Signing in with Northwind ID, on the server

func (p *payroll) login(w http.ResponseWriter, r *http.Request) {
	l := &login{Reauth: r.URL.Query().Get("reauth") == "1", ReturnTo: site.Next(r.URL.Query().Get("return"), "/"), Expires: time.Now().Add(10 * time.Minute)}
	params := url.Values{"client_id": {clientID}, "redirect_uri": {redirectURI}}
	if l.Reauth {
		c, err := r.Cookie("pd_rt")
		if err != nil {
			http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
			return
		}
		l.Session = c.Value
		// Northwind ID asks for the password even though you are signed in.
		params.Set("prompt", "login")
		params.Set("max_age", "0")
	}
	state := site.Token()
	params.Set("state", state)
	p.mu.Lock()
	p.logins[state] = l
	p.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "pd_state", Value: state, Path: "/auth", MaxAge: 600, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, northwind.AuthorizeURL(params), http.StatusSeeOther)
}

func (p *payroll) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, err := r.Cookie("pd_state")
	p.mu.Lock()
	l := p.logins[q.Get("state")]
	delete(p.logins, q.Get("state"))
	p.mu.Unlock()
	if err != nil || c.Value != q.Get("state") || l == nil || time.Now().After(l.Expires) {
		http.Error(w, "This sign-in did not start here, or took too long. Go to "+hosts.URL(hosts.Payroll)+"/ and try again.", http.StatusBadRequest)
		return
	}
	if e := q.Get("error"); e != "" {
		http.Error(w, "Northwind ID did not sign you in: "+q.Get("error_description"), http.StatusUnauthorized)
		return
	}
	u, err := p.exchangeCode(r.Context(), url.Values{"code": {q.Get("code")}, "redirect_uri": {redirectURI}}, clientID, secret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if u.Username != p.data.Employee.Username {
		http.Error(w, u.Username+" has no Payday account.", http.StatusForbidden)
		return
	}
	if s, ok := p.sessions.Get(l.Session); l.Reauth && ok && s.Username == u.Username {
		s.Fresh = time.Now()
		p.sessions.Put(l.Session, s)
	} else {
		id := "rt_" + site.Token()
		p.sessions.Put(id, session{Username: u.Username})
		// The refresh endpoint needs it, and so does /auth/login, to make
		// this session fresh after the password again. Nothing else sees it.
		http.SetCookie(w, &http.Cookie{Name: "pd_rt", Value: id, Path: "/api/auth", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.SetCookie(w, &http.Cookie{Name: "pd_rt", Value: id, Path: "/auth", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	http.SetCookie(w, &http.Cookie{Name: "pd_state", Path: "/auth", MaxAge: -1, Secure: true})
	http.Redirect(w, r, l.ReturnTo, http.StatusSeeOther)
}

type access struct {
	site.Claims
	Session string `json:"sid"`
}

// refresh gives the page a new access token, which it keeps in memory. It
// ends the session if the browser has signed out of Northwind ID.
func (p *payroll) refresh(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("pd_rt")
	var s session
	ok := false
	if err == nil {
		s, ok = p.sessions.Get(c.Value)
	}
	if !ok {
		site.Fail(w, http.StatusUnauthorized, "signed_out", "Sign in with Northwind ID.")
		return
	}
	if ok, err := p.signedIn(r.Context(), r); err != nil {
		site.Fail(w, http.StatusBadGateway, "idp_unavailable", err.Error())
		return
	} else if !ok {
		p.sessions.Delete(c.Value)
		site.Fail(w, http.StatusUnauthorized, "signed_out", "You signed out of Northwind ID. Sign in again.")
		return
	}
	now := time.Now()
	token := site.SignJWT(p.key, access{Claims: site.Claims{Subject: s.Username, Issued: now.Unix(), Expires: now.Add(accessFor).Unix()}, Session: c.Value})
	site.JSON(w, 200, map[string]any{"accessToken": token, "expiresIn": int(accessFor.Seconds())})
}

func (p *payroll) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("pd_rt"); err == nil {
		p.sessions.Delete(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "pd_rt", Path: "/api/auth", MaxAge: -1, Secure: true})
	http.SetCookie(w, &http.Cookie{Name: "pd_rt", Path: "/auth", MaxAge: -1, Secure: true})
	site.JSON(w, 200, map[string]string{"logoutUrl": northwind.LogoutURL(hosts.URL(hosts.Payroll) + "/")})
}

// bearer returns the access token's claims, or writes a 401.
func (p *payroll) bearer(w http.ResponseWriter, r *http.Request) *access {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	var a access
	if err := site.ParseJWT(p.key, token, &a); err != nil {
		site.Fail(w, http.StatusUnauthorized, "invalid_token", "Send Authorization: Bearer with a token from /api/auth/refresh: "+err.Error())
		return nil
	}
	return &a
}

// ---- RPC

type call struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type result struct {
	ID     json.RawMessage `json:"id"`
	Result any             `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

// pack writes v as Payday's packed format: JSON, gzipped, in base64.
func pack(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	b64 := base64.NewEncoder(base64.StdEncoding, &buf)
	z := gzip.NewWriter(b64)
	json.NewEncoder(z).Encode(v)
	z.Close()
	b64.Close()
	w.Header().Set("Content-Type", packedType)
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

func (p *payroll) rpc(w http.ResponseWriter, r *http.Request) {
	a := p.bearer(w, r)
	if a == nil {
		return
	}
	if r.URL.Query().Get("batch") != "1" {
		pack(w, 400, []result{{Error: &rpcError{Code: "batch_required", Message: "Payday's RPC takes batches only: POST /rpc?batch=1 with a JSON array of calls."}}})
		return
	}
	var calls []call
	if err := json.NewDecoder(r.Body).Decode(&calls); err != nil {
		pack(w, 400, []result{{Error: &rpcError{Code: "parse_error", Message: "The body must be a JSON array of {id, method, params}: " + err.Error()}}})
		return
	}
	out := make([]result, len(calls))
	for i, c := range calls {
		res, err := p.dispatch(a, c)
		out[i] = result{ID: c.ID, Result: res, Error: err}
	}
	pack(w, 200, out)
}

func fail(code, format string, args ...any) *rpcError {
	return &rpcError{Code: code, Message: fmt.Sprintf(format, args...)}
}

func params[T any](c call) (T, *rpcError) {
	var v T
	if len(c.Params) > 0 {
		if err := json.Unmarshal(c.Params, &v); err != nil {
			return v, fail("invalid_params", "%s's params: %v", c.Method, err)
		}
	}
	return v, nil
}

func (p *payroll) dispatch(a *access, c call) (any, *rpcError) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch c.Method {
	case "me.get":
		return p.data.Employee, nil
	case "payslips.list":
		in, err := params[struct{ Year int }](c)
		if err != nil {
			return nil, err
		}
		out := []map[string]any{}
		for _, s := range p.data.Payslips {
			if in.Year == 0 || strings.HasPrefix(s.PayDate, fmt.Sprint(in.Year)) {
				out = append(out, map[string]any{"id": s.ID, "payDate": s.PayDate, "periodStart": s.PeriodStart, "periodEnd": s.PeriodEnd,
					"grossCents": s.Gross, "netCents": s.Net, "pdfUrl": hosts.URL(hosts.Payroll) + "/payslips/" + s.ID + ".pdf"})
			}
		}
		return out, nil
	case "payslips.get":
		in, err := params[struct{ ID string }](c)
		if err != nil {
			return nil, err
		}
		if s := p.payslip(in.ID); s != nil {
			return s, nil
		}
		return nil, fail("not_found", "There is no payslip %q.", in.ID)
	case "timeoff.balances":
		return p.data.TimeOff.Balances, nil
	case "timeoff.list":
		return p.data.TimeOff.Requests, nil
	case "timeoff.request":
		return p.requestTimeOff(c)
	case "timeoff.cancel":
		in, err := params[struct{ ID string }](c)
		if err != nil {
			return nil, err
		}
		for _, t := range p.data.TimeOff.Requests {
			if t.ID == in.ID {
				if t.Status != "pending" {
					return nil, fail("not_pending", "Only pending requests can be cancelled; %s is %s.", t.ID, t.Status)
				}
				t.Status = "cancelled"
				return t, nil
			}
		}
		return nil, fail("not_found", "There is no time-off request %q.", in.ID)
	case "bank.get":
		b := p.data.Bank
		return map[string]string{"bankName": b.BankName, "routingNumber": b.Routing, "accountLast4": b.Account[len(b.Account)-4:], "accountType": b.Type}, nil
	case "bank.update":
		s, ok := p.sessions.Get(a.Session)
		if !ok || time.Since(s.Fresh) > freshFor {
			return nil, &rpcError{Code: "reauth_required", Message: "Confirm your password with Northwind ID to change bank details.",
				Data: map[string]string{"url": hosts.URL(hosts.Payroll) + "/auth/login?reauth=1&return=" + url.QueryEscape("/#bank")}}
		}
		in, err := params[struct {
			BankName      string `json:"bankName"`
			RoutingNumber string `json:"routingNumber"`
			AccountNumber string `json:"accountNumber"`
			AccountType   string `json:"accountType"`
		}](c)
		if err != nil {
			return nil, err
		}
		if len(in.RoutingNumber) != 9 || len(in.AccountNumber) < 4 || (in.AccountType != "checking" && in.AccountType != "savings") || in.BankName == "" {
			return nil, fail("invalid_params", "Send bankName, a 9-digit routingNumber, accountNumber, and accountType (checking or savings).")
		}
		p.data.Bank = bank{BankName: in.BankName, Routing: in.RoutingNumber, Account: in.AccountNumber, Type: in.AccountType}
		return map[string]string{"bankName": in.BankName, "accountLast4": in.AccountNumber[len(in.AccountNumber)-4:]}, nil
	}
	return nil, fail("method_not_found", "There is no method %q.", c.Method)
}

func (p *payroll) payslip(id string) *payslip {
	for i := range p.data.Payslips {
		if p.data.Payslips[i].ID == id {
			return &p.data.Payslips[i]
		}
	}
	return nil
}

func (p *payroll) requestTimeOff(c call) (any, *rpcError) {
	in, err := params[struct {
		Type  string `json:"type"`
		Start string `json:"start"`
		End   string `json:"end"`
		Note  string `json:"note"`
	}](c)
	if err != nil {
		return nil, err
	}
	start, err1 := time.Parse("2006-01-02", in.Start)
	end, err2 := time.Parse("2006-01-02", in.End)
	if err1 != nil || err2 != nil || end.Before(start) {
		return nil, fail("invalid_params", "start and end are dates (YYYY-MM-DD), and end is not before start.")
	}
	var bal *balance
	for i := range p.data.TimeOff.Balances {
		if p.data.TimeOff.Balances[i].Type == in.Type {
			bal = &p.data.TimeOff.Balances[i]
		}
	}
	if bal == nil {
		return nil, fail("invalid_params", "type is vacation, sick, or personal.")
	}
	hours := 0.0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			hours += 8
		}
	}
	if hours > bal.Hours {
		return nil, fail("insufficient_balance", "That is %.0f hours; you have %.1f hours of %s.", hours, bal.Hours, bal.Label)
	}
	t := &timeOff{ID: fmt.Sprintf("TO-%d", 367+len(p.data.TimeOff.Requests)), Type: in.Type, Start: in.Start, End: in.End, Hours: hours, Status: "pending", Note: in.Note}
	p.data.TimeOff.Requests = append(p.data.TimeOff.Requests, t)
	return t, nil
}

func (p *payroll) payslipPDF(w http.ResponseWriter, r *http.Request) {
	a := p.bearer(w, r)
	if a == nil {
		return
	}
	id, ok := strings.CutSuffix(r.PathValue("file"), ".pdf")
	p.mu.Lock()
	s := p.payslip(id)
	emp := p.data.Employee
	p.mu.Unlock()
	if !ok || s == nil {
		http.NotFound(w, r)
		return
	}
	lines := []string{emp.Name + " (" + emp.ID + "), " + emp.Title, "Pay date " + s.PayDate + ", for " + s.PeriodStart + " to " + s.PeriodEnd, "",
		"Gross pay: " + site.Money(s.Gross), ""}
	for _, d := range s.Deductions {
		lines = append(lines, fmt.Sprintf("%-28s %12s", d.Name, "-"+site.Money(d.Amount)))
	}
	lines = append(lines, "", "Net pay: "+site.Money(s.Net), "Deposited to Tidewater Bank ••••"+p.data.Bank.Account[len(p.data.Bank.Account)-4:])
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="Payslip-`+s.PayDate+`.pdf"`)
	w.Write(site.PDF("Northwind Inc. payslip", lines))
}
