package securitykey_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/michaelckearney/monorepo/framework/browser/passkey"
	"github.com/michaelckearney/monorepo/framework/browser/securitykey"
	"github.com/michaelckearney/monorepo/framework/browser/securitykey/softkey"
)

const origin = "https://shop.localhost:8443"

type user struct{ creds []webauthn.Credential }

func (u *user) WebAuthnID() []byte                         { return []byte("user-1") }
func (u *user) WebAuthnName() string                       { return "alex@example.localhost" }
func (u *user) WebAuthnDisplayName() string                { return "Alex" }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// withOrigin adds the page's origin to the options, as Chrome does.
func withOrigin(t *testing.T, options any) json.RawMessage {
	t.Helper()
	raw, _ := json.Marshal(options)
	var m map[string]any
	json.Unmarshal(raw, &m)
	ext, _ := m["extensions"].(map[string]any)
	if ext == nil {
		ext = map[string]any{}
	}
	ext["remoteDesktopClientOverride"] = map[string]any{"origin": origin, "sameOriginWithAncestors": true}
	m["extensions"] = ext
	raw, _ = json.Marshal(m)
	return raw
}

// asker answers with the PINs given, in turn, and counts the touches it is
// asked for.
type asker struct {
	pins     []string
	problems []string
	touches  int
}

func (a *asker) PIN(ctx context.Context, retries int, problem string) (string, error) {
	a.problems = append(a.problems, problem)
	if len(a.pins) == 0 {
		return "", errors.New("no more PINs")
	}
	pin := a.pins[0]
	a.pins = a.pins[1:]
	return pin, nil
}

func (a *asker) Touch() { a.touches++ }

func TestASiteTakesPasskeysOnASecurityKey(t *testing.T) {
	for _, pin := range []string{"", "1234"} {
		name := "with no PIN"
		if pin != "" {
			name = "with a PIN, after a wrong one"
		}
		t.Run(name, func(t *testing.T) {
			site, err := webauthn.New(&webauthn.Config{RPID: "shop.localhost", RPDisplayName: "Shop", RPOrigins: []string{origin}})
			if err != nil {
				t.Fatal(err)
			}
			u := &user{}
			key := softkey.New(pin)
			ctx := context.Background()
			uv := protocol.VerificationDiscouraged
			ask := &asker{}
			if pin != "" {
				uv = protocol.VerificationRequired
				ask.pins = []string{"0000", pin, "0000", pin}
			}

			options, session, err := site.BeginRegistration(u,
				webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
				webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{UserVerification: uv}))
			if err != nil {
				t.Fatal(err)
			}
			cr, err := passkey.ParseCreation(withOrigin(t, options.Response))
			if err != nil {
				t.Fatal(err)
			}
			response, err := securitykey.Create(ctx, key, cr, ask)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := protocol.ParseCredentialCreationResponseBytes(response)
			if err != nil {
				t.Fatalf("the site cannot read the new passkey: %v\n%s", err, response)
			}
			cred, err := site.CreateCredential(u, *session, parsed)
			if err != nil {
				t.Fatalf("the site refused the new passkey: %v", err)
			}
			u.creds = append(u.creds, *cred)

			login, session, err := site.BeginDiscoverableLogin(webauthn.WithUserVerification(uv))
			if err != nil {
				t.Fatal(err)
			}
			r, err := passkey.ParseRequest(withOrigin(t, login.Response))
			if err != nil {
				t.Fatal(err)
			}
			response, err = securitykey.Get(ctx, key, r, ask)
			if err != nil {
				t.Fatal(err)
			}
			assertion, err := protocol.ParseCredentialRequestResponseBytes(response)
			if err != nil {
				t.Fatalf("the site cannot read the sign-in: %v\n%s", err, response)
			}
			_, err = site.ValidateDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
				return u, nil
			}, *session, assertion)
			if err != nil {
				t.Fatalf("the site refused the sign-in: %v", err)
			}

			if ask.touches != 2 || key.Touched != 2 {
				t.Errorf("asked for %d touches; the key waited for %d", ask.touches, key.Touched)
			}
			if pin != "" && (len(ask.problems) != 4 || ask.problems[1] != "that PIN is not right") {
				t.Errorf("the PIN prompts said %q", ask.problems)
			}
		})
	}
}

func TestAKeyWithNoPINForASiteThatNeedsOne(t *testing.T) {
	cr, _ := passkey.ParseCreation(json.RawMessage(`{"rp":{"id":"shop.localhost"},"user":{"id":"AA","name":"a"},"challenge":"AA",
		"pubKeyCredParams":[{"type":"public-key","alg":-7}],"authenticatorSelection":{"userVerification":"required"},
		"extensions":{"remoteDesktopClientOverride":{"origin":"https://shop.localhost:8443"}}}`))
	_, err := securitykey.Create(context.Background(), softkey.New(""), cr, &asker{})
	if !errors.Is(err, securitykey.StatusPINNotSet) {
		t.Errorf("err = %v", err)
	}
}
