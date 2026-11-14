// Package securitykey talks to a USB security key, such as a YubiKey, to
// make a passkey on it and to sign in with one. Headless Chromium cannot
// show the dialog Chrome talks to a security key through, so this package
// does it itself, in Go, with no library to install.
//
// A security key speaks CTAP2 (FIDO's Client to Authenticator Protocol),
// as CBOR messages, framed in 64-byte USB HID reports (CTAPHID). Only
// finding and opening the key differs between platforms (device_*.go).
package securitykey

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrNoUSB is Devices' error in a build that cannot reach security keys.
var ErrNoUSB = errors.New("this build of the app cannot reach security keys")

// Device is a security key, as 64-byte HID reports in each direction.
type Device interface {
	// Write sends one report.
	Write(report []byte) error
	// Read waits for the next report, or for ctx to end.
	Read(ctx context.Context) ([]byte, error)
	Close() error
}

// CTAPHID's framing.
const (
	reportSize = 64
	initData   = reportSize - 7 // an initialization packet's payload
	contData   = reportSize - 5 // a continuation packet's
	broadcast  = 0xffffffff     // the channel a channel is asked for on

	cmdInit      = 0x06
	cmdCBOR      = 0x10
	cmdCancel    = 0x11
	cmdKeepalive = 0x3b
	cmdError     = 0x3f

	// A keepalive's status: the key is working, or waits for your touch.
	statusProcessing = 1
	statusNeedsTouch = 2
)

// channel is a conversation with a key.
type channel struct {
	dev Device
	cid uint32
}

// openChannel asks the key for a channel of this program's own.
func openChannel(ctx context.Context, dev Device) (*channel, error) {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	c := &channel{dev: dev, cid: broadcast}
	if err := c.send(cmdInit, nonce); err != nil {
		return nil, err
	}
	for {
		cmd, data, err := c.receive(ctx, nil)
		if err != nil {
			return nil, err
		}
		// A reply to another program's request on the broadcast channel
		// has another nonce.
		if cmd == cmdInit && len(data) >= 12 && string(data[:8]) == string(nonce) {
			c.cid = binary.BigEndian.Uint32(data[8:12])
			return c, nil
		}
	}
}

// send sends a message, in as many reports as it takes.
func (c *channel) send(cmd byte, data []byte) error {
	report := make([]byte, reportSize)
	binary.BigEndian.PutUint32(report, c.cid)
	report[4] = 0x80 | cmd
	binary.BigEndian.PutUint16(report[5:], uint16(len(data)))
	n := copy(report[7:], data)
	if err := c.dev.Write(report); err != nil {
		return err
	}
	for seq := byte(0); n < len(data); seq++ {
		report = make([]byte, reportSize)
		binary.BigEndian.PutUint32(report, c.cid)
		report[4] = seq
		n += copy(report[5:], data[n:])
		if err := c.dev.Write(report); err != nil {
			return err
		}
	}
	return nil
}

// receive waits for the next message on the channel, calling keepalive with
// the status of each keepalive the key sends while it works. If ctx ends
// first, it asks the key to cancel.
func (c *channel) receive(ctx context.Context, keepalive func(status byte)) (cmd byte, data []byte, err error) {
	for {
		report, err := c.dev.Read(ctx)
		if err != nil {
			if ctx.Err() != nil && c.cid != broadcast {
				_ = c.send(cmdCancel, nil)
			}
			return 0, nil, err
		}
		if len(report) < 7 || binary.BigEndian.Uint32(report) != c.cid || report[4]&0x80 == 0 {
			continue
		}
		cmd = report[4] &^ 0x80
		size := int(binary.BigEndian.Uint16(report[5:]))
		data = append([]byte{}, report[7:min(7+size, len(report))]...)
		for seq := byte(0); len(data) < size; seq++ {
			report, err := c.dev.Read(ctx)
			if err != nil {
				return 0, nil, err
			}
			if binary.BigEndian.Uint32(report) != c.cid || report[4] != seq {
				return 0, nil, errors.New("the security key's reply arrived out of order")
			}
			data = append(data, report[5:min(5+size-len(data), len(report))]...)
		}
		switch cmd {
		case cmdKeepalive:
			if keepalive != nil && len(data) > 0 {
				keepalive(data[0])
			}
			continue
		case cmdError:
			code := byte(0)
			if len(data) > 0 {
				code = data[0]
			}
			return 0, nil, fmt.Errorf("the security key's USB connection failed (CTAPHID error %#x)", code)
		}
		return cmd, data, nil
	}
}
