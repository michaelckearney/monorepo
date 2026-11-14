package securitykey

import (
	"errors"
	"testing"
)

// Finding keys works with none plugged in, as on a machine without one.
func TestDevices(t *testing.T) {
	devs, err := Devices()
	if errors.Is(err, ErrNoUSB) {
		t.Skip(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range devs {
		t.Logf("found a security key: %T", d)
		d.Close()
	}
}
