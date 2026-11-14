package home

import (
	"net/http"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

func TestTheAPIIsBoundToTheBrowser(t *testing.T) {
	b := fake.New(t, hosts.Home, New(fake.Store(t)))
	b.Header.Set("User-Agent", "Chromium A")
	b.Get("/")
	if p := b.Get("/api/devices"); p.Status != 200 || !strings.Contains(p.Body, "alex-laptop") {
		t.Fatalf("devices: %d %s", p.Status, p.Body)
	}
	b.Header.Set("User-Agent", "Chromium B")
	if p := b.Get("/api/devices"); p.Status != 401 || !strings.Contains(p.Body, "session_bound_to_another_browser") {
		t.Errorf("another browser: %d %s", p.Status, p.Body)
	}
}

func TestTheRouterNeedsBasicAndItsCookie(t *testing.T) {
	b := fake.New(t, hosts.HomeRouter, Router(fake.Store(t)))
	if p := b.Get("/admin/"); p.Status != 401 || p.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("no password: %d", p.Status)
	}
	basic := http.Header{}
	r, _ := http.NewRequest("GET", "/", nil)
	r.SetBasicAuth("alex@example.localhost", site.Password)
	basic.Set("Authorization", r.Header.Get("Authorization"))
	if p := b.Do("GET", "/admin/api/clients", nil, basic); p.Status != 403 {
		t.Errorf("the API before the page: %d", p.Status)
	}
	b.Do("GET", "/admin/", nil, basic)
	if p := b.Do("GET", "/admin/api/clients", nil, basic); p.Status != 200 || !strings.Contains(p.Body, "thermostat") {
		t.Errorf("clients: %d %s", p.Status, p.Body)
	}
}
