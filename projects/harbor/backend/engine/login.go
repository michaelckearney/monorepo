package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// Signing in, to a service and, for a tool that needs more, again before it
// runs, as a bank asks for your password again before a transfer. Both are
// a login: a page (url), a check that says whether you are signed in, and
// maybe a run that signs in on its own. Harbor checks; if the check says no,
// it runs login.run, page by page; if that does not work, it shows you the
// page in a live view (view.go) until the check passes.

// autoPages is how many pages login.run may go through, such as a username
// page, a password page, and a code page, before Harbor gives up on it.
const autoPages = 5

// Window shows Harbor's window the live views: while one is open, the window
// shows it.
type Window interface {
	// Open shows a live view of the service's tab, by its ID, headed
	// heading. browse is set for one you opened to look around, which ends
	// when you are done, rather than when a check passes. close takes it
	// away.
	Open(service, tab, heading string, browse bool) (close func(), err error)
	// Prompt shows p beside the live view of the service, in place of the
	// prompt it showed, or shows none for a nil p.
	Prompt(service string, p *Prompt) error
}

// gate is one login: the service's, or a tool's.
type gate struct {
	login *service.Login
	// tool is the tool's name, for a tool's login.
	tool string
	// path is where the login's functions are in the service, such as
	// "login" or "tools.transfer.login".
	path string
	// heading is the live view's, such as "Sign in to Tidewater Bank".
	heading string
	// needed is the error when you are not signed in.
	needed string
}

func serviceGate(svc Service) gate {
	t := svc.Manifest.Title
	return gate{login: svc.Manifest.Login, path: "login", heading: "Sign in to " + t,
		needed: "sign in to " + t + " first"}
}

// signInNeeded is the error for the gate when you are not through it.
func (g gate) signInNeeded(svc Service) *SignInNeeded {
	return &SignInNeeded{Service: svc.Manifest.Name, Tool: g.tool, Heading: g.heading, Message: g.needed}
}

// NotSignedIn is what Call's error says when you are signed out of the
// service: what SignedIn's false means.
func NotSignedIn(svc Service) *SignInNeeded { return serviceGate(svc).signInNeeded(svc) }

func toolGate(svc Service, tool service.Tool) gate {
	t := svc.Manifest.Title
	return gate{login: tool.Login, tool: tool.Name, path: "tools." + tool.Name + ".login",
		heading: fmt.Sprintf("%s wants you to confirm before %s", t, tool.Name),
		needed:  fmt.Sprintf("%s wants you to confirm it's you before %s", t, tool.Name)}
}

// gatesFor is the logins to get through before tool runs: the service's,
// then the tool's.
func gatesFor(svc Service, tool service.Tool) []gate {
	var gates []gate
	if svc.Manifest.Login != nil {
		gates = append(gates, serviceGate(svc))
	}
	if tool.Login != nil {
		gates = append(gates, toolGate(svc, tool))
	}
	return gates
}

// through gets you through each gate in turn, or returns the
// *SignInNeeded for the first you are not through. wait is pass's.
func (e *Engine) through(ctx context.Context, tab *browser.Tab, svc Service, c compiled, gates []gate, wait bool) error {
	for _, g := range gates {
		ok, err := e.pass(ctx, tab, svc, c, g, wait)
		if err != nil {
			return err
		}
		if !ok {
			return g.signInNeeded(svc)
		}
	}
	return nil
}

// check runs the login's check once.
func (g gate) check(ctx context.Context, tab *browser.Tab, svc Service, c compiled) (bool, error) {
	jsCtx, err := pageContext(ctx, tab, svc, time.Now())
	if err != nil {
		return false, err
	}
	raw, err := invoke(ctx, tab, c, g.path+".check", jsCtx)
	if err != nil {
		return false, err
	}
	var ok bool
	if err := json.Unmarshal(raw, &ok); err != nil {
		return false, fmt.Errorf("%s's %s.check returned %s; it returns true or false", svc.Manifest.Name, g.path, short(raw))
	}
	return ok, nil
}

// ErrCannotTell is wrapped by the error when a login check threw for as
// long as Harbor waits for it.
var ErrCannotTell = errors.New("could not tell whether you are signed in")

// waitForCheck runs the check until it answers rather than throws, for up
// to checkPatience.
func (g gate) waitForCheck(ctx context.Context, tab *browser.Tab, svc Service, c compiled) (bool, error) {
	deadline := time.Now().Add(checkPatience)
	for {
		ok, err := g.check(ctx, tab, svc, c)
		var jsErr *browser.JSError
		if err == nil || !errors.As(err, &jsErr) || time.Now().After(deadline) {
			if err != nil {
				return false, fmt.Errorf("%w to %s: %w", ErrCannotTell, svc.Manifest.Title, err)
			}
			return ok, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(checkEvery):
		}
	}
}

// pass gets you through the login: it checks, runs login.run if there is
// one, and then, if wait is set, shows you the login page, until the check
// passes. It returns false if you cancel, or wait five minutes, or if it
// would have to show you the page and wait is not set. The caller has the
// service's turn.
func (e *Engine) pass(ctx context.Context, tab *browser.Tab, svc Service, c compiled, g gate, wait bool) (bool, error) {
	ok, err := g.waitForCheck(ctx, tab, svc, c)
	if err != nil || ok {
		return ok, err
	}
	if g.login.Run {
		ok, err := e.auto(ctx, tab, svc, c, g)
		if err != nil || ok || !wait {
			return ok, err
		}
		// You carry on from the page login.run got to, such as the page
		// for a code when Harbor has no authenticator secret.
		return e.show(ctx, tab, svc, c, g, false)
	}
	if !wait {
		return false, nil
	}
	return e.show(ctx, tab, svc, c, g, true)
}

// SignInTest is the service's page after a sign-in an agent tested: what a
// tool would run on. Told only whether the check passed, a model whose
// check was wrong could not tell: on a single-page app, a check that
// fetches a page reads the bare page, not what the browser shows, and one
// model spent a trial's 20 minutes on a sign-in page its check said was
// signed in, and another signed in with a check that never said so.
type SignInTest struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Text  string `json:"text"`
	// Events are what the sign-in did, as a browser action's are: the pages
	// it went through, its requests, and the cookies and saved values it
	// added, by name. They show where the site keeps its sign-in, for
	// login.check and the tools to read.
	Events []string `json:"events,omitempty"`
}

// pageTextLength is how much of the page's text a SignInTest carries:
// enough to tell a sign-in page from the site.
const pageTextLength = 500

// ErrCheckWithNothingKept is the error when a login check says you are
// signed in to a site the browser keeps nothing for: it can never say no.
var ErrCheckWithNothingKept = errors.New("login.check said signed in with nothing kept for the site, its cookies and storage cleared, so Harbor signed nothing in: the check is wrong, and must say not signed in here")

// TestSignIn tests the service's login from the start, in the agent's
// browser: it clears what the browser keeps for the site, so a check that
// works says no, then gets you through the login, as SignIn does. It
// returns the service's page, loaded once the sign-in has ended, however it
// ended, beside any error.
func (e *Engine) TestSignIn(ctx context.Context, svc Service) (SignInTest, error) {
	if svc.Manifest.Login != nil {
		if err := e.ClearData(ctx, svc.Manifest.URL); err != nil {
			return SignInTest{}, err
		}
	}
	var r SignInTest
	var signInErr error
	err := e.use(ctx, svc, func(tab *browser.Tab, c compiled) error {
		tabs := func() []*browser.Tab { return append([]*browser.Tab{tab}, tab.Opened()...) }
		w, err := e.watchFrom(ctx, tabs())
		if err != nil {
			return err
		}
		if svc.Manifest.Login != nil {
			g := serviceGate(svc)
			ok, err := g.waitForCheck(ctx, tab, svc, c)
			if err != nil {
				return err
			}
			if ok {
				signInErr = ErrCheckWithNothingKept
			} else if ok, err = e.pass(ctx, tab, svc, c, g, true); err != nil {
				return err
			} else if !ok {
				signInErr = g.signInNeeded(svc)
			}
		}
		if err := tab.Navigate(ctx, svc.Manifest.URL); err != nil {
			return err
		}
		if err := tab.Settle(ctx); err != nil {
			return err
		}
		raw, err := tab.Evaluate(ctx, `(n) => ({ url: location.href, title: document.title, text: (document.body?.innerText ?? "").replace(/\s+/g, " ").trim().slice(0, n) })`, pageTextLength)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return err
		}
		r.Events, err = e.events(ctx, w, tabs(), signInEventLimit)
		return err
	})
	if err == nil {
		err = signInErr
	}
	return r, err
}

// auto runs login.run on each page of the login, until the check passes. It
// returns false if the check has not passed after autoPages pages, or if
// login.run throws, as it does when Harbor has no saved login to fill in.
func (e *Engine) auto(ctx context.Context, tab *browser.Tab, svc Service, c compiled, g gate) (bool, error) {
	// For as long as this runs, and no longer, the page may have Harbor
	// fill in a saved login, or use a passkey without asking: a page that
	// clicks its passkey button asks for the passkey after run returns.
	s := e.session(svc.Manifest.Name)
	s.signingIn.Store(tab)
	defer s.signingIn.Store(nil)
	if err := tab.Navigate(ctx, siteURL(svc.Manifest, g.login.URL)); err != nil {
		return false, err
	}
	for range autoPages {
		if err := tab.Settle(ctx); err != nil {
			return false, err
		}
		if ok, err := g.check(ctx, tab, svc, c); err == nil && ok {
			return true, tab.Navigate(ctx, svc.Manifest.URL)
		}
		jsCtx, err := pageContext(ctx, tab, svc, time.Now())
		if err != nil {
			return false, err
		}
		_, err = invoke(ctx, tab, c, g.path+".run", jsCtx)
		var jsErr *browser.JSError
		if errors.As(err, &jsErr) {
			// Not signed in by itself; you sign in instead.
			return false, nil
		}
		if err != nil && ctx.Err() != nil {
			return false, ctx.Err()
		}
		// Anything else is the page going away under run, as a form it
		// submitted loads the next page. Give the next page a moment to
		// start loading before Settle waits for it to finish.
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	if err := tab.Settle(ctx); err != nil {
		return false, err
	}
	ok, err := g.check(ctx, tab, svc, c)
	if err == nil && ok {
		return true, tab.Navigate(ctx, svc.Manifest.URL)
	}
	return false, nil
}

// show shows the service's tab as a live view, first opening the login
// page if open is set, until the check passes, you cancel, the page closes
// the tab, or five minutes pass. The tab then goes back to the service's
// URL, and Harbor offers to save the login you typed.
func (e *Engine) show(ctx context.Context, tab *browser.Tab, svc Service, c compiled, g gate, open bool) (bool, error) {
	signedIn := false
	v, err := e.live(ctx, tab, svc, g.heading, false, func(ctx context.Context, v *view) error {
		if open {
			if err := tab.Navigate(ctx, siteURL(svc.Manifest, g.login.URL)); err != nil {
				return err
			}
		}
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			// While you sign in, the page navigates and the check throws or
			// says no; both mean not yet.
			if ok, err := g.check(ctx, tab, svc, c); err == nil && ok {
				signedIn = true
				// Where a new tab for the service would start.
				if err := tab.Navigate(ctx, svc.Manifest.URL); err != nil {
					return err
				}
				e.offerToSave(ctx, v)
				return nil
			}
			select {
			case <-tab.Done():
				return nil
			case <-ctx.Done():
				return ctx.Err()
			case <-tick.C:
			}
		}
	})
	if err != nil && v != nil && v.ended() {
		return false, nil
	}
	return signedIn, err
}

// live shows the service's tab as a live view, headed heading, while fn
// runs, for up to five minutes, or until you close it. v.ended reports
// whether you closed it, or the five minutes passed.
func (e *Engine) live(ctx context.Context, tab *browser.Tab, svc Service, heading string, browse bool, fn func(context.Context, *view) error) (*view, error) {
	name := svc.Manifest.Name
	e.mu.Lock()
	if e.views[name] != nil {
		e.mu.Unlock()
		return nil, fmt.Errorf("%s is already shown in Harbor's window", svc.Manifest.Title)
	}
	ctx, cancel := context.WithTimeout(ctx, signInPatience)
	defer cancel()
	// The view comes first: a login page that asks for a password over
	// HTTP, or a passkey, does not finish loading until you answer in it.
	v := newView(ctx, svc, tab, cancel, func(p *Prompt) { _ = e.window.Prompt(name, p) })
	e.views[name] = v
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.views, name)
		e.mu.Unlock()
	}()
	closeWindow, err := e.window.Open(name, tab.ID(), heading, browse)
	if err != nil {
		return nil, err
	}
	defer closeWindow()
	// The tabs it opened go with it, as a sign-in's pop-up does.
	defer func() {
		for _, t := range tab.Opened() {
			t.Close()
		}
	}()
	return v, fn(ctx, v)
}

// Browse shows the service's tab in Harbor's window, where it is, until you
// close it, or five minutes pass: to look around, or to do what Harbor
// cannot do for you, such as add a passkey to your account.
func (e *Engine) Browse(ctx context.Context, svc Service) error {
	return e.use(ctx, svc, func(tab *browser.Tab, _ compiled) error {
		v, err := e.live(ctx, tab, svc, svc.Manifest.Title, true, func(ctx context.Context, _ *view) error {
			select {
			case <-tab.Done():
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		if err != nil && v != nil && v.ended() {
			return nil
		}
		return err
	})
}

// siteURL is path, which may have a query, on the service's site.
func siteURL(m service.Manifest, path string) string {
	base, err := url.Parse(m.URL)
	if err != nil {
		return m.URL
	}
	to, err := base.Parse(path)
	if err != nil {
		return m.URL
	}
	return to.String()
}
