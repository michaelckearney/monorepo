package passkey

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const origin = "https://shop.localhost:8443"

type user struct{ creds []webauthn.Credential }

func (u *user) WebAuthnID() []byte                         { return []byte("user-1") }
func (u *user) WebAuthnName() string                       { return "alex@example.localhost" }
func (u *user) WebAuthnDisplayName() string                { return "Alex" }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// withOrigin adds the page's origin to the options, as Chrome does.
func withOrigin(t *testing.T, publicKey any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	ext, _ := m["extensions"].(map[string]any)
	if ext == nil {
		ext = map[string]any{}
	}
	ext["remoteDesktopClientOverride"] = map[string]any{"origin": origin, "sameOriginWithAncestors": true}
	m["extensions"] = ext
	raw, _ = json.Marshal(m)
	return raw
}

// A site that checks passkeys as the specification says takes this package's,
// to sign up and to sign in.
func TestASiteTakesThesePasskeys(t *testing.T) {
	site, err := webauthn.New(&webauthn.Config{RPID: "shop.localhost", RPDisplayName: "Shop", RPOrigins: []string{origin}})
	if err != nil {
		t.Fatal(err)
	}
	u := &user{}

	options, session, err := site.BeginRegistration(u,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{UserVerification: protocol.VerificationRequired}),
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCreation(withOrigin(t, options.Response))
	if err != nil {
		t.Fatal(err)
	}
	if c.Origin != origin || c.RPID != "shop.localhost" || c.User.Name != "alex@example.localhost" || string(c.User.ID) != "user-1" {
		t.Errorf("ParseCreation = %+v", c)
	}
	key, response, err := Create(c)
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
	if !bytes.Equal(cred.ID, key.ID) {
		t.Errorf("the site saved passkey %x; Create made %x", cred.ID, key.ID)
	}
	u.creds = append(u.creds, *cred)

	// Signing in, as a site with a "Sign in with a passkey" button does: it
	// does not know who you are, so it lists no passkeys.
	login, session, err := site.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseRequest(withOrigin(t, login.Response))
	if err != nil {
		t.Fatal(err)
	}
	if !r.Takes(key) {
		t.Fatal("the request does not take the passkey made for its site")
	}
	if other := (Key{RPID: "evil.localhost"}); r.Takes(other) {
		t.Error("the request takes another site's passkey")
	}
	response, err = Get(r, key)
	if err != nil {
		t.Fatal(err)
	}
	assertion, err := protocol.ParseCredentialRequestResponseBytes(response)
	if err != nil {
		t.Fatalf("the site cannot read the sign-in: %v\n%s", err, response)
	}
	_, err = site.ValidateDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		if string(userHandle) != "user-1" {
			return nil, errors.New("unknown user")
		}
		return u, nil
	}, *session, assertion)
	if err != nil {
		t.Fatalf("the site refused the sign-in: %v", err)
	}
}

func TestASiteThatTakesNoP256Key(t *testing.T) {
	c, err := ParseCreation(json.RawMessage(`{"rp":{"id":"a.localhost"},"user":{"id":"AA"},"challenge":"AA",
		"pubKeyCredParams":[{"type":"public-key","alg":-257}],
		"extensions":{"remoteDesktopClientOverride":{"origin":"https://a.localhost"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.CanMake() {
		t.Error("CanMake = true for a site that takes only RSA")
	}
	if _, _, err := Create(c); !errors.Is(err, ErrNoAlgorithm) {
		t.Errorf("err = %v, want ErrNoAlgorithm", err)
	}
}
