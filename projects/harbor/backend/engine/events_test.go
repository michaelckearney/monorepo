package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/michaelckearney/monorepo/framework/browser"
)

func TestEventsSayWhatAnActionDid(t *testing.T) {
	for _, c := range []struct {
		got, want string
	}{
		{requestLine(browser.Request{Method: "GET", URL: "https://tasks.example.com/", Navigation: true, Status: 200}), "went to https://tasks.example.com/: 200"},
		{requestLine(browser.Request{Method: "POST", URL: "https://tasks.example.com/graphql", Status: 401}), "request POST https://tasks.example.com/graphql: 401"},
		{requestLine(browser.Request{Method: "GET", URL: "https://tasks.example.com/api", Failed: "net::ERR_FAILED"}), "request GET https://tasks.example.com/api: failed, net::ERR_FAILED"},
		{requestLine(browser.Request{Method: "GET", URL: "https://tasks.example.com/poll"}), "request GET https://tasks.example.com/poll: no response yet"},
		{savedLine(browser.Saved{Origin: "https://tasks.example.com", Local: true, Change: "added", Key: "tasklane.accessToken"}), "localStorage of https://tasks.example.com: added tasklane.accessToken"},
		{savedLine(browser.Saved{Origin: "https://tasks.example.com", Change: "cleared"}), "sessionStorage of https://tasks.example.com: cleared"},
		{cookieLine(browser.CookieChange{Name: "sid", Domain: ".example.com", Path: "/", Change: "added"}), "cookie of .example.com: added sid"},
		// A sign-in's address, with its state, without the query.
		{cut("https://id.example.com/authorize?" + strings.Repeat("state=x&", 30)), "https://id.example.com/authorize?…"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// Past the limit, requests go first; the same line twice is one, counted.
func TestEventsAreLimited(t *testing.T) {
	list := []event{{line: "went to https://a.example/: 200"}}
	for i := range 30 {
		list = append(list, event{line: fmt.Sprintf("request GET https://a.example/%d: 200", i), request: true})
	}
	list = append(list, event{line: "request GET https://a.example/poll: 200", request: true}, event{line: "request GET https://a.example/poll: 200", request: true})
	list = append(list, event{line: "localStorage of https://a.example: added token"})
	got := limit(list, eventLimit)
	if len(got) != eventLimit+1 || got[0] != "went to https://a.example/: 200" || got[len(got)-2] != "localStorage of https://a.example: added token" || got[len(got)-1] != "and 13 more" {
		t.Errorf("got %d lines: %q", len(got), got)
	}
	if got := limit(list[len(list)-3:], eventLimit); strings.Join(got, "; ") != "request GET https://a.example/poll: 200 (2 times); localStorage of https://a.example: added token" {
		t.Errorf("got %q", got)
	}
}
