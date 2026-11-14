package chat

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

func signIn(t *testing.T, b *fake.Browser) http.Header {
	p := b.JSON("POST", "/api/auth/login", `{"email":"alex@example.localhost","password":"`+site.Password+`"}`, nil)
	var out struct{ Token string }
	json.Unmarshal([]byte(p.Body), &out)
	if out.Token == "" {
		t.Fatalf("login: %s", p.Body)
	}
	return http.Header{"Authorization": {"Bearer " + out.Token}}
}

func TestSearchIsRateLimitedAndSignOutEverywhere(t *testing.T) {
	app := New(fake.Store(t))
	b := fake.New(t, hosts.Chat, app)
	auth := signIn(t, b)
	other := signIn(t, b)
	if p := b.Do("GET", "/api/mentions?unread=1", nil, auth); !strings.Contains(p.Body, "@alex") {
		t.Errorf("mentions: %s", p.Body)
	}
	for i := range searchLimit {
		if p := b.Do("GET", "/api/search?q=deploy", nil, auth); p.Status != 200 {
			t.Fatalf("search %d: %d", i, p.Status)
		}
	}
	p := b.Do("GET", "/api/search?q=deploy", nil, auth)
	if p.Status != 429 || p.Header.Get("Retry-After") == "" {
		t.Errorf("search %d: %d %v", searchLimit+1, p.Status, p.Header)
	}
	b.Do("POST", "/api/auth/logout-everywhere", nil, other)
	if p := b.Do("GET", "/api/me", nil, auth); p.Status != 401 || !strings.Contains(p.Body, "session_revoked") {
		t.Errorf("after signing out everywhere: %d %s", p.Status, p.Body)
	}
}
