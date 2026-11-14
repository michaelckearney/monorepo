package fixture

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// authority is the fixtures' CA, from the store, made the first time. It
// lasts, so a browser of your own that trusts it keeps trusting it. Its
// name constraints keep it to localhost and the names under it, so a copy
// of its key can sign nothing else.
func authority(store *site.Store) (*x509.Certificate, *signer, error) {
	raw := store.Secret("fixtures CA", func() []byte {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			panic(err)
		}
		now := time.Now()
		tmpl := &x509.Certificate{
			SerialNumber: serial(),
			Subject:      pkix.Name{CommonName: "Harbor fixtures"},
			NotBefore:    now.Add(-time.Hour),
			NotAfter:     now.AddDate(10, 0, 0),
			IsCA:         true, BasicConstraintsValid: true, MaxPathLenZero: true,
			KeyUsage:                    x509.KeyUsageCertSign,
			PermittedDNSDomainsCritical: true,
			PermittedDNSDomains:         []string{"localhost", ".localhost"},
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			panic(err)
		}
		keyDER, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			panic(err)
		}
		return append(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	})
	keyBlock, rest := pem.Decode(raw)
	certBlock, _ := pem.Decode(rest)
	if keyBlock == nil || certBlock == nil {
		return nil, nil, errors.New("the fixtures' CA in their store is not two PEM blocks")
	}
	k, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("the fixtures' CA key: %w", err)
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("the fixtures' CA key is not ECDSA")
	}
	ca, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("the fixtures' CA: %w", err)
	}
	return ca, &signer{ca: ca, key: key, certs: map[string]*tls.Certificate{}}, nil
}

func pemCertificate(c *x509.Certificate) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		panic(err)
	}
	return n
}

// leafFor is how long a site's certificate lasts: Apple's limit for one a
// user trusts is 825 days, and browsers' for a public one 398.
const leafFor = 397 * 24 * time.Hour

// signer makes a certificate for each name a browser asks for, from the
// CA. A list of names made in advance would be one more thing to keep up
// to date with the sites.
type signer struct {
	ca  *x509.Certificate
	key *ecdsa.PrivateKey

	mu    sync.Mutex
	certs map[string]*tls.Certificate
}

func (s *signer) certificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := hello.ServerName
	if !local(name) {
		return nil, fmt.Errorf("%q is not a fixture site's name; they are localhost and the names under it", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c := s.certs[name]; c != nil {
		return c, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	notAfter := time.Now().Add(leafFor)
	if s.ca.NotAfter.Before(notAfter) {
		notAfter = s.ca.NotAfter
	}
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, s.ca, &key.PublicKey, s.key)
	if err != nil {
		return nil, err
	}
	// The CA goes with each certificate: Chromium's trust of it by its key's
	// hash matches only certificates the site sends.
	c := &tls.Certificate{Certificate: [][]byte{der, s.ca.Raw}, PrivateKey: key}
	s.certs[name] = c
	return c, nil
}
