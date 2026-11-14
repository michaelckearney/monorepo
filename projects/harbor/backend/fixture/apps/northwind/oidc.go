// Package northwind is what Tasklane and Payday share: signing in with
// Northwind ID, the company's single sign-on, over OIDC.
package northwind

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
)

// IdP is Northwind ID, reached from the apps' servers.
type IdP struct {
	Client *http.Client
}

var issuer = hosts.URL(hosts.NorthwindID)

// AuthorizeURL is where a browser goes to sign in.
func AuthorizeURL(params url.Values) string {
	params.Set("response_type", "code")
	if params.Get("scope") == "" {
		params.Set("scope", "openid profile email")
	}
	return issuer + "/api/oidc/authorization?" + params.Encode()
}

// LogoutURL signs the browser out of Northwind ID, then goes to back.
func LogoutURL(back string) string {
	return issuer + "/logout?rd=" + url.QueryEscape(back)
}

// Challenge is the PKCE S256 challenge for a verifier.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type User struct {
	Username string `json:"preferred_username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

// Exchange trades a code for an access token, then asks who it is for.
// clientSecret is "" for a public client, which sends client_id instead.
func (p IdP) Exchange(ctx context.Context, form url.Values, clientID, clientSecret string) (User, error) {
	form.Set("grant_type", "authorization_code")
	if clientSecret == "" {
		form.Set("client_id", clientID)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, issuer+"/api/oidc/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if clientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := p.do(req, &token); err != nil {
		return User{}, fmt.Errorf("Northwind ID did not give a token: %w", err)
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/api/oidc/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	var u User
	if err := p.do(req, &u); err != nil {
		return User{}, fmt.Errorf("Northwind ID did not say who signed in: %w", err)
	}
	return u, nil
}

// SignedIn reports whether the browser behind r is still signed in to
// Northwind ID. The apps share Northwind ID's parent domain, so the browser
// sends them its session cookie; this asks Northwind ID about it. It is how
// signing out of one app signs you out of the others (single logout).
func (p IdP) SignedIn(ctx context.Context, r *http.Request) (bool, error) {
	c, err := r.Cookie("northwind_session")
	if err != nil {
		return false, nil
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/api/state", nil)
	req.AddCookie(c)
	var state struct {
		Data struct {
			Username string `json:"username"`
			Level    int    `json:"authentication_level"`
		} `json:"data"`
	}
	if err := p.do(req, &state); err != nil {
		return false, err
	}
	return state.Data.Level >= 1, nil
}

func (p IdP) do(req *http.Request, v any) error {
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %d %s", req.Method, req.URL.Path, resp.StatusCode, body)
	}
	return json.Unmarshal(body, v)
}
