package fixture

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

// A sign-in, and the CA, last while the fixtures restart on the same store,
// as when Harbor restarts after a change to the backend.
func TestASignInLastsARestart(t *testing.T) {
	dir := t.TempDir()
	f, err := Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Transport: Transport(http.DefaultTransport.(*http.Transport))}
	portal := hosts.URL(hosts.NorthwindID)
	r, err := client.PostForm(portal+"/", url.Values{"username": {"alex@example.localhost"}, "password": {site.Password}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	state := func() string {
		t.Helper()
		r, err := client.Get(portal + "/api/state")
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		return string(body)
	}
	if got := state(); !strings.Contains(got, `"username":"alex"`) {
		t.Fatalf("after signing in, Northwind ID says %s", got)
	}
	ca := f.caPEM
	f.Stop()
	if Transport(http.DefaultTransport.(*http.Transport)) != http.DefaultTransport {
		t.Error("Transport changed a client's transport while no fixtures serve")
	}

	f, err = Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Stop()
	client.Transport = Transport(http.DefaultTransport.(*http.Transport))
	if got := state(); !strings.Contains(got, `"username":"alex"`) {
		t.Errorf("after a restart, Northwind ID says %s", got)
	}
	if !bytes.Equal(f.caPEM, ca) {
		t.Error("the CA changed with a restart")
	}
}
