// Package engine runs services' tools in Harbor's browser, signed in as you.
// Dashboards, and later MCP clients and the agent, all go through Call.
//
// It is built in layers. Slipway's package browser is Chromium and its tabs.
// A session is one service's tab, with the service's compiled file loaded
// into the page (session.go). Call, SignedIn, and SignIn put those together.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/framework/browser/securitykey"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/fixture"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// Service is what the engine needs of a saved service.
type Service struct {
	Manifest service.Manifest
	Source   string
}

// ErrSignInNeeded is wrapped by Call's error when the service's login check
// says you are signed out.
var ErrSignInNeeded = errors.New("sign in needed")

// SignInNeeded is the error when you are not through a login: the
// service's, or the tool's, if Tool is set. Harbor's window reads its
// fields, to offer you the sign-in.
type SignInNeeded struct {
	Service string `json:"service"`
	Tool    string `json:"tool,omitempty"`
	// Heading says what you are to do, such as "Sign in to Tidewater Bank".
	Heading string `json:"heading"`
	Message string `json:"message"`
}

func (e *SignInNeeded) Error() string { return ErrSignInNeeded.Error() + ": " + e.Message }
func (e *SignInNeeded) Unwrap() error { return ErrSignInNeeded }

// How long the engine waits on a site.
const (
	// checkPatience is how long a login check may keep throwing, which it
	// does while it cannot tell yet, before Call gives up.
	checkPatience = 10 * time.Second
	checkEvery    = 500 * time.Millisecond
	// signInPatience is how long a sign-in waits for you.
	signInPatience = 5 * time.Minute
	// openAtOnce is how many services' tabs OpenTabs loads at a time.
	openAtOnce = 3
)

// Engine owns Harbor's browser and one session for each service in use.
type Engine struct {
	browser *browser.Browser
	secrets Secrets
	window  Window
	files   Files

	// SecurityKeys finds the security keys plugged in. Tests set a key in
	// software (securitykey/softkey).
	SecurityKeys func() ([]securitykey.Device, error)

	mu       sync.Mutex
	sessions map[string]*session
	// views are the live views shown, by service.
	views map[string]*view
	// uploads are the folders of the files you gave pages (pages.go).
	uploads []string

	// browsing is the agent's own tab (browse.go).
	browsing browsing
}

// New returns an engine whose browser keeps its profile in dir, or in
// Slipway's default for an empty dir, keeps the passwords and passkeys you
// sign in with in secrets (prompt.go), shows its live views in window
// (login.go), and keeps the files tools take and return in files
// (files.go). Chromium starts on the first call.
func New(dir string, secrets Secrets, window Window, files Files) *Engine {
	e := &Engine{secrets: secrets, window: window, files: files, SecurityKeys: securitykey.Devices,
		sessions: map[string]*session{}, views: map[string]*view{}}
	e.browsing.turn = make(chan struct{}, 1)
	e.browsing.turn <- struct{}{}
	a := asker{e}
	// The fixture sites, while this process serves them, as in dev mode.
	hostRules, cas := fixture.Browser()
	e.browser = browser.Open(browser.Options{
		Dir:         dir,
		HTTPAuth:    a.password,
		Passkey:     a.passkey,
		Dialog:      a.dialog,
		ChooseFiles: a.chooseFiles,
		Downloaded:  a.downloaded,
		HostRules:   hostRules,
		TrustedCAs:  cas,
	})
	return e
}

// Close stops Chromium, and removes the files you gave pages.
func (e *Engine) Close() {
	e.browser.Close()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, dir := range e.uploads {
		os.RemoveAll(dir)
	}
	e.uploads = nil
}

func (e *Engine) session(name string) *session {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.sessions[name]
	if s == nil {
		s = newSession()
		e.sessions[name] = s
	}
	return s
}

// use runs fn with the service's turn taken, and its tab open and settled,
// with the compiled service ready to load.
func (e *Engine) use(ctx context.Context, svc Service, fn func(*browser.Tab, compiled) error) error {
	c, err := compile(svc)
	if err != nil {
		return err
	}
	s := e.session(svc.Manifest.Name)
	done, err := s.take(ctx)
	if err != nil {
		return err
	}
	defer done()
	tab, err := e.tabFor(ctx, s, svc)
	if err != nil {
		return err
	}
	if err := tab.Settle(ctx); err != nil {
		return err
	}
	return fn(tab, c)
}

// Call runs a tool with input, a JSON object, and returns its output. The
// input and the output are checked against the tool's schemas.
//
// First you must be through the service's login, and the tool's, if it has
// one: Harbor signs in on its own if it can, or shows you the login page
// and waits for you (login.go). If you do not sign in, the error is a
// *SignInNeeded. If the tool throws, and a login's check now says no,
// Harbor gets you through it, and runs the tool once more: some sites ask
// only once you try, such as for a large transfer.
func (e *Engine) Call(ctx context.Context, svc Service, tool string, input json.RawMessage) (json.RawMessage, error) {
	return e.call(ctx, svc, tool, input, true)
}

// CallWithoutWaiting is Call, but if Harbor cannot sign in on its own, it
// returns the *SignInNeeded at once, rather than show you the login page.
// A dashboard calls this way, and offers you the sign-in instead: many
// calls each waiting for you would hold every request Harbor's window may
// have open, and your clicks in the sign-in could not get through.
func (e *Engine) CallWithoutWaiting(ctx context.Context, svc Service, tool string, input json.RawMessage) (json.RawMessage, error) {
	return e.call(ctx, svc, tool, input, false)
}

func (e *Engine) call(ctx context.Context, svc Service, tool string, input json.RawMessage, wait bool) (json.RawMessage, error) {
	t, ok := findTool(svc.Manifest, tool)
	if !ok {
		return nil, noTool(svc.Manifest, tool)
	}
	if len(bytes.TrimSpace(input)) == 0 {
		input = json.RawMessage("{}")
	}
	if err := validate(t.InputSchema, input); err != nil {
		return nil, fmt.Errorf("the input does not match %s's inputSchema: %w", tool, err)
	}
	args, err := e.filesIn(t.InputSchema, input)
	if err != nil {
		return nil, err
	}
	gates := gatesFor(svc, t)
	var out json.RawMessage
	err = e.use(ctx, svc, func(tab *browser.Tab, c compiled) error {
		run := func() error {
			in, site := tab, svc.Manifest.URL
			if t.Frame != nil {
				var err error
				if in, err = e.frame(ctx, tab, svc, c, t, input); err != nil {
					return err
				}
				site = t.Frame.Site
			}
			jsCtx, err := siteContext(ctx, in, site, time.Now())
			if err != nil {
				return err
			}
			value, files, err := invokeWithFiles(ctx, in, c, "tools."+tool+".run", args, jsCtx)
			if err != nil {
				return err
			}
			out, err = e.filesOut(value, files)
			return err
		}
		if err := e.through(ctx, tab, svc, c, gates, wait); err != nil {
			return err
		}
		err := run()
		var jsErr *browser.JSError
		if !errors.As(err, &jsErr) {
			return err
		}
		for _, g := range gates {
			if ok, checkErr := g.waitForCheck(ctx, tab, svc, c); checkErr == nil && !ok {
				if err := e.through(ctx, tab, svc, c, gates, wait); err != nil {
					return err
				}
				return run()
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(t.OutputSchema) > 0 {
		if err := validate(t.OutputSchema, out); err != nil {
			return nil, fmt.Errorf("%s returned something that does not match its outputSchema: %w", tool, err)
		}
	} else if !isObject(out) {
		return nil, fmt.Errorf("%s returned %s; a tool returns an object", tool, short(out))
	}
	return out, nil
}

// SignedIn runs the service's login check. A service with no login is
// always signed in.
func (e *Engine) SignedIn(ctx context.Context, svc Service) (bool, error) {
	if svc.Manifest.Login == nil {
		return true, nil
	}
	var signedIn bool
	err := e.use(ctx, svc, func(tab *browser.Tab, c compiled) error {
		var err error
		signedIn, err = serviceGate(svc).waitForCheck(ctx, tab, svc, c)
		return err
	})
	return signedIn, err
}

// OpenTabs opens each service's tab, a few at a time, so its page has
// loaded and settled before its first call. A tab that does not open is
// opened on the service's next call.
func (e *Engine) OpenTabs(ctx context.Context, svcs []Service) {
	turns := make(chan struct{}, openAtOnce)
	var wg sync.WaitGroup
	for _, svc := range svcs {
		turns <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-turns }()
			_ = e.use(ctx, svc, func(*browser.Tab, compiled) error { return nil })
		}()
	}
	wg.Wait()
}

// SignIn gets you through the service's login (login.go), and the tool's,
// if tool is named and has one: it signs in on its own if it can, or shows
// you the login page until you have signed in. It returns false if you
// cancel, or wait five minutes. Calls to the service wait meanwhile; calls
// to others do not.
func (e *Engine) SignIn(ctx context.Context, svc Service, tool string) (bool, error) {
	gates := gatesFor(svc, service.Tool{})
	if tool != "" {
		t, ok := findTool(svc.Manifest, tool)
		if !ok {
			return false, noTool(svc.Manifest, tool)
		}
		gates = gatesFor(svc, t)
	}
	if len(gates) == 0 {
		return true, nil
	}
	err := e.use(ctx, svc, func(tab *browser.Tab, c compiled) error {
		return e.through(ctx, tab, svc, c, gates, true)
	})
	if errors.Is(err, ErrSignInNeeded) {
		return false, nil
	}
	return err == nil, err
}

// noTool says which tools the service does have: an agent told only that a
// tool was missing went on calling it, not seeing its last save had dropped
// it.
func noTool(m service.Manifest, tool string) error {
	if len(m.Tools) == 0 {
		return fmt.Errorf("%s has no tool %s: it has no tools", m.Name, tool)
	}
	names := make([]string, len(m.Tools))
	for i, t := range m.Tools {
		names[i] = t.Name
	}
	return fmt.Errorf("%s has no tool %s. Its tools: %s", m.Name, tool, strings.Join(names, ", "))
}

func findTool(m service.Manifest, name string) (service.Tool, bool) {
	for _, t := range m.Tools {
		if t.Name == name {
			return t, true
		}
	}
	return service.Tool{}, false
}

// validate checks value against a JSON Schema.
func validate(schema, value json.RawMessage) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("schema.json", doc); err != nil {
		return err
	}
	sch, err := c.Compile("schema.json")
	if err != nil {
		return err
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(value))
	if err != nil {
		return fmt.Errorf("it is not JSON: %w", err)
	}
	if err := sch.Validate(v); err != nil {
		var verr *jsonschema.ValidationError
		if errors.As(err, &verr) {
			return errors.New(schemaProblems(verr))
		}
		return err
	}
	return nil
}

// schemaProblems is what the validator found, without its first line, which
// names the schema by an internal file path. A problem at the top of the
// value has no location; any other starts with where it is, as /owner.
func schemaProblems(verr *jsonschema.ValidationError) string {
	var problems []string
	for _, line := range strings.Split(verr.Error(), "\n")[1:] {
		line = strings.TrimPrefix(strings.TrimSpace(line), "- at ")
		where, problem, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		where = strings.Trim(where, "'")
		if where != "" {
			problem = where + ": " + problem
		}
		problems = append(problems, problem)
	}
	return strings.Join(problems, "; ")
}

func isObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// short is raw cut to a length that fits in an error message.
func short(raw json.RawMessage) string {
	if len(raw) > 80 {
		return string(raw[:77]) + "..."
	}
	return string(raw)
}

// pageFramePatience is how long Harbor waits for a page's frame to appear.
const pageFramePatience = 10 * time.Second

// frame opens the page the tool's frame is in, and returns the frame, once
// it has loaded.
func (e *Engine) frame(ctx context.Context, tab *browser.Tab, svc Service, c compiled, t service.Tool, input json.RawMessage) (*browser.Tab, error) {
	page := t.Frame.Page
	if t.Frame.PageFromInput {
		raw, err := invoke(ctx, tab, c, "tools."+t.Name+".frame.page", input)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &page); err != nil || !strings.HasPrefix(page, "/") {
			return nil, fmt.Errorf("%s's frame.page returned %s; it returns a path, such as /students/1", t.Name, short(raw))
		}
	}
	if err := tab.Navigate(ctx, siteURL(svc.Manifest, page)); err != nil {
		return nil, err
	}
	if err := tab.Settle(ctx); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(pageFramePatience)
	for {
		f, err := tab.Frame(ctx, t.Frame.Site)
		if err == nil || time.Now().After(deadline) {
			return f, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(checkEvery):
		}
	}
}
