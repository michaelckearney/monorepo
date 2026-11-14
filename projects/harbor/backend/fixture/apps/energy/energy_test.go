package energy

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/mail"
)

func TestSignInByEmailAndExport(t *testing.T) {
	defer func(d time.Duration) { exportTakes = d }(exportTakes)
	exportTakes = 100 * time.Millisecond
	box := &mail.Box{}
	pages, api := New(fake.Store(t), box)
	b := fake.New(t, hosts.Energy, pages)
	b.Post("/login", url.Values{"email": {"Alex@Example.localhost"}})
	msgs := box.List("alex@example.localhost")
	if len(msgs) != 1 {
		t.Fatalf("mail: %+v", msgs)
	}
	link := regexp.MustCompile(`/login/verify\?token=\S+`).FindString(msgs[0].Text)
	if p := b.Get(link); p.Path != "/account" {
		t.Fatalf("the link led to %s", p.Path)
	}
	if p := b.Get(link); p.Status != 410 {
		t.Errorf("the link worked twice: %d", p.Status)
	}

	// The API is another host on the same parent domain: same cookie.
	b.Serve(hosts.EnergyAPI, api)
	a := b.At(hosts.EnergyAPI)
	a.Header.Set("Origin", hosts.URL(hosts.Energy))
	p := a.Get("/v1/usage?from=2026-09&to=2026-09&by=month")
	if p.Header.Get("Access-Control-Allow-Origin") != hosts.URL(hosts.Energy) || !strings.Contains(p.Body, `"period":"2026-09"`) {
		t.Fatalf("usage: %v %s", p.Header, p.Body)
	}
	p = a.JSON("POST", "/v1/exports", `{"from":"2026-09-01","to":"2026-09-03"}`, nil)
	if p.Status != 202 || !strings.Contains(p.Body, `"status":"pending"`) {
		t.Fatalf("export: %d %s", p.Status, p.Body)
	}
	id := regexp.MustCompile(`exp_[\w-]+`).FindString(p.Body)
	if p := a.Get("/v1/exports/" + id + "/file"); p.Status != 409 {
		t.Errorf("the file before it is ready: %d", p.Status)
	}
	time.Sleep(exportTakes)
	p = a.Get("/v1/exports/" + id + "/file")
	if lines := strings.Split(strings.TrimSpace(p.Body), "\n"); len(lines) != 4 || lines[0] != "date,kwh" {
		t.Errorf("file:\n%s", p.Body)
	}

	other := fake.New(t, hosts.EnergyAPI, api)
	if p := other.Get("/v1/bills"); p.Status != 401 {
		t.Errorf("no cookie: %d", p.Status)
	}
}
