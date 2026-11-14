package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// The agent's own browsing, to find out how a site works before it writes a
// service: one tab, not a service's, that it moves around in as you would,
// as Playwright MCP's tools do. It runs in the agent's browser, a profile of
// its own (backend/tools.go), so nothing it does there changes yours.
//
// The tab it acts in is the newest it opened that is still open, such as a
// sign-in's pop-up, or else the tab itself.

const (
	// snapshotLimit keeps a long page from filling an agent's context.
	snapshotLimit = 25000
	// scriptPatience is how long Evaluate waits for code. Code that never
	// finishes would otherwise hold the tab for good.
	scriptPatience = 30 * time.Second
	// waitPatience is the longest WaitFor waits.
	waitPatience = 30 * time.Second
	// actionSettles is how long after a click or a key Harbor waits for the
	// page to start what it does, such as loading the next page, before it
	// waits for the page to settle.
	actionSettles = 300 * time.Millisecond
	// eventsArrive is how long after an action Harbor waits for Chromium's
	// news of what it did, such as what a script saved.
	eventsArrive = 100 * time.Millisecond
)

// ErrNoPage is the error for an action before the agent has gone anywhere.
var ErrNoPage = errors.New("no page is open: go to one with browser_navigate first")

// browsing is the agent's tab, and the dialog a page there has open.
type browsing struct {
	// turn holds one token; take it to use the tab.
	turn chan struct{}

	// mu guards tab, which a page's dialog reads, and dialog.
	mu     sync.Mutex
	tab    *browser.Tab
	dialog *openDialog
}

// openDialog is a page's alert, confirm, or prompt, waiting for the agent.
type openDialog struct {
	browser.Dialog
	answer chan dialogAnswer
}

type dialogAnswer struct {
	accept bool
	text   string
}

// Page is where the agent's tab is, and what is on it.
type Page struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	// Events are what the action did: the pages it went to, the site's
	// requests, and the cookies and saved values it added, changed, or
	// removed, by name (events.go).
	Events []string `json:"events,omitempty"`
	// Snapshot is the page's accessibility tree, a line an element, with
	// the ref each element is named by.
	Snapshot  string `json:"snapshot,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	// Tabs are the tabs open, when the page opened more than one: actions
	// go to the newest.
	Tabs []string `json:"tabs,omitempty"`
	// Dialog is a dialog the page has open, which must be answered with
	// HandleDialog before anything else.
	Dialog *Dialog `json:"dialog,omitempty"`
}

// Dialog is a page's alert, confirm, prompt, or "leave this page?".
type Dialog struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Default string `json:"default,omitempty"`
}

// take waits for the agent's turn at its tab.
func (b *browsing) take(ctx context.Context) (func(), error) {
	select {
	case <-b.turn:
		return func() { b.turn <- struct{}{} }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// root is the agent's tab, or nil before it opened one, or once it closed.
func (b *browsing) root() *browser.Tab {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tab == nil {
		return nil
	}
	select {
	case <-b.tab.Done():
		return nil
	default:
		return b.tab
	}
}

// current is the tab actions go to, or nil before the agent opened one.
func (b *browsing) current() *browser.Tab {
	root := b.root()
	if root == nil {
		return nil
	}
	if opened := root.Opened(); len(opened) > 0 {
		return opened[len(opened)-1]
	}
	return root
}

// shows reports whether t is the agent's tab, or one it opened.
func (b *browsing) shows(t *browser.Tab) bool {
	root := b.root()
	for ; t != nil && root != nil; t = t.Opener() {
		if t == root {
			return true
		}
	}
	return false
}

// open is the dialog open in the agent's tab, if any.
func (b *browsing) open() *openDialog {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dialog
}

// waitForDialog holds a page's dialog in the agent's tab until the agent
// answers it, or the page gives up on it.
func (b *browsing) waitForDialog(ctx context.Context, d browser.Dialog) (bool, string) {
	o := &openDialog{Dialog: d, answer: make(chan dialogAnswer, 1)}
	b.mu.Lock()
	b.dialog = o
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if b.dialog == o {
			b.dialog = nil
		}
		b.mu.Unlock()
	}()
	select {
	case a := <-o.answer:
		return a.accept, a.text
	case <-ctx.Done():
		return false, ""
	}
}

// in runs fn with the agent's turn and its current tab, after making sure
// no dialog is in the way. A page that opens a dialog stops its scripts until
// it is answered, so nothing can be read from it meanwhile.
func (e *Engine) in(ctx context.Context, fn func(*browser.Tab) error) error {
	done, err := e.browsing.take(ctx)
	if err != nil {
		return err
	}
	defer done()
	tab := e.browsing.current()
	if tab == nil {
		return ErrNoPage
	}
	if d := e.browsing.open(); d != nil {
		return fmt.Errorf("the page has a %s dialog open (%q): answer it with browser_handle_dialog first", d.Kind, d.Message)
	}
	return fn(tab)
}

// tabs are the agent's tab and the tabs it opened.
func (b *browsing) tabs() []*browser.Tab {
	root := b.root()
	if root == nil {
		return nil
	}
	return append([]*browser.Tab{root}, root.Opened()...)
}

// acts runs an action in the agent's tab, as in does, and adds to the page
// it leaves what the action did.
func (e *Engine) acts(ctx context.Context, p *Page, fn func(*browser.Tab) error) error {
	return e.in(ctx, func(tab *browser.Tab) error {
		w, err := e.watchFrom(ctx, e.browsing.tabs())
		if err != nil {
			return err
		}
		if err := fn(tab); err != nil {
			return err
		}
		// Chromium tells of a change to storage after the code that made it
		// returns: without the wait, browser_evaluate's localStorage.setItem
		// came back with no events.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(eventsArrive):
		}
		p.Events, err = e.events(ctx, w, e.browsing.tabs(), eventLimit)
		return err
	})
}

// look is where the tab is, and its snapshot, once the page has settled.
func (e *Engine) look(ctx context.Context, tab *browser.Tab) (Page, error) {
	if err := tab.Settle(ctx); err != nil {
		return Page{}, err
	}
	p := Page{}
	if d := e.browsing.open(); d != nil {
		s := tab.State()
		p.URL, p.Title = s.URL, s.Title
		p.Dialog = &Dialog{Kind: d.Kind, Message: d.Message, Default: d.Default}
	} else {
		s, err := tab.Snapshot(ctx, snapshotLimit)
		if err != nil {
			return Page{}, err
		}
		p.URL, p.Title, p.Snapshot, p.Truncated = s.URL, s.Title, s.Text, s.Truncated
	}
	if root := e.browsing.root(); root != nil && len(root.Opened()) > 0 {
		for _, t := range append([]*browser.Tab{root}, root.Opened()...) {
			p.Tabs = append(p.Tabs, t.State().URL)
		}
	}
	return p, nil
}

// Navigate goes to address, a full URL, in the agent's tab, opening it the
// first time.
func (e *Engine) Navigate(ctx context.Context, address string) (Page, error) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return Page{}, fmt.Errorf("%q is not a full URL, such as https://example.com/inbox", address)
	}
	done, err := e.browsing.take(ctx)
	if err != nil {
		return Page{}, err
	}
	defer done()
	if e.browsing.open() != nil {
		return Page{}, errors.New("the page has a dialog open: answer it with browser_handle_dialog first")
	}
	w, err := e.watchFrom(ctx, e.browsing.tabs())
	if err != nil {
		return Page{}, err
	}
	tab := e.browsing.current()
	if tab == nil {
		if tab, err = e.browser.Open(ctx, address); err != nil {
			return Page{}, err
		}
		e.browsing.mu.Lock()
		e.browsing.tab = tab
		e.browsing.mu.Unlock()
	} else if err := tab.Navigate(ctx, address); err != nil {
		return Page{}, err
	}
	p, err := e.look(ctx, tab)
	if err != nil {
		return Page{}, err
	}
	p.Events, err = e.events(ctx, w, e.browsing.tabs(), eventLimit)
	return p, err
}

// Back goes back a page.
func (e *Engine) Back(ctx context.Context) (Page, error) {
	var p Page
	err := e.acts(ctx, &p, func(tab *browser.Tab) error {
		if err := tab.Back(ctx); err != nil {
			return err
		}
		return e.after(ctx, tab, &p)
	})
	return p, err
}

// Snapshot is the page the agent's tab shows.
func (e *Engine) Snapshot(ctx context.Context) (Page, error) {
	var p Page
	err := e.in(ctx, func(tab *browser.Tab) error {
		var err error
		p, err = e.look(ctx, tab)
		return err
	})
	return p, err
}

// after waits for what an action started, and looks at the page it left.
func (e *Engine) after(ctx context.Context, tab *browser.Tab, p *Page) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(actionSettles):
	}
	// A click can open a tab, which actions then go to.
	if now := e.browsing.current(); now != nil {
		tab = now
	}
	var err error
	*p, err = e.look(ctx, tab)
	return err
}

// Click clicks the element, or double-clicks it.
func (e *Engine) Click(ctx context.Context, ref string, double bool) (Page, error) {
	var p Page
	err := e.acts(ctx, &p, func(tab *browser.Tab) error {
		click := tab.Click
		if double {
			click = tab.DoubleClick
		}
		if err := click(ctx, ref); err != nil {
			return err
		}
		return e.after(ctx, tab, &p)
	})
	return p, err
}

// Type types text into a field, in place of what it had, and presses Enter
// after if submit is set.
func (e *Engine) Type(ctx context.Context, ref, text string, submit bool) (Page, error) {
	var p Page
	err := e.acts(ctx, &p, func(tab *browser.Tab) error {
		if err := tab.Type(ctx, ref, text); err != nil {
			return err
		}
		if submit {
			if err := tab.Press(ctx, "Enter"); err != nil {
				return err
			}
		}
		return e.after(ctx, tab, &p)
	})
	return p, err
}

// SelectOption picks options in a <select>.
func (e *Engine) SelectOption(ctx context.Context, ref string, values []string) (Page, error) {
	var p Page
	err := e.acts(ctx, &p, func(tab *browser.Tab) error {
		if err := tab.Select(ctx, ref, values...); err != nil {
			return err
		}
		return e.after(ctx, tab, &p)
	})
	return p, err
}

// Field is one field FillForm fills in.
type Field struct {
	Ref string `json:"ref"`
	// Type is textbox, checkbox, radio, combobox, or slider.
	Type string `json:"type"`
	// Value is the text, "true" or "false" for a checkbox or a radio, or an
	// option for a combobox.
	Value string `json:"value"`
}

// FillForm fills in several fields.
func (e *Engine) FillForm(ctx context.Context, fields []Field) (Page, error) {
	var p Page
	err := e.acts(ctx, &p, func(tab *browser.Tab) error {
		for _, f := range fields {
			var err error
			switch f.Type {
			case "textbox", "slider":
				err = tab.Type(ctx, f.Ref, f.Value)
			case "checkbox", "radio":
				err = tab.Check(ctx, f.Ref, f.Value == "true")
			case "combobox":
				err = tab.Select(ctx, f.Ref, f.Value)
			default:
				err = fmt.Errorf("%q is not a field type; use textbox, checkbox, radio, combobox, or slider", f.Type)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", f.Ref, err)
			}
		}
		return e.after(ctx, tab, &p)
	})
	return p, err
}

// PressKey presses a key, such as Enter, ArrowDown, a, or Control+a.
func (e *Engine) PressKey(ctx context.Context, name string) (Page, error) {
	var p Page
	err := e.acts(ctx, &p, func(tab *browser.Tab) error {
		if err := tab.Press(ctx, name); err != nil {
			return err
		}
		return e.after(ctx, tab, &p)
	})
	return p, err
}

// WaitFor waits until text is on the page, or gone from it, or for
// seconds, up to 30 seconds.
func (e *Engine) WaitFor(ctx context.Context, text, gone string, seconds float64) (Page, error) {
	if text == "" && gone == "" && seconds <= 0 {
		return Page{}, errors.New("say what to wait for: text, textGone, or time")
	}
	var p Page
	err := e.acts(ctx, &p, func(tab *browser.Tab) error {
		ctx, cancel := context.WithTimeout(ctx, waitPatience)
		defer cancel()
		if seconds > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("waited %s, the longest it waits", waitPatience)
			case <-time.After(time.Duration(seconds * float64(time.Second))):
			}
		}
		for text != "" || gone != "" {
			raw, err := tab.Evaluate(ctx, `(text, gone) => {
				const all = document.body ? document.body.innerText : "";
				return (!text || all.includes(text)) && (!gone || !all.includes(gone));
			}`, text, gone)
			if err == nil && string(raw) == "true" {
				break
			}
			if e.browsing.open() != nil {
				break
			}
			select {
			case <-ctx.Done():
				if text != "" {
					return fmt.Errorf("%q was not on the page within %s", text, waitPatience)
				}
				return fmt.Errorf("%q was still on the page after %s", gone, waitPatience)
			case <-time.After(250 * time.Millisecond):
			}
		}
		var err error
		p, err = e.look(ctx, tab)
		return err
	})
	return p, err
}

// HandleDialog answers the dialog the page has open: accept it, or not,
// and for a prompt, with text.
func (e *Engine) HandleDialog(ctx context.Context, accept bool, text string) (Page, error) {
	done, err := e.browsing.take(ctx)
	if err != nil {
		return Page{}, err
	}
	defer done()
	d := e.browsing.open()
	if d == nil {
		return Page{}, errors.New("the page has no dialog open")
	}
	w, err := e.watchFrom(ctx, e.browsing.tabs())
	if err != nil {
		return Page{}, err
	}
	d.answer <- dialogAnswer{accept, text}
	for e.browsing.open() == d {
		select {
		case <-ctx.Done():
			return Page{}, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	var p Page
	tab := e.browsing.current()
	if tab == nil {
		return Page{}, ErrNoPage
	}
	if err := e.after(ctx, tab, &p); err != nil {
		return Page{}, err
	}
	p.Events, err = e.events(ctx, w, e.browsing.tabs(), eventLimit)
	return p, err
}

// Evaluate calls fn, the source of a JavaScript function, in the page: with
// the element, if ref names one, and with the context object a tool's run
// gets ({ cookies }), last. It returns what fn returns, as JSON, and what
// the code did, as an action's events.
func (e *Engine) Evaluate(ctx context.Context, fn, ref string) (json.RawMessage, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, scriptPatience)
	defer cancel()
	var out json.RawMessage
	var p Page
	err := e.acts(ctx, &p, func(tab *browser.Tab) error {
		jsCtx, err := siteContext(ctx, tab, tab.State().URL, time.Now())
		if err != nil {
			return err
		}
		if ref == "" {
			out, err = tab.Evaluate(ctx, fn, jsCtx)
		} else {
			out, err = tab.EvaluateOn(ctx, ref, fn, jsCtx)
		}
		return err
	})
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, nil, fmt.Errorf("the code did not finish within %s", scriptPatience)
	}
	return out, p.Events, err
}

// Screenshot is a JPEG of the agent's tab: its window, or the whole page.
func (e *Engine) Screenshot(ctx context.Context, full bool) ([]byte, error) {
	var shot []byte
	err := e.in(ctx, func(tab *browser.Tab) error {
		var err error
		shot, err = tab.Screenshot(ctx, full)
		return err
	})
	return shot, err
}

// Requests are the requests the agent's tab made, on this page and the
// pages before it, the newest thousand.
func (e *Engine) Requests(ctx context.Context) ([]browser.Request, error) {
	var out []browser.Request
	err := e.in(ctx, func(tab *browser.Tab) error {
		out = tab.Requests()
		return nil
	})
	return out, err
}

// RequestDetail is one request, with its bodies.
type RequestDetail struct {
	browser.Request
	RequestBody  string `json:"requestBody,omitempty"`
	ResponseBody string `json:"responseBody,omitempty"`
	// Truncated says a body was cut to bodyLimit.
	Truncated bool `json:"truncated,omitempty"`
}

// bodyLimit keeps a large response from filling an agent's context.
const bodyLimit = 20000

// Request is one of the page's requests, by its ID, with what it sent and
// what came back. A body that is not text is described, not given.
func (e *Engine) Request(ctx context.Context, id string) (RequestDetail, error) {
	var d RequestDetail
	err := e.in(ctx, func(tab *browser.Tab) error {
		found := false
		for _, r := range tab.Requests() {
			if r.ID == id {
				d.Request, found = r, true
			}
		}
		if !found {
			return fmt.Errorf("the tab made no request %s, or it is gone from the newest thousand: list them again", id)
		}
		body := func(data []byte, kind string) string {
			if !textual(data) {
				return fmt.Sprintf("(%d bytes of %s)", len(data), kind)
			}
			if len(data) > bodyLimit {
				d.Truncated = true
				return string(data[:bodyLimit])
			}
			return string(data)
		}
		if d.HasBody {
			if data, err := tab.RequestBody(ctx, id); err == nil {
				d.RequestBody = body(data, "data")
			}
		}
		// A redirect's body is never kept.
		if d.Status != 0 && (d.Status < 300 || d.Status >= 400) {
			data, err := tab.ResponseBody(ctx, id)
			if err != nil {
				d.ResponseBody = "(Chromium no longer has it: " + err.Error() + ")"
			} else {
				d.ResponseBody = body(data, d.MimeType)
			}
		}
		return nil
	})
	return d, err
}

// textual reports whether data reads as text.
func textual(data []byte) bool {
	return utf8.Valid(data) && !bytes.ContainsRune(data, 0)
}

// ClearData removes what the agent's browser keeps for the site at origin,
// or everything, if origin is empty (Slipway's browser.ClearData). Services'
// tabs close, so their next call starts with a page that loads afresh.
func (e *Engine) ClearData(ctx context.Context, origin string) error {
	if err := e.browser.ClearData(ctx, origin); err != nil {
		return err
	}
	e.mu.Lock()
	sessions := make([]*session, 0, len(e.sessions))
	for _, s := range e.sessions {
		sessions = append(sessions, s)
	}
	e.mu.Unlock()
	for _, s := range sessions {
		done, err := s.take(ctx)
		if err != nil {
			return err
		}
		if s.tab != nil {
			s.tab.Close()
			s.tab = nil
		}
		done()
	}
	return nil
}

// SiteView starts the name of the live view WatchSignIn shows: "site:" and
// the site's host. A service's name has no colon, so the two never meet.
const SiteView = "site:"

// SignInWatched is what WatchSignIn saw: where the agent's tab ended, and
// what the sign-in did.
type SignInWatched struct {
	URL    string   `json:"url"`
	Title  string   `json:"title"`
	Events []string `json:"events"`
}

// WatchSignIn shows you the agent's tab at site, a full URL, in Harbor's
// window, to sign in there, until you click Done, or five minutes pass. It
// returns what the sign-in did: the pages it went through, the requests,
// and the cookies and saved values it added, by name. Before it, the agent
// had to write a service's login.check to see a sign-in at all, since only
// the check could say when one was done: a wrong first guess refused the
// sign-in, or kept it open for good, and one model, unable to sign in,
// typed admin and admin into the sign-in page.
func (e *Engine) WatchSignIn(ctx context.Context, site string) (SignInWatched, error) {
	u, err := url.Parse(site)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return SignInWatched{}, fmt.Errorf("%q is not a full URL, such as https://example.com", site)
	}
	done, err := e.browsing.take(ctx)
	if err != nil {
		return SignInWatched{}, err
	}
	defer done()
	if e.browsing.open() != nil {
		return SignInWatched{}, errors.New("the page has a dialog open: answer it with browser_handle_dialog first")
	}
	w, err := e.watchFrom(ctx, e.browsing.tabs())
	if err != nil {
		return SignInWatched{}, err
	}
	tab := e.browsing.root()
	if tab == nil {
		if tab, err = e.browser.Open(ctx, site); err != nil {
			return SignInWatched{}, err
		}
		e.browsing.mu.Lock()
		e.browsing.tab = tab
		e.browsing.mu.Unlock()
	} else if err := tab.Navigate(ctx, site); err != nil {
		return SignInWatched{}, err
	}
	svc := Service{Manifest: service.Manifest{Name: SiteView + u.Host, Title: u.Host, URL: site}}
	v, err := e.live(ctx, tab, svc, "Sign in to "+u.Host+", then click Done", true, func(ctx context.Context, _ *view) error {
		select {
		case <-tab.Done():
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil && !(v != nil && v.ended()) {
		return SignInWatched{}, err
	}
	r := SignInWatched{}
	if tab = e.browsing.current(); tab == nil {
		return SignInWatched{}, ErrNoPage
	}
	if err := tab.Settle(ctx); err != nil {
		return SignInWatched{}, err
	}
	state := tab.State()
	r.URL, r.Title = state.URL, state.Title
	r.Events, err = e.events(ctx, w, e.browsing.tabs(), signInEventLimit)
	return r, err
}
