package securitykey

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// CTAP2's commands.
const (
	ctapMakeCredential = 0x01
	ctapGetAssertion   = 0x02
	ctapGetInfo        = 0x04
	ctapClientPIN      = 0x06
)

// Status is a CTAP2 error the key answered with.
type Status byte

// The statuses a key gives that the app tells you about.
const (
	StatusCredentialExcluded Status = 0x19
	StatusOperationDenied    Status = 0x27
	StatusKeepaliveCancel    Status = 0x2d
	StatusNoCredentials      Status = 0x2e
	StatusActionTimeout      Status = 0x2f
	StatusPINInvalid         Status = 0x31
	StatusPINBlocked         Status = 0x32
	StatusPINAuthBlocked     Status = 0x34
	StatusPINNotSet          Status = 0x35
	StatusPINRequired        Status = 0x36
	StatusPINPolicyViolation Status = 0x37
	StatusUserActionTimeout  Status = 0x3a
)

func (s Status) Error() string {
	switch s {
	case StatusCredentialExcluded:
		return "this security key already has a passkey for this account"
	case StatusOperationDenied, StatusKeepaliveCancel:
		return "the security key was not used"
	case StatusNoCredentials:
		return "this security key has no passkey for this site"
	case StatusActionTimeout, StatusUserActionTimeout:
		return "the security key was not touched in time"
	case StatusPINInvalid:
		return "that PIN is not right"
	case StatusPINBlocked:
		return "the security key's PIN is blocked: reset the key to use it again"
	case StatusPINAuthBlocked:
		return "too many wrong PINs: unplug the security key, and plug it in again"
	case StatusPINNotSet, StatusPINRequired:
		return "this site needs a PIN on your security key; set one first"
	case StatusPINPolicyViolation:
		return "the PIN does not meet the security key's rules"
	}
	return fmt.Sprintf("the security key refused (CTAP2 error %#x)", byte(s))
}

// encode writes CBOR as CTAP2 wants it: map keys in canonical order.
var encode = func() cbor.EncMode {
	m, err := cbor.CTAP2EncOptions().EncMode()
	if err != nil {
		panic(err)
	}
	return m
}()

// do sends a CTAP2 command and reads its response into out, if any.
func (c *channel) do(ctx context.Context, command byte, params, out any, keepalive func(byte)) error {
	payload := []byte{command}
	if params != nil {
		raw, err := encode.Marshal(params)
		if err != nil {
			return err
		}
		payload = append(payload, raw...)
	}
	if err := c.send(cmdCBOR, payload); err != nil {
		return err
	}
	cmd, data, err := c.receive(ctx, keepalive)
	if err != nil {
		return err
	}
	if cmd != cmdCBOR || len(data) == 0 {
		return errors.New("the security key gave an answer this package cannot read")
	}
	if data[0] != 0 {
		return Status(data[0])
	}
	if out == nil || len(data) == 1 {
		return nil
	}
	if err := cbor.Unmarshal(data[1:], out); err != nil {
		return fmt.Errorf("the security key's answer: %w", err)
	}
	return nil
}

// info is what a key says about itself.
type info struct {
	Versions     []string        `cbor:"1,keyasint"`
	Options      map[string]bool `cbor:"4,keyasint"`
	PinProtocols []uint64        `cbor:"6,keyasint"`
}

func (c *channel) info(ctx context.Context) (info, error) {
	var i info
	err := c.do(ctx, ctapGetInfo, nil, &i, nil)
	return i, err
}

// hasPIN reports whether the key has a PIN set.
func (i info) hasPIN() bool { return i.Options["clientPin"] }

// The PIN protocol: the key and this package agree a secret with ECDH, this
// package sends a hash of your PIN encrypted with it, and gets a token back, which
// it signs each request with. Version 2 is CTAP 2.1's; version 1 is what
// older keys know.

// keyAgreement is a P-256 public key, as COSE writes it.
type keyAgreement struct {
	Kty int    `cbor:"1,keyasint"`
	Alg int    `cbor:"3,keyasint"`
	Crv int    `cbor:"-1,keyasint"`
	X   []byte `cbor:"-2,keyasint"`
	Y   []byte `cbor:"-3,keyasint"`
}

type clientPIN struct {
	Protocol     uint64        `cbor:"1,keyasint"`
	SubCommand   uint64        `cbor:"2,keyasint"`
	KeyAgreement *keyAgreement `cbor:"3,keyasint,omitempty"`
	PinHashEnc   []byte        `cbor:"6,keyasint,omitempty"`
	Permissions  uint64        `cbor:"9,keyasint,omitempty"`
	RPID         string        `cbor:"10,keyasint,omitempty"`
}

type clientPINResponse struct {
	KeyAgreement *keyAgreement `cbor:"1,keyasint"`
	Token        []byte        `cbor:"2,keyasint"`
	Retries      int           `cbor:"3,keyasint"`
}

// The permissions a token is for.
const (
	permMakeCredential = 0x01
	permGetAssertion   = 0x02
)

// pinProtocol is one version of the protocol, with the secret agreed.
type pinProtocol struct {
	version uint64
	secret  []byte // version 1: the AES and HMAC key; 2: HMAC key, then AES key
}

func (p pinProtocol) aesKey() []byte {
	if p.version == 2 {
		return p.secret[32:]
	}
	return p.secret
}

func (p pinProtocol) encrypt(plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(p.aesKey())
	if err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	if p.version == 2 {
		if _, err := rand.Read(iv); err != nil {
			return nil, err
		}
	}
	out := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plain)
	if p.version == 2 {
		return append(iv, out...), nil
	}
	return out, nil
}

func (p pinProtocol) decrypt(sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(p.aesKey())
	if err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	if p.version == 2 {
		if len(sealed) < aes.BlockSize {
			return nil, errors.New("the security key's token is too short")
		}
		iv, sealed = sealed[:aes.BlockSize], sealed[aes.BlockSize:]
	}
	if len(sealed)%aes.BlockSize != 0 {
		return nil, errors.New("the security key's token is not whole blocks")
	}
	out := make([]byte, len(sealed))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, sealed)
	return out, nil
}

// authenticate signs msg with key, as the protocol's version does.
func authenticate(version uint64, key, msg []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(msg)
	sum := mac.Sum(nil)
	if version == 1 {
		return sum[:16]
	}
	return sum
}

// agree agrees a secret with the key, and returns this side's public key to
// send with what it encrypts.
func (c *channel) agree(ctx context.Context, version uint64) (pinProtocol, *keyAgreement, error) {
	var resp clientPINResponse
	if err := c.do(ctx, ctapClientPIN, clientPIN{Protocol: version, SubCommand: 2}, &resp, nil); err != nil {
		return pinProtocol{}, nil, err
	}
	if resp.KeyAgreement == nil {
		return pinProtocol{}, nil, errors.New("the security key sent no key to agree a secret with")
	}
	theirs, err := ecdh.P256().NewPublicKey(append(append([]byte{4}, resp.KeyAgreement.X...), resp.KeyAgreement.Y...))
	if err != nil {
		return pinProtocol{}, nil, fmt.Errorf("the security key's key: %w", err)
	}
	ours, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return pinProtocol{}, nil, err
	}
	z, err := ours.ECDH(theirs)
	if err != nil {
		return pinProtocol{}, nil, err
	}
	p := pinProtocol{version: version}
	if version == 2 {
		salt := make([]byte, 32)
		hmacKey, err := hkdf.Key(sha256.New, z, salt, "CTAP2 HMAC key", 32)
		if err != nil {
			return pinProtocol{}, nil, err
		}
		aesKey, err := hkdf.Key(sha256.New, z, salt, "CTAP2 AES key", 32)
		if err != nil {
			return pinProtocol{}, nil, err
		}
		p.secret = append(hmacKey, aesKey...)
	} else {
		sum := sha256.Sum256(z)
		p.secret = sum[:]
	}
	point := ours.PublicKey().Bytes()
	return p, &keyAgreement{Kty: 2, Alg: -25, Crv: 1, X: point[1:33], Y: point[33:65]}, nil
}

// token is a PIN token, for one request, and the protocol to sign with.
type token struct {
	version uint64
	key     []byte
}

// pinToken gets a token for a request to rpID with permissions, with your
// PIN.
func (c *channel) pinToken(ctx context.Context, i info, pin, rpID string, permissions uint64) (token, error) {
	version := uint64(1)
	for _, v := range i.PinProtocols {
		version = max(version, min(v, 2))
	}
	p, ours, err := c.agree(ctx, version)
	if err != nil {
		return token{}, err
	}
	hash := sha256.Sum256([]byte(pin))
	pinHashEnc, err := p.encrypt(hash[:16])
	if err != nil {
		return token{}, err
	}
	req := clientPIN{Protocol: version, SubCommand: 5, KeyAgreement: ours, PinHashEnc: pinHashEnc}
	// A CTAP 2.1 key gives a token for one site and one use.
	if i.Options["pinUvAuthToken"] {
		req.SubCommand, req.Permissions, req.RPID = 9, permissions, rpID
	}
	var resp clientPINResponse
	if err := c.do(ctx, ctapClientPIN, req, &resp, nil); err != nil {
		return token{}, err
	}
	key, err := p.decrypt(resp.Token)
	if err != nil {
		return token{}, err
	}
	return token{version: version, key: key}, nil
}

// retries is how many more wrong PINs the key takes before it blocks it.
func (c *channel) retries(ctx context.Context) (int, error) {
	var resp clientPINResponse
	err := c.do(ctx, ctapClientPIN, clientPIN{Protocol: 1, SubCommand: 1}, &resp, nil)
	return resp.Retries, err
}
