package site

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TOTP returns the six-digit code for a base32 secret at t (RFC 6238: SHA-1,
// 30 seconds), as authenticator apps make it.
func TOTP(secret string, t time.Time) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		panic("TOTP secret: " + err.Error())
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(t.Unix()/30))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1_000_000)
}

// TOTPValid accepts the code for now, or for the 30 seconds either side, as
// most sites do for clocks that drift.
func TOTPValid(secret, code string, now time.Time) bool {
	for _, d := range []time.Duration{0, -30 * time.Second, 30 * time.Second} {
		if hmac.Equal([]byte(TOTP(secret, now.Add(d))), []byte(code)) {
			return true
		}
	}
	return false
}

// HMAC returns hex(HMAC-SHA256(key, msg)).
func HMAC(key []byte, msg string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return fmt.Sprintf("%x", mac.Sum(nil))
}

var b64 = base64.RawURLEncoding

// SignJWT makes an HS256 JWT of claims.
func SignJWT(key []byte, claims any) string {
	header := b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		panic(err)
	}
	unsigned := header + "." + b64.EncodeToString(body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(unsigned))
	return unsigned + "." + b64.EncodeToString(mac.Sum(nil))
}

// Claims are the JWT claims every fixture uses.
type Claims struct {
	Subject string `json:"sub"`
	ID      string `json:"jti,omitempty"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
}

var ErrExpired = errors.New("the token has expired")

// ParseJWT checks an HS256 JWT's signature and expiry, and decodes its
// claims into v, which must embed Claims or have an "exp" claim.
func ParseJWT(key []byte, token string, v any) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return errors.New("not a JWT")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := b64.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return errors.New("the token's signature is wrong")
	}
	body, err := b64.DecodeString(parts[1])
	if err != nil {
		return err
	}
	var exp struct {
		Expires int64 `json:"exp"`
	}
	if err := json.Unmarshal(body, &exp); err != nil {
		return err
	}
	if time.Now().Unix() >= exp.Expires {
		return ErrExpired
	}
	return json.Unmarshal(body, v)
}
