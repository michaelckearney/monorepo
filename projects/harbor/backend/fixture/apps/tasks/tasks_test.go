package tasks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/apps/northwind"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// setup returns Tasklane, a browser, and tokens, with Northwind ID faked:
// a code signs alex in, and the browser stays signed in while *idp is true.
func setup(t *testing.T) (*tasks, *fake.Browser, tokens, *bool) {
	signedIn := true
	tk := newTasks(fake.Store(t), http.DefaultClient)
	tk.signedIn = func(context.Context, *http.Request) (bool, error) { return signedIn, nil }
	tk.exchangeCode = func(_ context.Context, form url.Values, client, _ string) (northwind.User, error) {
		if client != "tasklane" || form.Get("code") != "good-code" || form.Get("code_verifier") == "" {
			t.Errorf("exchange: %s %v", client, form)
		}
		return northwind.User{Username: "alex"}, nil
	}
	b := fake.New(t, hosts.Tasks, tk.handler())
	p := b.JSON("POST", "/api/auth/exchange", `{"code":"good-code","codeVerifier":"v"}`, nil)
	var tok tokens
	if err := json.Unmarshal([]byte(p.Body), &tok); err != nil || tok.AccessToken == "" {
		t.Fatalf("exchange: %d %s", p.Status, p.Body)
	}
	return tk, b, tok, &signedIn
}

func gql(b *fake.Browser, access, name string, vars map[string]any) fake.Page {
	body, _ := json.Marshal(map[string]any{"operationName": name, "variables": vars,
		"extensions": map[string]any{"persistedQuery": map[string]any{"version": 1, "sha256Hash": hash(operations[name])}}})
	return b.JSON("POST", "/graphql", string(body), http.Header{"Authorization": {"Bearer " + access}})
}

func TestPersistedQueriesAndPages(t *testing.T) {
	_, b, tok, _ := setup(t)
	if app := b.Get("/app.js").Body; strings.Contains(app, "__OPERATIONS__") || !strings.Contains(app, hash(operations["Issues"])) {
		t.Error("app.js does not have the hashes")
	}
	if p := gql(b, tok.AccessToken, "Viewer", nil); !strings.Contains(p.Body, `"username":"alex"`) {
		t.Errorf("viewer: %s", p.Body)
	}
	p := b.JSON("POST", "/graphql", `{"query":"{ viewer { name } }"}`, http.Header{"Authorization": {"Bearer " + tok.AccessToken}})
	if !strings.Contains(p.Body, "PERSISTED_QUERY_ONLY") {
		t.Errorf("query text: %s", p.Body)
	}

	type issuesPage struct {
		Data struct {
			Issues struct {
				TotalCount int
				PageInfo   struct {
					HasNextPage bool
					EndCursor   string
				}
				Edges []struct{ Node struct{ Key string } }
			}
		}
	}
	var page, second issuesPage
	json.Unmarshal([]byte(gql(b, tok.AccessToken, "Issues", map[string]any{"first": 5}).Body), &page)
	first := page.Data.Issues
	if len(first.Edges) != 5 || !first.PageInfo.HasNextPage || first.TotalCount != 64 {
		t.Fatalf("first page: %+v", first)
	}
	json.Unmarshal([]byte(gql(b, tok.AccessToken, "Issues", map[string]any{"first": 5, "after": first.PageInfo.EndCursor}).Body), &second)
	if second.Data.Issues.Edges[0].Node.Key == first.Edges[0].Node.Key {
		t.Error("the second page is the first")
	}

	key := first.Edges[4].Node.Key
	p = gql(b, tok.AccessToken, "UpdateIssue", map[string]any{"key": key, "input": map[string]any{"state": "DONE"}})
	if !strings.Contains(p.Body, `"state":"DONE"`) {
		t.Errorf("update: %s", p.Body)
	}
}

func TestAnExpiredTokenAndARefreshUsedTwice(t *testing.T) {
	tk, b, tok, _ := setup(t)
	old := site.SignJWT(tk.key, site.Claims{Subject: "alex", Expires: time.Now().Add(-time.Second).Unix()})
	if p := gql(b, old, "Viewer", nil); p.Status != 401 || !strings.Contains(p.Body, "TOKEN_EXPIRED") {
		t.Errorf("expired: %d %s", p.Status, p.Body)
	}
	body := `{"refreshToken":"` + tok.RefreshToken + `"}`
	p := b.JSON("POST", "/api/auth/refresh", body, nil)
	var next tokens
	json.Unmarshal([]byte(p.Body), &next)
	if next.RefreshToken == "" || next.RefreshToken == tok.RefreshToken {
		t.Fatalf("refresh: %s", p.Body)
	}
	// The old refresh token again: it leaked, so the new one ends too.
	if p := b.JSON("POST", "/api/auth/refresh", body, nil); !strings.Contains(p.Body, "refresh_token_reused") {
		t.Errorf("reuse: %s", p.Body)
	}
	if p := b.JSON("POST", "/api/auth/refresh", `{"refreshToken":"`+next.RefreshToken+`"}`, nil); p.Status != 401 {
		t.Errorf("the family lives on: %s", p.Body)
	}
}

func TestSigningOutOfNorthwindIDEndsTheRefresh(t *testing.T) {
	_, b, tok, signedIn := setup(t)
	*signedIn = false
	if p := b.JSON("POST", "/api/auth/refresh", `{"refreshToken":"`+tok.RefreshToken+`"}`, nil); !strings.Contains(p.Body, "signed_out") {
		t.Errorf("%s", p.Body)
	}
}
