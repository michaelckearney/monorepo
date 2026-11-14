//go:build !linux && !darwin

package securitykey

// Devices are the security keys plugged in: none this build can reach.
func Devices() ([]Device, error) {
	return nil, ErrNoUSB
}
