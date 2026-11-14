package site

import (
	"testing"
	"time"
)

// RFC 6238's test vector, for SHA-1 at 59 seconds, is 94287082; the last six
// digits are the code.
func TestTOTPMatchesTheRFC(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" // "12345678901234567890"
	if got := TOTP(secret, time.Unix(59, 0)); got != "287082" {
		t.Errorf("TOTP = %s", got)
	}
}

func TestJWT(t *testing.T) {
	key := []byte("k")
	token := SignJWT(key, Claims{Subject: "alex", Expires: time.Now().Add(time.Minute).Unix()})
	var c Claims
	if err := ParseJWT(key, token, &c); err != nil || c.Subject != "alex" {
		t.Fatalf("%v %+v", err, c)
	}
	if err := ParseJWT([]byte("other"), token, &c); err == nil {
		t.Error("a token signed with another key was accepted")
	}
	old := SignJWT(key, Claims{Subject: "alex", Expires: time.Now().Add(-time.Second).Unix()})
	if err := ParseJWT(key, old, &c); err != ErrExpired {
		t.Errorf("expired: %v", err)
	}
}

func TestMoney(t *testing.T) {
	for cents, want := range map[int64]string{0: "$0.00", 123456: "$1,234.56", -1200: "-$12.00", 100000000: "$1,000,000.00"} {
		if got := Money(cents); got != want {
			t.Errorf("Money(%d) = %s, want %s", cents, got, want)
		}
	}
}
