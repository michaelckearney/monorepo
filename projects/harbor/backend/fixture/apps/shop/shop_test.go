package shop

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/mail"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

func TestOrdersAndAReturn(t *testing.T) {
	box := &mail.Box{}
	b := fake.New(t, hosts.Shop, New(fake.Store(t), box))
	p := b.Get("/orders")
	if !strings.HasPrefix(p.Path, "/signin?return_to=") {
		t.Fatalf("not sent to sign in: %s", p.Path)
	}
	form := url.Values{"utf8": {"✓"}, "commit": {"Sign in"}, "authenticity_token": {fake.Field(p.Body, "authenticity_token")},
		"session[email]": {"alex@example.localhost"}, "session[password]": {site.Password}}
	p = b.Post("/signin", form)
	code := regexp.MustCompile(`\d{6}`).FindString(box.List("alex@example.localhost")[0].Subject)
	p = b.Post("/signin/verify", url.Values{"authenticity_token": {form.Get("authenticity_token")}, "otp": {code}})
	if p.Path != "/orders" || !strings.Contains(p.Body, `id="__PARCEL_STATE__"`) {
		t.Fatalf("after the code: %s", p.Path)
	}
	cursor := regexp.MustCompile(`"nextCursor":"([^"]+)"`).FindStringSubmatch(p.Body)[1]
	more := b.Get("/api/orders?cursor=" + cursor)
	if !strings.Contains(more.Body, `"orders":[{`) {
		t.Fatalf("the next page: %s", more.Body)
	}

	order := b.Get("/api/orders/P1007919-001")
	if !strings.Contains(order.Body, `"status":"delivered"`) {
		t.Fatalf("order: %s", order.Body)
	}
	sku := regexp.MustCompile(`PRC-\d+`).FindString(order.Body)
	body := `{"skus":["` + sku + `"],"reason":"Arrived damaged"}`
	if p := b.JSON("POST", "/api/orders/P1007919-001/returns", body, nil); p.Status != 403 {
		t.Errorf("no XSRF header: %d", p.Status)
	}
	p = b.JSON("POST", "/api/orders/P1007919-001/returns", body, http.Header{"X-XSRF-TOKEN": {b.Cookie("XSRF-TOKEN")}})
	if p.Status != 201 {
		t.Errorf("return: %d %s", p.Status, p.Body)
	}
}
