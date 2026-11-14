// Package totp makes the code an authenticator app shows, for a site that
// asks for one when you sign in, from the secret the site showed beside its
// QR code. An app that keeps the secret can sign in again on its own.
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
)

// Code is the code for secret at t, as RFC 6238 makes it, with what nearly
// every site uses: SHA-1, 30 seconds, 6 digits. secret is base32, as sites
// show it; spaces, dashes, padding, and lower case are allowed.
func Code(secret string, t time.Time) (string, error) {
	clean := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(secret))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(clean, "="))
	if err != nil {
		return "", fmt.Errorf("the authenticator secret is not base32: %w", err)
	}
	mac := hmac.New(sha1.New, key)
	binary.Write(mac, binary.BigEndian, uint64(t.Unix()/30))
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0xf
	code := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1_000_000), nil
}
