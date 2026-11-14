package securitykey

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/fxamacker/cbor/v2"

	"github.com/michaelckearney/monorepo/framework/browser/passkey"
)

// Asker asks you, while the app uses a security key.
type Asker interface {
	// PIN asks for the key's PIN. retries is how many more wrong ones the
	// key takes; problem is why it is asked again, if it is.
	PIN(ctx context.Context, retries int, problem string) (string, error)
	// Touch tells you to touch the key, which waits for it.
	Touch()
}

type credential struct {
	Type string `cbor:"type"`
	ID   []byte `cbor:"id"`
}

type makeCredential struct {
	ClientDataHash    []byte           `cbor:"1,keyasint"`
	RP                map[string]any   `cbor:"2,keyasint"`
	User              map[string]any   `cbor:"3,keyasint"`
	Params            []map[string]any `cbor:"4,keyasint"`
	Exclude           []credential     `cbor:"5,keyasint,omitempty"`
	Options           map[string]bool  `cbor:"7,keyasint,omitempty"`
	PinUvAuthParam    []byte           `cbor:"8,keyasint,omitempty"`
	PinUvAuthProtocol uint64           `cbor:"9,keyasint,omitempty"`
}

type madeCredential struct {
	Fmt      string          `cbor:"1,keyasint"`
	AuthData []byte          `cbor:"2,keyasint"`
	AttStmt  cbor.RawMessage `cbor:"3,keyasint"`
}

type getAssertion struct {
	RPID              string          `cbor:"1,keyasint"`
	ClientDataHash    []byte          `cbor:"2,keyasint"`
	Allow             []credential    `cbor:"3,keyasint,omitempty"`
	Options           map[string]bool `cbor:"5,keyasint,omitempty"`
	PinUvAuthParam    []byte          `cbor:"6,keyasint,omitempty"`
	PinUvAuthProtocol uint64          `cbor:"7,keyasint,omitempty"`
}

type assertion struct {
	Credential credential `cbor:"1,keyasint"`
	AuthData   []byte     `cbor:"2,keyasint"`
	Signature  []byte     `cbor:"3,keyasint"`
	User       struct {
		ID []byte `cbor:"id"`
	} `cbor:"4,keyasint"`
}

// start opens a channel, and reads what the key can do. A key that knows
// only U2F, CTAP2's older form, cannot make passkeys.
func start(ctx context.Context, dev Device) (*channel, info, error) {
	c, err := openChannel(ctx, dev)
	if err != nil {
		return nil, info{}, err
	}
	i, err := c.info(ctx)
	if err != nil {
		return nil, info{}, fmt.Errorf("this security key does not make passkeys; a FIDO2 key is needed: %w", err)
	}
	return c, i, nil
}

// verify gets your PIN's go-ahead for the request, signed over hash: the
// pinUvAuthParam and its protocol, or none when the key has no PIN and the
// site does not insist.
func verify(ctx context.Context, c *channel, i info, ask Asker, uv, rpID string, permissions uint64, hash []byte) ([]byte, uint64, error) {
	if !i.hasPIN() {
		if uv == "required" {
			return nil, 0, StatusPINNotSet
		}
		return nil, 0, nil
	}
	if uv == "discouraged" {
		return nil, 0, nil
	}
	problem := ""
	for {
		retries, err := c.retries(ctx)
		if err != nil {
			return nil, 0, err
		}
		pin, err := ask.PIN(ctx, retries, problem)
		if err != nil {
			return nil, 0, err
		}
		t, err := c.pinToken(ctx, i, pin, rpID, permissions)
		if errors.Is(err, StatusPINInvalid) {
			problem = err.Error()
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		return authenticate(t.version, t.key, hash), t.version, nil
	}
}

// touching tells you to touch the key when it waits for it.
func touching(ask Asker) func(byte) {
	told := false
	return func(status byte) {
		if status == statusNeedsTouch && !told {
			told = true
			ask.Touch()
		}
	}
}

// Create makes a passkey on the key, for the page's request, and returns
// the page's answer.
func Create(ctx context.Context, dev Device, cr *passkey.Creation, ask Asker) (json.RawMessage, error) {
	c, i, err := start(ctx, dev)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(cr.ClientData())
	param, protocol, err := verify(ctx, c, i, ask, cr.UserVerification, cr.RPID, permMakeCredential, hash[:])
	if err != nil {
		return nil, err
	}
	rp := map[string]any{"id": cr.RPID, "name": cr.RPName}
	if cr.RPName == "" {
		rp["name"] = cr.RPID
	}
	req := makeCredential{
		ClientDataHash: hash[:],
		RP:             rp,
		User:           map[string]any{"id": cr.User.ID, "name": cr.User.Name, "displayName": cr.User.DisplayName},
		PinUvAuthParam: param, PinUvAuthProtocol: protocol,
	}
	for _, alg := range cr.Algorithms {
		req.Params = append(req.Params, map[string]any{"alg": alg, "type": "public-key"})
	}
	for _, id := range cr.Exclude {
		req.Exclude = append(req.Exclude, credential{Type: "public-key", ID: id})
	}
	if cr.Resident {
		req.Options = map[string]bool{"rk": true}
	}
	var made madeCredential
	if err := c.do(ctx, ctapMakeCredential, req, &made, touching(ask)); err != nil {
		return nil, err
	}
	id, cose, err := attested(made.AuthData)
	if err != nil {
		return nil, err
	}
	spki, alg, err := publicKey(cose)
	if err != nil {
		return nil, err
	}
	attestation, err := encode.Marshal(map[string]any{"fmt": made.Fmt, "attStmt": made.AttStmt, "authData": made.AuthData})
	if err != nil {
		return nil, err
	}
	return cr.Registration(passkey.Made{ID: id, Attestation: attestation, AuthData: made.AuthData, PublicKey: spki,
		Algorithm: alg, Transports: []string{"usb"}, Attachment: "cross-platform", Resident: cr.Resident})
}

// Get signs in with a passkey on the key, for the page's request, and
// returns the page's answer. If the key has several for the site, it uses
// the one it lists first.
func Get(ctx context.Context, dev Device, r *passkey.Request, ask Asker) (json.RawMessage, error) {
	c, i, err := start(ctx, dev)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(r.ClientData())
	param, protocol, err := verify(ctx, c, i, ask, r.UserVerification, r.RPID, permGetAssertion, hash[:])
	if err != nil {
		return nil, err
	}
	req := getAssertion{RPID: r.RPID, ClientDataHash: hash[:], Options: map[string]bool{"up": true},
		PinUvAuthParam: param, PinUvAuthProtocol: protocol}
	for _, id := range r.Allow {
		req.Allow = append(req.Allow, credential{Type: "public-key", ID: id})
	}
	var a assertion
	if err := c.do(ctx, ctapGetAssertion, req, &a, touching(ask)); err != nil {
		return nil, err
	}
	id := a.Credential.ID
	if len(id) == 0 && len(r.Allow) == 1 {
		// A key may leave out the one passkey the site listed.
		id = r.Allow[0]
	}
	return r.Assertion(passkey.Signed{ID: id, AuthData: a.AuthData, Signature: a.Signature,
		UserHandle: a.User.ID, Attachment: "cross-platform"})
}

// attested reads the new passkey's ID and public key out of the
// authenticator data.
func attested(authData []byte) (id []byte, cose []byte, err error) {
	const start = 32 + 1 + 4 + 16 // site hash, flags, count, AAGUID
	if len(authData) < start+2 || authData[32]&0x40 == 0 {
		return nil, nil, errors.New("the security key did not say which passkey it made")
	}
	n := int(binary.BigEndian.Uint16(authData[start:]))
	if len(authData) < start+2+n {
		return nil, nil, errors.New("the security key's passkey is cut short")
	}
	id = authData[start+2 : start+2+n]
	var key cbor.RawMessage
	if _, err := cbor.UnmarshalFirst(authData[start+2+n:], &key); err != nil {
		return nil, nil, fmt.Errorf("the security key's public key: %w", err)
	}
	return id, key, nil
}

// publicKey is a COSE public key as SubjectPublicKeyInfo, which the page is
// told, and its algorithm. COSE numbers a key's fields by its type: -1 is
// an EC2 key's curve, but an RSA key's modulus.
func publicKey(cose []byte) ([]byte, int, error) {
	var k map[int]cbor.RawMessage
	if err := cbor.Unmarshal(cose, &k); err != nil {
		return nil, 0, fmt.Errorf("the security key's public key: %w", err)
	}
	num := func(label int) int {
		var n int
		_ = cbor.Unmarshal(k[label], &n)
		return n
	}
	raw := func(label int) []byte {
		var b []byte
		_ = cbor.Unmarshal(k[label], &b)
		return b
	}
	var pub any
	switch kty, alg := num(1), num(3); {
	case kty == 2 && num(-1) == 1: // EC2, P-256
		pub = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(raw(-2)), Y: new(big.Int).SetBytes(raw(-3))}
	case kty == 1 && num(-1) == 6: // OKP, Ed25519
		pub = ed25519.PublicKey(raw(-2))
	case kty == 3: // RSA
		pub = &rsa.PublicKey{N: new(big.Int).SetBytes(raw(-1)), E: int(new(big.Int).SetBytes(raw(-2)).Int64())}
	default:
		return nil, alg, fmt.Errorf("the security key made a kind of key this package does not know (COSE key type %d)", kty)
	}
	spki, err := x509.MarshalPKIXPublicKey(pub)
	return spki, num(3), err
}
