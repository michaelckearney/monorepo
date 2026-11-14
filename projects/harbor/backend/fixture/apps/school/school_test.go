package school

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/hosts"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/internal/fake"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture/site"
)

var input = regexp.MustCompile(`<input[^>]*name="([^"]+)"[^>]*value="([^"]*)"`)

func TestSAMLSignInAndTheGradebook(t *testing.T) {
	portal, sso, gradebook := New(fake.Store(t))
	b := fake.New(t, hosts.School, portal)
	b.Serve(hosts.SchoolSSO, sso)
	b.Serve(hosts.Gradebook, gradebook)

	p := b.Get("/students/s-riley")
	if p.URL.Host != hosts.SchoolSSO+":"+hosts.Port || !strings.Contains(p.Body, "District sign-in") {
		t.Fatalf("not sent to the district: %s %s", p.URL, p.Body)
	}
	form := url.Values{"username": {"alex@example.localhost"}, "password": {site.Password},
		"SAMLRequest": {fake.Field(p.Body, "SAMLRequest")}, "RelayState": {fake.Field(p.Body, "RelayState")}}
	p = b.At(hosts.SchoolSSO).Post("/sso", form)

	// The district answers with a form that posts itself to the portal.
	action := html.UnescapeString(regexp.MustCompile(`action="([^"]+)"`).FindStringSubmatch(p.Body)[1])
	post := url.Values{}
	for _, m := range input.FindAllStringSubmatch(p.Body, -1) {
		post.Set(m[1], html.UnescapeString(m[2]))
	}
	if action != hosts.URL(hosts.School)+"/saml/acs" || post.Get("SAMLResponse") == "" {
		t.Fatalf("no SAML response form: %s", p.Body)
	}
	p = b.At(hosts.SchoolSSO).Post(action, post)
	if p.Path != "/students/s-riley" || !strings.Contains(p.Body, "Plate tectonics lab report") {
		t.Fatalf("after SAML: %s\n%s", p.Path, p.Body)
	}

	launch := html.UnescapeString(regexp.MustCompile(`src="(https://gradebook[^"]+)"`).FindStringSubmatch(p.Body)[1])
	g := b.Get(launch)
	if !strings.Contains(g.Body, "Riley Morgan: current grades") || !strings.Contains(g.Body, "Algebra I") {
		t.Fatalf("gradebook: %s", g.Body)
	}
	if g.Header.Get("Content-Security-Policy") != "frame-ancestors "+hosts.URL(hosts.School) {
		t.Errorf("CSP: %q", g.Header.Get("Content-Security-Policy"))
	}
	if again := b.Get(launch); strings.Contains(again.Body, "current grades") {
		t.Error("a launch link worked twice")
	}
}
