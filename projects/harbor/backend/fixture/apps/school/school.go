// Package school is Maple Grove Schools' parent portal, the district's SAML
// sign-in, and the gradebook another company runs in an iframe.
//
// Scenarios: grades only inside a cross-origin iframe (D20), whose cookie is
// partitioned by the page around it (S11); the district's SAML sign-in, a
// form that posts itself back to the portal (A15); signing in to the
// gradebook inside the iframe, by a launch link (A18); choosing a child
// (A19).
package school

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/pem"
	"io/fs"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"github.com/crewjam/saml/samlsp"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

//go:embed data portal sso gradebook
var files embed.FS

type Student struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Grade    int    `json:"grade"`
	School   string `json:"school"`
	Teacher  string `json:"teacher"`
	Homeroom string `json:"homeroom"`
}

type Assignment struct {
	ID      string `json:"id"`
	Student string `json:"student"`
	Course  string `json:"course"`
	Title   string `json:"title"`
	Due     string `json:"due"`
	Status  string `json:"status"`
}

type Event struct {
	Date   string `json:"date"`
	Title  string `json:"title"`
	School string `json:"school"`
}

type Course struct {
	Course  string  `json:"course"`
	Teacher string  `json:"teacher"`
	Percent float64 `json:"percent"`
	Letter  string  `json:"letter"`
	Scores  []struct {
		Title string `json:"title"`
		Score int    `json:"score"`
		Max   int    `json:"max"`
	} `json:"scores"`
}

type data struct {
	Parent struct {
		Username string `json:"username"`
		Name     string `json:"name"`
		Email    string `json:"email"`
	} `json:"parent"`
	Students    []Student           `json:"students"`
	Assignments []Assignment        `json:"assignments"`
	Events      []Event             `json:"events"`
	Grades      map[string][]Course `json:"grades"`
}

// New returns the portal, the district's sign-in, and the gradebook.
func New(store *site.Store) (portal, sso, gradebook http.Handler) {
	var d data
	site.Load(files, "data/school.json", &d)

	// The two sides of SAML trust each other's certificates. Neither leaves
	// the fixtures. The portal signs its session cookie with its key, so a
	// new key would sign you out.
	idpKey, idpCert := keyPair(store, "Maple Grove School District")
	spKey, spCert := keyPair(store, "Maple Grove parent portal")
	ssoURL := mustURL(hosts.URL(hosts.SchoolSSO))
	portalURL := mustURL(hosts.URL(hosts.School))

	sp, err := samlsp.New(samlsp.Options{
		URL: *portalURL, Key: spKey, Certificate: spCert, CookieName: "mg_portal", CookieSameSite: http.SameSiteLaxMode,
	})
	if err != nil {
		panic(err)
	}
	idp := newIdP(store, d, idpKey, idpCert, ssoURL, sp)
	sp.ServiceProvider.IDPMetadata = idp.idp.Metadata()

	key := store.Key("maple grove launch links")
	return newPortal(d, sp, key), idp, newGradebook(store, d, key)
}

// keyPair is name's SAML key and certificate, kept in the store as one
// secret, so the two always match.
func keyPair(store *site.Store, name string) (*rsa.PrivateKey, *x509.Certificate) {
	raw := store.Secret(name+" SAML key pair", func() []byte {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0),
			KeyUsage: x509.KeyUsageDigitalSignature,
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
		if err != nil {
			panic(err)
		}
		return append(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}),
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	})
	keyBlock, rest := pem.Decode(raw)
	certBlock, _ := pem.Decode(rest)
	if keyBlock == nil || certBlock == nil {
		panic(name + "'s SAML key pair in the fixtures' store is not two PEM blocks")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		panic(err)
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		panic(err)
	}
	return key, cert
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func sub(dir string) fs.FS {
	f, err := fs.Sub(files, dir)
	if err != nil {
		panic(err)
	}
	return f
}
