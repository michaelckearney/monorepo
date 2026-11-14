package health

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

func TestChooseAPersonThenTheTermsThenTheAPI(t *testing.T) {
	b := fake.New(t, hosts.Health, New(fake.Store(t)))
	client := http.Header{ClientHeader: {ClientValue}}
	p := b.Post("/login", url.Values{"username": {"alex@example.localhost"}, "password": {site.Password}})
	if p.Path != "/choose" {
		t.Fatalf("after the password: %s", p.Path)
	}
	if p := b.Do("GET", "/api/v2/appointments", nil, client); p.Status != 403 || !strings.Contains(p.Body, "setup_incomplete") {
		t.Errorf("before choosing: %d %s", p.Status, p.Body)
	}
	if p := b.Post("/choose", url.Values{"patient": {"p-sam"}}); p.Path != "/terms" {
		t.Fatalf("after choosing: %s", p.Path)
	}
	if p := b.Post("/terms", url.Values{}); p.Status != 422 {
		t.Errorf("not agreeing: %d", p.Status)
	}
	if p := b.Post("/terms", url.Values{"agree": {"yes"}}); p.Path != "/app/" {
		t.Fatalf("after the terms: %s", p.Path)
	}

	if p := b.Get("/api/v2/appointments"); p.Status != 400 || !strings.HasPrefix(p.Body, Prefix) {
		t.Errorf("no client header: %d %q", p.Status, p.Body)
	}
	p = b.Do("GET", "/api/v2/appointments", nil, client)
	if !strings.HasPrefix(p.Body, Prefix+`{"appointments":[{`) || !strings.Contains(p.Body, "Well-child visit") || strings.Contains(p.Body, "knee") {
		t.Errorf("Sam's appointments: %s", p.Body)
	}
	b.JSON("POST", "/api/v2/patient", `{"patient":"p-alex"}`, client)
	if p := b.Do("GET", "/api/v2/results/R-7710", nil, client); !strings.Contains(p.Body, "LDL") {
		t.Errorf("Alex's result: %s", p.Body)
	}
}
