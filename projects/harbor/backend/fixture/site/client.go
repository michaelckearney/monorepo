package site

import (
	"net/http"
	"time"
)

// Client calls the fixtures' own sites, as a server-side app calls an
// identity provider, through transport, from fixture.Transport, which sends
// them where the fixtures listen and trusts their CA.
func Client(transport *http.Transport) *http.Client {
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
		// An app reads redirects itself, as with an identity provider's.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
