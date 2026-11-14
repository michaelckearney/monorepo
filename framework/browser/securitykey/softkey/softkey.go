// Package softkey is a security key in software, for tests: CTAPHID over
// 64-byte reports, and CTAP2 with PIN protocol 2, as a FIDO2 key speaks
// them.
package softkey

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"math/big"
	"sync"

	"github.com/fxamacker/cbor/v2"
)

// CTAPHID and CTAP2, as package securitykey speaks them.
const (
	broadcast        = 0xffffffff
	cmdInit          = 0x06
	cmdCBOR          = 0x10
	cmdKeepalive     = 0x3b
	statusNeedsTouch = 2

	ctapMakeCredential = 0x01
	ctapGetAssertion   = 0x02
	ctapGetInfo        = 0x04
	ctapClientPIN      = 0x06

	statusNoCredentials = 0x2e
	statusPINInvalid    = 0x31
	statusPINRequired   = 0x36
)

var encode = func() cbor.EncMode {
	m, err := cbor.CTAP2EncOptions().EncMode()
	if err != nil {
		panic(err)
	}
	return m
}()

type keyAgreement struct {
	Kty int    `cbor:"1,keyasint"`
	Alg int    `cbor:"3,keyasint"`
	Crv int    `cbor:"-1,keyasint"`
	X   []byte `cbor:"-2,keyasint"`
	Y   []byte `cbor:"-3,keyasint"`
}

type credential struct {
	Type string `cbor:"type"`
	ID   []byte `cbor:"id"`
}

// Key is a security key in software, with a PIN, or none if empty.
type Key struct {
	pin     string
	retries int
	// Touched is how many times it waited for a touch.
	Touched int

	mu    sync.Mutex
	out   chan []byte
	in    []byte // the message being received
	want  int
	cid   uint32
	agree *ecdh.PrivateKey
	token []byte
	creds []softCred
}

type softCred struct {
	id, user []byte
	rpHash   [32]byte
	key      *ecdsa.PrivateKey
}

// New returns a key with the PIN, or none if pin is empty.
func New(pin string) *Key {
	return &Key{pin: pin, retries: 8, out: make(chan []byte, 64)}
}

// Close does nothing: the key is never unplugged.
func (k *Key) Close() error { return nil }

// Read waits for the key's next report.
func (k *Key) Read(ctx context.Context) ([]byte, error) {
	select {
	case r := <-k.out:
		return r, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Write takes a report, and answers once a message is whole.
func (k *Key) Write(report []byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	cid := binary.BigEndian.Uint32(report)
	if report[4]&0x80 != 0 {
		k.cid = cid
		k.want = int(binary.BigEndian.Uint16(report[5:]))
		k.in = append([]byte{report[4] &^ 0x80}, report[7:min(7+k.want, 64)]...)
	} else {
		k.in = append(k.in, report[5:min(5+k.want-(len(k.in)-1), 64)]...)
	}
	if len(k.in)-1 < k.want {
		return nil
	}
	cmd, data := k.in[0], k.in[1:]
	switch cmd {
	case cmdInit:
		k.reply(cmdInit, append(append(append([]byte{}, data...), 0, 0, 0, 7), 2, 1, 0, 0, 5))
	case cmdCBOR:
		k.reply(cmdCBOR, k.ctap(data[0], data[1:]))
	}
	return nil
}

// reply sends a message, framed in reports. k.mu is held.
func (k *Key) reply(cmd byte, data []byte) {
	cid := k.cid
	if cmd == cmdInit {
		cid = broadcast
	}
	r := make([]byte, 64)
	binary.BigEndian.PutUint32(r, cid)
	r[4] = 0x80 | cmd
	binary.BigEndian.PutUint16(r[5:], uint16(len(data)))
	n := copy(r[7:], data)
	k.out <- r
	for seq := byte(0); n < len(data); seq++ {
		r = make([]byte, 64)
		binary.BigEndian.PutUint32(r, cid)
		r[4] = seq
		n += copy(r[5:], data[n:])
		k.out <- r
	}
}

func (k *Key) ctap(cmd byte, params []byte) []byte {
	ok := func(v any) []byte {
		raw, _ := encode.Marshal(v)
		return append([]byte{0}, raw...)
	}
	switch cmd {
	case ctapGetInfo:
		return ok(map[int]any{1: []string{"FIDO_2_0", "FIDO_2_1"},
			4: map[string]bool{"rk": true, "up": true, "clientPin": k.pin != "", "pinUvAuthToken": true}, 6: []int{2, 1}})
	case ctapClientPIN:
		var p struct {
			Sub        int           `cbor:"2,keyasint"`
			Key        *keyAgreement `cbor:"3,keyasint"`
			PinHashEnc []byte        `cbor:"6,keyasint"`
		}
		cbor.Unmarshal(params, &p)
		switch p.Sub {
		case 1:
			return ok(map[int]any{3: k.retries})
		case 2:
			k.agree, _ = ecdh.P256().GenerateKey(rand.Reader)
			pt := k.agree.PublicKey().Bytes()
			return ok(map[int]any{1: keyAgreement{Kty: 2, Alg: -25, Crv: 1, X: pt[1:33], Y: pt[33:]}})
		case 9:
			theirs, _ := ecdh.P256().NewPublicKey(append(append([]byte{4}, p.Key.X...), p.Key.Y...))
			z, _ := k.agree.ECDH(theirs)
			salt := make([]byte, 32)
			aesKey, _ := hkdf.Key(sha256.New, z, salt, "CTAP2 AES key", 32)
			block, _ := aes.NewCipher(aesKey)
			plain := make([]byte, len(p.PinHashEnc)-16)
			cipher.NewCBCDecrypter(block, p.PinHashEnc[:16]).CryptBlocks(plain, p.PinHashEnc[16:])
			want := sha256.Sum256([]byte(k.pin))
			if !bytes.Equal(plain, want[:16]) {
				k.retries--
				return []byte{statusPINInvalid}
			}
			k.token = make([]byte, 32)
			rand.Read(k.token)
			iv := make([]byte, 16)
			rand.Read(iv)
			sealed := make([]byte, 32)
			cipher.NewCBCEncrypter(block, iv).CryptBlocks(sealed, k.token)
			return ok(map[int]any{2: append(iv, sealed...)})
		}
	case ctapMakeCredential:
		var p struct {
			Hash  []byte         `cbor:"1,keyasint"`
			RP    map[string]any `cbor:"2,keyasint"`
			User  map[string]any `cbor:"3,keyasint"`
			Param []byte         `cbor:"8,keyasint"`
		}
		cbor.Unmarshal(params, &p)
		if status := k.checkPIN(p.Hash, p.Param); status != 0 {
			return []byte{status}
		}
		k.touch()
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		c := softCred{id: make([]byte, 24), user: p.User["id"].([]byte), rpHash: sha256.Sum256([]byte(p.RP["id"].(string))), key: key}
		rand.Read(c.id)
		k.creds = append(k.creds, c)
		pad := func(b *big.Int) []byte { return b.FillBytes(make([]byte, 32)) }
		cose, _ := encode.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: pad(key.X), -3: pad(key.Y)})
		auth := append(append([]byte{}, c.rpHash[:]...), k.flags(p.Param)|0x40, 0, 0, 0, 1)
		auth = append(auth, make([]byte, 16)...)
		auth = binary.BigEndian.AppendUint16(auth, uint16(len(c.id)))
		auth = append(append(auth, c.id...), cose...)
		return ok(map[int]any{1: "none", 2: auth, 3: map[string]any{}})
	case ctapGetAssertion:
		var p struct {
			RPID  string       `cbor:"1,keyasint"`
			Hash  []byte       `cbor:"2,keyasint"`
			Allow []credential `cbor:"3,keyasint"`
			Param []byte       `cbor:"6,keyasint"`
		}
		cbor.Unmarshal(params, &p)
		if status := k.checkPIN(p.Hash, p.Param); status != 0 {
			return []byte{status}
		}
		rpHash := sha256.Sum256([]byte(p.RPID))
		for _, c := range k.creds {
			if c.rpHash != rpHash {
				continue
			}
			k.touch()
			auth := append(append([]byte{}, rpHash[:]...), k.flags(p.Param), 0, 0, 0, 2)
			signed := sha256.Sum256(append(append([]byte{}, auth...), p.Hash...))
			sig, _ := ecdsa.SignASN1(rand.Reader, c.key, signed[:])
			return ok(map[int]any{1: credential{Type: "public-key", ID: c.id}, 2: auth, 3: sig, 4: map[string]any{"id": c.user}})
		}
		return []byte{statusNoCredentials}
	}
	return []byte{0x01}
}

// checkPIN checks a request's pinUvAuthParam, when the key has a PIN.
func (k *Key) checkPIN(hash, param []byte) byte {
	if k.pin == "" {
		return 0
	}
	if param == nil {
		return statusPINRequired
	}
	mac := hmac.New(sha256.New, k.token)
	mac.Write(hash)
	if !hmac.Equal(param, mac.Sum(nil)) {
		return 0x33 // CTAP2_ERR_PIN_AUTH_INVALID
	}
	return 0
}

// flags are user present, and user verified when a PIN was used.
func (k *Key) flags(param []byte) byte {
	if param != nil {
		return 0x05
	}
	return 0x01
}

// touch has the key wait for a touch, as a key says with a keepalive.
func (k *Key) touch() {
	k.Touched++
	k.reply(cmdKeepalive, []byte{statusNeedsTouch})
}
