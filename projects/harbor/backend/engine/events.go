package engine

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"github.com/michaelckearney/monorepo/framework/browser"
)

// What an action in the agent's browser did, as short lines the agent reads
// with the page it left: the pages it went to, the site's requests, and
// the cookies and saved values it added, changed, or removed, by name.
// Shown only the page it ended on, one model spent a benchmark trial's
// 57 calls guessing where Tasklane kept its sign-in: cookies named
// auth_token or session, a localStorage key auth_token. The sign-in had
// saved tasklane.accessToken, in the requests and storage of pages already
// gone.

// StaticTypes are the resource types left out of what an action did, and of
// browser_network_requests unless asked: they are how a page looks, not
// where its data comes from.
var StaticTypes = []string{"Image", "Stylesheet", "Script", "Font", "Media", "Manifest", "Ping", "CSPViolationReport", "Preflight"}

const (
	// eventLimit keeps an action's events short: every result stays in the
	// agent's context, and is sent again with each call after.
	eventLimit = 20
	// signInEventLimit is a sign-in's: a whole sign-in goes through more
	// pages than one action, and what it saved is the point of watching it.
	signInEventLimit = 50
	// urlLimit cuts a long address, such as a sign-in's with its state.
	urlLimit = 150
)

// watch is how the browser was before an action: where each tab was in its
// life, and the profile's cookies.
type watch struct {
	marks   map[*browser.Tab]browser.Mark
	cookies browser.CookieMark
}

// watchFrom notes the browser as it is, before an action in tabs.
func (e *Engine) watchFrom(ctx context.Context, tabs []*browser.Tab) (*watch, error) {
	cookies, err := e.browser.CookieMark(ctx)
	if err != nil {
		return nil, err
	}
	w := &watch{marks: map[*browser.Tab]browser.Mark{}, cookies: cookies}
	for _, t := range tabs {
		w.marks[t] = t.Mark()
	}
	return w, nil
}

// event is one line, and when it happened in its tab.
type event struct {
	at   browser.Mark
	line string
	// request is set for a request that is not a page load, the lines left
	// out first when there are too many.
	request bool
}

// events are what happened in tabs since w: a tab opened since has its whole
// life after it.
func (e *Engine) events(ctx context.Context, w *watch, tabs []*browser.Tab, most int) ([]string, error) {
	// A tab's events in the order they happened, and a tab's after the
	// tab's that opened it: each tab counts its own time.
	var list []event
	for _, t := range tabs {
		mark := w.marks[t]
		var in []event
		for _, r := range t.RequestsSince(mark) {
			if slices.Contains(StaticTypes, r.Type) || !web(r.URL) {
				continue
			}
			in = append(in, event{r.At, requestLine(r), !r.Navigation})
		}
		for _, s := range t.SavedSince(mark) {
			in = append(in, event{s.At, savedLine(s), false})
		}
		sort.SliceStable(in, func(i, j int) bool { return in[i].at < in[j].at })
		list = append(list, in...)
	}

	cookies, err := e.browser.CookiesChangedSince(ctx, w.cookies)
	if err != nil {
		return nil, err
	}
	for _, c := range cookies {
		list = append(list, event{line: cookieLine(c)})
	}
	return limit(list, most), nil
}

// web reports whether address is a site's, not a page of Chromium's own or
// data in the page.
func web(address string) bool {
	return strings.HasPrefix(address, "https://") || strings.HasPrefix(address, "http://")
}

func requestLine(r browser.Request) string {
	what := "request " + r.Method + " " + cut(r.URL)
	if r.Navigation {
		what = "went to " + cut(r.URL)
	}
	switch {
	case r.Failed != "":
		return what + ": failed, " + r.Failed
	case r.Status == 0:
		return what + ": no response yet"
	}
	return fmt.Sprintf("%s: %d", what, r.Status)
}

func savedLine(s browser.Saved) string {
	storage := "sessionStorage"
	if s.Local {
		storage = "localStorage"
	}
	if s.Change == "cleared" {
		return fmt.Sprintf("%s of %s: cleared", storage, s.Origin)
	}
	return fmt.Sprintf("%s of %s: %s %s", storage, s.Origin, s.Change, s.Key)
}

func cookieLine(c browser.CookieChange) string {
	return fmt.Sprintf("cookie of %s: %s %s", c.Domain, c.Change, c.Name)
}

// cut shortens a long address.
func cut(address string) string {
	if u, err := url.Parse(address); err == nil && u.RawQuery != "" && len(address) > urlLimit {
		u.RawQuery = ""
		return u.String() + "?…"
	}
	if len(address) > urlLimit {
		return address[:urlLimit] + "…"
	}
	return address
}

// limit makes the lines: the same line twice is one, with a count; and past
// most, requests are left out first, then the rest, with how many.
func limit(list []event, most int) []string {
	type line struct {
		text    string
		times   int
		request bool
	}
	var lines []line
	index := map[string]int{}
	for _, ev := range list {
		if i, ok := index[ev.line]; ok {
			lines[i].times++
			continue
		}
		index[ev.line] = len(lines)
		lines = append(lines, line{ev.line, 1, ev.request})
	}
	keep := make([]bool, len(lines))
	room := most
	for _, request := range []bool{false, true} {
		for i, l := range lines {
			if l.request == request && room > 0 {
				keep[i] = true
				room--
			}
		}
	}
	var out []string
	left := 0
	for i, l := range lines {
		if !keep[i] {
			left++
			continue
		}
		if l.times > 1 {
			l.text += fmt.Sprintf(" (%d times)", l.times)
		}
		out = append(out, l.text)
	}
	if left > 0 {
		out = append(out, fmt.Sprintf("and %d more", left))
	}
	return out
}
