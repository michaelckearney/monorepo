package bank

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

const secret = "TIDEWATERBANKHARBORFIXTURESALEXQ"

// signIn signs in from /accounts/cc-1194, and returns the page it lands on.
func signIn(t *testing.T, b *fake.Browser, remember bool) fake.Page {
	t.Helper()
	p := b.Get("/accounts/cc-1194")
	if p.Path != "/login?next=%2Faccounts%2Fcc-1194" {
		t.Fatalf("not sent to sign in: %s", p.Path)
	}
	csrf := fake.Field(p.Body, "authenticity_token")
	p = b.Post("/login", url.Values{"authenticity_token": {csrf}, "username": {"alex@example.localhost"}})
	p = b.Post("/login/password", url.Values{"authenticity_token": {csrf}, "password": {site.Password}})
	if p.Path == "/login/code" {
		form := url.Values{"authenticity_token": {csrf}, "code": {site.TOTP(secret, time.Now())}}
		if remember {
			form.Set("remember", "on")
		}
		p = b.Post("/login/code", form)
		if !strings.Contains(p.Body, "Is this your device?") {
			t.Fatalf("no new-device page: %s %s", p.Path, p.Body)
		}
		p = b.Post("/login/new-device", url.Values{"authenticity_token": {csrf}})
	}
	return p
}

func TestSignInComesBackToThePage(t *testing.T) {
	b := fake.New(t, hosts.Bank, New(fake.Store(t)))
	p := signIn(t, b, true)
	if p.Path != "/accounts/cc-1194" || !strings.Contains(p.Body, "Tidewater Rewards Visa") {
		t.Fatalf("landed on %s", p.Path)
	}
	if !strings.Contains(p.Body, "Page 1 of") || !strings.Contains(p.Body, "-$1,240.00") {
		t.Error("the first page of transactions is missing")
	}
	csv := b.Get("/statements/chk-4821-2026-08.csv")
	if !strings.HasPrefix(csv.Body, "Date,Description,Category,Amount,Balance\n") || !strings.Contains(csv.Body, "MAPLE GROVE APTS RENT") {
		t.Errorf("statement:\n%s", csv.Body)
	}
	if pdf := b.Get("/statements/chk-4821-2026-08.pdf"); !strings.HasPrefix(pdf.Body, "%PDF-1.4") {
		t.Error("the PDF statement is not a PDF")
	}

	// A remembered device skips the code.
	b.Post("/logout", url.Values{})
	if p = signIn(t, b, false); p.Path != "/accounts/cc-1194" {
		t.Errorf("a remembered device: landed on %s", p.Path)
	}
}

func TestANewSignInEndsTheOldSession(t *testing.T) {
	app := New(fake.Store(t))
	first, second := fake.New(t, hosts.Bank, app), fake.New(t, hosts.Bank, app)
	signIn(t, first, false)
	signIn(t, second, false)
	p := first.Get("/accounts")
	if !strings.Contains(p.Body, "You signed in on another device, so this session ended.") {
		t.Errorf("%s\n%s", p.Path, p.Body)
	}
}

func TestOriginAndCSRF(t *testing.T) {
	b := fake.New(t, hosts.Bank, New(fake.Store(t)))
	signIn(t, b, false)
	p := b.Do("POST", "/transfer", strings.NewReader("from=chk-4821"), map[string][]string{
		"Origin": {"https://evil.localhost:8443"}, "Content-Type": {"application/x-www-form-urlencoded"}})
	if p.Status != 403 {
		t.Errorf("another origin: %d", p.Status)
	}
	p = b.Post("/transfer", url.Values{"from": {"chk-4821"}, "to": {"sav-7730"}, "amount": {"10"}})
	if p.Status != 403 {
		t.Errorf("no CSRF token: %d", p.Status)
	}
}

func TestTransfer(t *testing.T) {
	b := fake.New(t, hosts.Bank, New(fake.Store(t)))
	signIn(t, b, false)
	page := b.Get("/transfer")
	csrf := fake.Field(page.Body, "authenticity_token")
	p := b.Post("/transfer", url.Values{"authenticity_token": {csrf}, "from": {"chk-4821"}, "to": {"sav-7730"}, "amount": {"125.50"}, "memo": {"rainy day"}})
	if !strings.Contains(p.Body, "Moved $125.50 from Everyday Checking to High-Yield Savings.") {
		t.Fatalf("%s\n%s", p.Path, p.Body)
	}
	if acct := b.Get("/accounts/sav-7730"); !strings.Contains(acct.Body, "ONLINE TRANSFER FROM ••••4821 RAINY DAY") {
		t.Error("the transfer is not in savings")
	}
}

func TestLockout(t *testing.T) {
	b := fake.New(t, hosts.Bank, New(fake.Store(t)))
	p := b.Get("/login")
	csrf := fake.Field(p.Body, "authenticity_token")
	b.Post("/login", url.Values{"authenticity_token": {csrf}, "username": {"alex@example.localhost"}})
	for i := range maxFailures {
		p = b.Post("/login/password", url.Values{"authenticity_token": {csrf}, "password": {"wrong"}})
		if i < maxFailures-1 && !strings.Contains(p.Body, "attempts left") {
			t.Fatalf("attempt %d: %s", i, p.Body)
		}
	}
	if p.Status != 423 {
		t.Errorf("after %d failures: %d", maxFailures, p.Status)
	}
	p = b.Post("/login/password", url.Values{"authenticity_token": {csrf}, "password": {site.Password}})
	if p.Status != 423 {
		t.Errorf("the right password while locked: %d", p.Status)
	}
}
