package securitykey

import (
	"bytes"
	"context"
	"errors"
)

// isFIDO reports whether a HID report descriptor is a FIDO device's: usage
// page 0xF1D0, usage 1, as every security key declares itself.
func isFIDO(descriptor []byte) bool {
	page := bytes.Index(descriptor, []byte{0x06, 0xd0, 0xf1})
	return page >= 0 && bytes.Contains(descriptor[page:], []byte{0x09, 0x01})
}

// reports is a device's incoming reports, read by a goroutine of its own:
// reading blocks, and Read must also end with its context.
type reports struct {
	in   chan []byte
	done chan struct{}
	err  error
}

func newReports() *reports {
	return &reports{in: make(chan []byte, 16), done: make(chan struct{})}
}

// put is called with each report the device sends.
func (r *reports) put(report []byte) {
	select {
	case r.in <- report:
	case <-r.done:
	}
}

// stop is called when the device can send no more.
func (r *reports) stop(err error) {
	select {
	case <-r.done:
	default:
		r.err = err
		close(r.done)
	}
}

func (r *reports) Read(ctx context.Context) ([]byte, error) {
	select {
	case report := <-r.in:
		return report, nil
	case <-r.done:
		if r.err == nil {
			return nil, errors.New("the security key was unplugged")
		}
		return nil, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
