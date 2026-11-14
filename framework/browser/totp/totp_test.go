package totp

import (
	"testing"
	"time"
)

// RFC 6238's own test vectors, for SHA-1, cut to 6 digits.
func TestCode(t *testing.T) {
	// "12345678901234567890" in base32.
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	for at, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		got, err := Code(secret, time.Unix(at, 0))
		if err != nil || got != want {
			t.Errorf("Code at %d = %s, %v; want %s", at, got, err, want)
		}
	}
	// As a site shows it.
	if got, err := Code("gezd gnbv-gy3t qojq gezd gnbv gy3t qojq====", time.Unix(59, 0)); err != nil || got != "287082" {
		t.Errorf("Code of a spaced secret = %s, %v", got, err)
	}
	if _, err := Code("not base32!", time.Unix(59, 0)); err == nil {
		t.Error("a secret that is not base32 gave a code")
	}
}
