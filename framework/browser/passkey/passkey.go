// Package passkey is a passkey authenticator in software, for an app's
// Passkey hook. It makes a P-256 key for a site, and signs in with it,
// answering the page's navigator.credentials call as an authenticator built
// into a browser would.
//
// It keeps nothing: the app saves each Key where it likes. An app that is not
// code-signed cannot use the Secure Enclave or the Keychain, which both need
// that.
package passkey

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Key is a passkey this package made.
type Key struct {
	ID              []byte
	RPID            string
	UserID          []byte
	UserName        string
	UserDisplayName string
	// Private is the P-256 private key, as PKCS #8.
	Private []byte
}

// ErrNoAlgorithm is returned when a site takes no key this package can make.
var ErrNoAlgorithm = errors.New("the site takes no kind of passkey this app makes, which is ES256 (P-256)")

// es256 is COSE's number for ECDSA with P-256 and SHA-256.
const es256 = -7

// Authenticator data flags.
const (
	userPresent  = 0x01
	userVerified = 0x04
	attested     = 0x40
)

var b64 = base64.RawURLEncoding

// client is what Chrome adds to the page's options: the page's origin, and
// whether it is in a frame from another site.
type client struct {
	Override struct {
		Origin                  string `json:"origin"`
		SameOriginWithAncestors bool   `json:"sameOriginWithAncestors"`
	} `json:"remoteDesktopClientOverride"`
}

// Creation is a page's request to make a passkey.
type Creation struct {
	Origin string
	RPID   string
	RPName string
	User   struct {
		ID                []byte
		Name, DisplayName string
	}
	Exclude [][]byte // passkeys the site has for this account already
	// Algorithms are the COSE algorithms the site takes, best first.
	Algorithms []int
	// UserVerification is the site's: "required", "preferred", or
	// "discouraged".
	UserVerification string
	// Resident is set when the site wants a passkey the authenticator
	// keeps, which it can sign in with before it knows who you are.
	Resident    bool
	challenge   string
	credProps   bool
	crossOrigin bool
}

// ParseCreation reads the page's options, as PublicKeyCredential's
// parseCreationOptionsFromJSON takes them, with Chrome's additions.
func ParseCreation(options json.RawMessage) (*Creation, error) {
	var o struct {
		RP struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"rp"`
		User struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
		Challenge        string `json:"challenge"`
		PubKeyCredParams []struct {
			Alg int `json:"alg"`
		} `json:"pubKeyCredParams"`
		ExcludeCredentials []struct {
			ID string `json:"id"`
		} `json:"excludeCredentials"`
		AuthenticatorSelection struct {
			ResidentKey        string `json:"residentKey"`
			RequireResidentKey bool   `json:"requireResidentKey"`
			UserVerification   string `json:"userVerification"`
		} `json:"authenticatorSelection"`
		Extensions struct {
			client
			CredProps bool `json:"credProps"`
		} `json:"extensions"`
	}
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, fmt.Errorf("the passkey options are not JSON this package can read: %w", err)
	}
	if o.Extensions.Override.Origin == "" || o.RP.ID == "" {
		return nil, errors.New("Chrome did not say which site asked for the passkey")
	}
	c := &Creation{Origin: o.Extensions.Override.Origin, RPID: o.RP.ID, RPName: o.RP.Name, challenge: o.Challenge,
		credProps: o.Extensions.CredProps, crossOrigin: !o.Extensions.Override.SameOriginWithAncestors}
	var err error
	if c.User.ID, err = b64.DecodeString(o.User.ID); err != nil {
		return nil, fmt.Errorf("the account's ID: %w", err)
	}
	c.User.Name, c.User.DisplayName = o.User.Name, o.User.DisplayName
	sel := o.AuthenticatorSelection
	c.UserVerification = sel.UserVerification
	if c.UserVerification == "" {
		c.UserVerification = "preferred"
	}
	c.Resident = sel.ResidentKey == "required" || sel.ResidentKey == "preferred" || sel.RequireResidentKey
	for _, p := range o.PubKeyCredParams {
		c.Algorithms = append(c.Algorithms, p.Alg)
	}
	if len(c.Algorithms) == 0 {
		// The specification's default, when a page lists none.
		c.Algorithms = []int{es256, -257}
	}
	for _, e := range o.ExcludeCredentials {
		if id, err := b64.DecodeString(e.ID); err == nil {
			c.Exclude = append(c.Exclude, id)
		}
	}
	return c, nil
}

// CanMake reports whether the site takes a key this package makes: an
// ES256 (P-256) key. A security key may make others.
func (c *Creation) CanMake() bool {
	return slices.Contains(c.Algorithms, es256)
}

// Create makes a passkey for c, and the page's answer: a
// PublicKeyCredential, as its toJSON makes it. Call it only once you have
// said yes.
func Create(c *Creation) (Key, json.RawMessage, error) {
	if !c.CanMake() {
		return Key{}, nil, ErrNoAlgorithm
	}
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Key{}, nil, err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return Key{}, nil, err
	}
	spki, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return Key{}, nil, err
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return Key{}, nil, err
	}
	k := Key{ID: id, RPID: c.RPID, UserID: c.User.ID, UserName: c.User.Name, UserDisplayName: c.User.DisplayName, Private: pkcs8}

	ecdh, err := priv.PublicKey.ECDH()
	if err != nil {
		return Key{}, nil, err
	}
	point := ecdh.Bytes() // 0x04, then x and y
	cose := cborMap(
		cborInt(1), cborInt(2), // key type: EC2
		cborInt(3), cborInt(es256),
		cborInt(-1), cborInt(1), // curve: P-256
		cborInt(-2), cborBytes(point[1:33]),
		cborInt(-3), cborBytes(point[33:65]),
	)
	var authData bytes.Buffer
	authData.Write(authenticatorData(c.RPID, userPresent|userVerified|attested))
	authData.Write(make([]byte, 16)) // AAGUID: none
	binary.Write(&authData, binary.BigEndian, uint16(len(id)))
	authData.Write(id)
	authData.Write(cose)
	attestation := cborMap(
		cborText("fmt"), cborText("none"),
		cborText("attStmt"), cborMap(),
		cborText("authData"), cborBytes(authData.Bytes()),
	)
	response, err := c.Registration(Made{ID: id, Attestation: attestation, AuthData: authData.Bytes(),
		PublicKey: spki, Algorithm: es256, Transports: []string{"internal"}, Attachment: "platform", Resident: true})
	return k, response, err
}

// ClientData is what the browser says it asked for: the authenticator signs
// its hash. In the proxy's case, the proxy writes it, not Chrome.
func (c *Creation) ClientData() []byte {
	return clientData("webauthn.create", c.challenge, c.Origin, c.crossOrigin)
}

// Made is a passkey an authenticator made, as the page is told of it.
type Made struct {
	ID          []byte
	Attestation []byte // the attestation object, CBOR
	AuthData    []byte
	PublicKey   []byte // SubjectPublicKeyInfo, DER
	Algorithm   int
	Transports  []string
	Attachment  string // "platform", or "cross-platform" for a security key
	Resident    bool   // whether the authenticator keeps it
}

// Registration is the page's answer: a PublicKeyCredential, as its toJSON
// makes it.
func (c *Creation) Registration(m Made) (json.RawMessage, error) {
	extensions := map[string]any{}
	if c.credProps {
		extensions["credProps"] = map[string]bool{"rk": m.Resident}
	}
	return json.Marshal(map[string]any{
		"id":                      b64.EncodeToString(m.ID),
		"rawId":                   b64.EncodeToString(m.ID),
		"type":                    "public-key",
		"authenticatorAttachment": m.Attachment,
		"response": map[string]any{
			"clientDataJSON":     b64.EncodeToString(c.ClientData()),
			"attestationObject":  b64.EncodeToString(m.Attestation),
			"authenticatorData":  b64.EncodeToString(m.AuthData),
			"publicKey":          b64.EncodeToString(m.PublicKey),
			"publicKeyAlgorithm": m.Algorithm,
			"transports":         m.Transports,
		},
		"clientExtensionResults": extensions,
	})
}

// Request is a page's request to sign in with a passkey.
type Request struct {
	Origin string
	RPID   string
	// Allow are the passkeys the site takes; any, if empty.
	Allow [][]byte
	// UserVerification is the site's: "required", "preferred", or
	// "discouraged".
	UserVerification string
	challenge        string
	crossOrigin      bool
}

// ParseRequest reads the page's options, as PublicKeyCredential's
// parseRequestOptionsFromJSON takes them, with Chrome's additions.
func ParseRequest(options json.RawMessage) (*Request, error) {
	var o struct {
		RPID             string `json:"rpId"`
		Challenge        string `json:"challenge"`
		UserVerification string `json:"userVerification"`
		AllowCredentials []struct {
			ID string `json:"id"`
		} `json:"allowCredentials"`
		Extensions client `json:"extensions"`
	}
	if err := json.Unmarshal(options, &o); err != nil {
		return nil, fmt.Errorf("the passkey options are not JSON this package can read: %w", err)
	}
	if o.Extensions.Override.Origin == "" || o.RPID == "" {
		return nil, errors.New("Chrome did not say which site asked for the passkey")
	}
	r := &Request{Origin: o.Extensions.Override.Origin, RPID: o.RPID, challenge: o.Challenge,
		UserVerification: o.UserVerification, crossOrigin: !o.Extensions.Override.SameOriginWithAncestors}
	if r.UserVerification == "" {
		r.UserVerification = "preferred"
	}
	for _, a := range o.AllowCredentials {
		if id, err := b64.DecodeString(a.ID); err == nil {
			r.Allow = append(r.Allow, id)
		}
	}
	return r, nil
}

// Takes reports whether the site takes k.
func (r *Request) Takes(k Key) bool {
	if k.RPID != r.RPID {
		return false
	}
	if len(r.Allow) == 0 {
		return true
	}
	for _, id := range r.Allow {
		if bytes.Equal(id, k.ID) {
			return true
		}
	}
	return false
}

// Get signs in with k: the page's answer, a PublicKeyCredential as its
// toJSON makes it. Call it only once you have said yes.
func Get(r *Request, k Key) (json.RawMessage, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(k.Private)
	if err != nil {
		return nil, fmt.Errorf("the passkey's private key: %w", err)
	}
	priv, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("the passkey's private key is not a P-256 key")
	}
	authData := authenticatorData(r.RPID, userPresent|userVerified)
	cdHash := sha256.Sum256(r.ClientData())
	signed := sha256.Sum256(append(authData, cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, signed[:])
	if err != nil {
		return nil, err
	}
	return r.Assertion(Signed{ID: k.ID, AuthData: authData, Signature: sig, UserHandle: k.UserID, Attachment: "platform"})
}

// ClientData is what the browser says it asked for: the authenticator signs
// its hash.
func (r *Request) ClientData() []byte {
	return clientData("webauthn.get", r.challenge, r.Origin, r.crossOrigin)
}

// Signed is a sign-in an authenticator signed.
type Signed struct {
	ID         []byte
	AuthData   []byte
	Signature  []byte
	UserHandle []byte // may be empty, for a passkey the authenticator does not keep
	Attachment string // "platform", or "cross-platform" for a security key
}

// Assertion is the page's answer: a PublicKeyCredential, as its toJSON
// makes it.
func (r *Request) Assertion(s Signed) (json.RawMessage, error) {
	response := map[string]any{
		"clientDataJSON":    b64.EncodeToString(r.ClientData()),
		"authenticatorData": b64.EncodeToString(s.AuthData),
		"signature":         b64.EncodeToString(s.Signature),
	}
	if len(s.UserHandle) > 0 {
		response["userHandle"] = b64.EncodeToString(s.UserHandle)
	}
	return json.Marshal(map[string]any{
		"id":                      b64.EncodeToString(s.ID),
		"rawId":                   b64.EncodeToString(s.ID),
		"type":                    "public-key",
		"authenticatorAttachment": s.Attachment,
		"response":                response,
		"clientExtensionResults":  map[string]any{},
	})
}

// authenticatorData is the start of it: the site's hash, the flags, and a
// signature count of 0, which tells the site that this authenticator does
// not count.
func authenticatorData(rpID string, flags byte) []byte {
	hash := sha256.Sum256([]byte(rpID))
	return append(append(hash[:], flags), 0, 0, 0, 0)
}

func clientData(typ, challenge, origin string, crossOrigin bool) []byte {
	raw, _ := json.Marshal(struct {
		Type        string `json:"type"`
		Challenge   string `json:"challenge"`
		Origin      string `json:"origin"`
		CrossOrigin bool   `json:"crossOrigin"`
	}{typ, challenge, origin, crossOrigin})
	return raw
}

// The few CBOR encodings an attestation needs, in CTAP2's canonical form:
// the caller lists a map's keys in canonical order.

func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n < 1<<8:
		return []byte{major<<5 | 24, byte(n)}
	case n < 1<<16:
		return binary.BigEndian.AppendUint16([]byte{major<<5 | 25}, uint16(n))
	default:
		return binary.BigEndian.AppendUint32([]byte{major<<5 | 26}, uint32(n))
	}
}

func cborInt(n int) []byte {
	if n < 0 {
		return cborHead(1, uint64(-1-n))
	}
	return cborHead(0, uint64(n))
}

func cborBytes(b []byte) []byte { return append(cborHead(2, uint64(len(b))), b...) }

func cborText(s string) []byte { return append(cborHead(3, uint64(len(s))), s...) }

// cborMap takes keys and values in turn.
func cborMap(items ...[]byte) []byte {
	out := cborHead(5, uint64(len(items)/2))
	for _, item := range items {
		out = append(out, item...)
	}
	return out
}
