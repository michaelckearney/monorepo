package browser

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A test site keeps its URL, https://bank.localhost:8443, while it listens
// on another port, with a certificate from a CA only the browser trusts.
func TestHostRulesAndTrustedCAsReachATestSite(t *testing.T) {
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(2), DNSNames: []string{"bank.localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<title>bank</title><p id=host>%s</p>", r.Host)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der, caDER}, PrivateKey: key}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	b := open(t, Options{HostRules: []string{"MAP *.localhost:8443 " + srv.Listener.Addr().String()}, TrustedCAs: []*x509.Certificate{ca}})
	tab, err := b.Open(quick(t), "https://bank.localhost:8443/")
	if err != nil {
		t.Fatal(err)
	}
	if got := evaluate(t, tab, `() => [document.title, document.querySelector("#host").textContent, isSecureContext]`); got != `["bank","bank.localhost:8443",true]` {
		t.Errorf("the page shows %s", got)
	}
}
