package school

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// district is the district's SAML identity provider, with its own sign-in
// form and session.
type district struct {
	idp      *saml.IdentityProvider
	pages    *site.Pages
	sessions *site.Sessions[string]
	data     data
	mux      *http.ServeMux
}

func newIdP(store *site.Store, d data, key *rsa.PrivateKey, cert *x509.Certificate, base *url.URL, sp *samlsp.Middleware) *district {
	s := &district{
		pages:    site.NewPages(site.Brand{Name: "Maple Grove School District", Color: "#1e3a8a", Footer: "Maple Grove School District 41."}, sub("sso"), nil),
		sessions: site.NewSessions[string](store, site.SessionOptions{Cookie: "mgsd_sso", Absolute: 8 * time.Hour}),
		data:     d,
	}
	s.idp = &saml.IdentityProvider{
		Key: key, Certificate: cert, Logger: log.New(os.Stderr, "maple grove sso: ", log.LstdFlags),
		MetadataURL:             *base.ResolveReference(&url.URL{Path: "/metadata"}),
		SSOURL:                  *base.ResolveReference(&url.URL{Path: "/sso"}),
		ServiceProviderProvider: spProvider{sp},
		SessionProvider:         s,
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /metadata", s.idp.ServeMetadata)
	s.mux.HandleFunc("/sso", s.idp.ServeSSO)
	s.mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		s.pages.Render(w, 200, "home", "", map[string]any{"Portal": hosts.URL(hosts.School)})
	})
	return s
}

func (s *district) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// GetSession is where the IdP signs a user in: the district's own form.
// The form carries the portal's SAML request through, so it can be answered
// after the password.
func (s *district) GetSession(w http.ResponseWriter, r *http.Request, req *saml.IdpAuthnRequest) *saml.Session {
	if sess, _ := s.sessions.Get(r); sess != nil {
		return s.session(sess.Data)
	}
	view := map[string]any{
		"SAMLRequest": base64.StdEncoding.EncodeToString(req.RequestBuffer),
		"RelayState":  req.RelayState,
	}
	if r.Method == http.MethodPost && r.PostFormValue("username") != "" {
		if r.PostFormValue("username") == s.data.Parent.Username && r.PostFormValue("password") == site.Password {
			s.sessions.Start(w, s.data.Parent.Username)
			return s.session(s.data.Parent.Username)
		}
		view["Error"] = "Your username or password is incorrect."
	}
	s.pages.Render(w, 200, "login", "Sign in", view)
	return nil
}

func (s *district) session(username string) *saml.Session {
	return &saml.Session{
		ID: site.Token(), CreateTime: time.Now(), ExpireTime: time.Now().Add(time.Hour),
		NameID: username, UserName: username, UserEmail: s.data.Parent.Email, UserCommonName: s.data.Parent.Name,
		Groups: []string{"parents"},
	}
}

// spProvider knows the one service provider: the parent portal.
type spProvider struct{ sp *samlsp.Middleware }

func (p spProvider) GetServiceProvider(_ *http.Request, id string) (*saml.EntityDescriptor, error) {
	md := p.sp.ServiceProvider.Metadata()
	if id != md.EntityID {
		return nil, os.ErrNotExist
	}
	return md, nil
}
