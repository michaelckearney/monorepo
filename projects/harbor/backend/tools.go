package backend

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// Tools runs services' tools for the frontend, in Harbor's browser.
//
// Harbor has two browsers, each a profile of its own. Yours is where
// dashboards, the Tools page, and the agent's call_service_tool run, signed
// in as you. The agent's is where the agent finds out how a site works and
// tests the services it writes (browser_* and test_service_tool): it can
// clear what a site keeps there, to try a sign-in from the start, without
// signing you out. Both sign in with the same saved logins and passkeys.
type Tools struct {
	db     *sql.DB
	dir    string
	window func(agent bool) engine.Window

	once   sync.Once
	engine *engine.Engine
	err    error

	agentOnce   sync.Once
	agentEngine *engine.Engine
}

// agentView starts the key of a live view in the agent's browser, in
// live_views.service. A service's name has no colon, so the keys of the two
// browsers' views never meet.
const agentView = "agent:"

func newTools(db *sql.DB, dir string, window func(agent bool) engine.Window) *Tools {
	// A live view does not outlive the app; a crash can leave its row.
	if _, err := db.Exec(`DELETE FROM live_views`); err != nil {
		panic(fmt.Sprintf("could not clear the live views: %v", err))
	}
	return &Tools{db: db, dir: dir, window: window}
}

// window shows the engine's live views in Harbor's window: the window shows
// each row of live_views, and its prompt. agent is set for the agent's
// browser.
type window struct {
	db    *sql.DB
	agent bool
}

func (w window) key(service string) string {
	if w.agent {
		return agentView + service
	}
	return service
}

func (w window) Open(service, tab, heading string, browse bool) (func(), error) {
	if w.agent {
		// You may sign in to a site twice: here, for the agent to build
		// the service, and then in your own browser, to use it.
		heading += ", in the agent's browser"
	}
	key := w.key(service)
	_, err := w.db.Exec(`INSERT INTO live_views (service, tab, heading, browse) VALUES (?, ?, ?, ?)`, key, tab, heading, browse)
	if err != nil {
		return nil, err
	}
	return func() { w.db.Exec(`DELETE FROM live_views WHERE service = ?`, key) }, nil
}

func (w window) Prompt(service string, p *engine.Prompt) error {
	var prompt any
	if p != nil {
		raw, err := json.Marshal(p)
		if err != nil {
			return err
		}
		prompt = string(raw)
	}
	_, err := w.db.Exec(`UPDATE live_views SET prompt = ? WHERE service = ?`, prompt, w.key(service))
	return err
}

// start creates the engine on first use. Chromium starts with the first
// service's tab.
func (t *Tools) start() (*engine.Engine, error) {
	t.once.Do(func() {
		t.engine = engine.New(browserDir(t.dir, false), secrets{t.db}, t.window(false), files{t.db})
	})
	return t.engine, t.err
}

// agent creates the agent's browser's engine on first use.
func (t *Tools) agent() *engine.Engine {
	t.agentOnce.Do(func() {
		t.agentEngine = engine.New(browserDir(t.dir, true), secrets{t.db}, t.window(true), files{t.db})
	})
	return t.agentEngine
}

// viewEngine is the engine that shows the live view by its key in
// live_views, and the service's name.
func (t *Tools) viewEngine(key string) (*engine.Engine, string, error) {
	if name, ok := strings.CutPrefix(key, agentView); ok {
		return t.agent(), name, nil
	}
	e, err := t.start()
	return e, key, err
}

// openTabs opens every service's tab. One that does not open is opened on
// its service's next call, so a failure here changes nothing.
func (t *Tools) openTabs(e *engine.Engine) {
	rows, err := t.db.Query(`SELECT name FROM services ORDER BY name`)
	if err != nil {
		return
	}
	var names []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			names = append(names, name)
		}
	}
	rows.Close()
	var svcs []engine.Service
	for _, name := range names {
		if svc, err := t.service(name); err == nil {
			svcs = append(svcs, svc)
		}
	}
	e.OpenTabs(context.Background(), svcs)
}

// ServiceShutdown stops Chromium when the app quits.
func (t *Tools) ServiceShutdown() error {
	if t.engine != nil {
		t.engine.Close()
	}
	if t.agentEngine != nil {
		t.agentEngine.Close()
	}
	return nil
}

func (t *Tools) service(name string) (engine.Service, error) {
	var svc engine.Service
	var manifest, problem sql.NullString
	err := t.db.QueryRow(`SELECT source, manifest, manifest_error FROM services WHERE name = ?`, name).
		Scan(&svc.Source, &manifest, &problem)
	if errors.Is(err, sql.ErrNoRows) {
		return svc, fmt.Errorf("there is no service named %s", name)
	}
	if err != nil {
		return svc, err
	}
	if !manifest.Valid {
		return svc, fmt.Errorf("the service %s could not be read, so it has no tools until its file is fixed: %s", name, problem.String)
	}
	var m service.Manifest
	if err := json.Unmarshal([]byte(manifest.String), &m); err != nil {
		return svc, err
	}
	svc.Manifest = m
	return svc, nil
}

// engineFor starts the engine, and reads the named service.
func (t *Tools) engineFor(serviceName string) (*engine.Engine, engine.Service, error) {
	e, err := t.start()
	if err != nil {
		return nil, engine.Service{}, err
	}
	svc, err := t.service(serviceName)
	return e, svc, err
}

// Call runs a service's tool with input, a JSON object, and returns its
// output as JSON.
func (t *Tools) Call(ctx context.Context, serviceName, tool, input string) (string, error) {
	e, svc, err := t.engineFor(serviceName)
	if err != nil {
		return "", err
	}
	out, err := e.Call(ctx, svc, tool, json.RawMessage(input))
	t.note(serviceName, tool, err)
	return string(out), err
}

// CallForDashboard is Call for the dashboard with this id: if Harbor cannot
// sign in on its own, it fails at once, with an engine.SignInNeeded that
// says what you need to sign in to, rather than show you the login page and
// wait. (Which services a dashboard uses, its file says: Documents notes them.)
func (t *Tools) CallForDashboard(ctx context.Context, dashboard int64, serviceName, tool, input string) (string, error) {
	e, svc, err := t.engineFor(serviceName)
	if err != nil {
		return "", err
	}
	out, err := e.CallWithoutWaiting(ctx, svc, tool, json.RawMessage(input))
	t.note(serviceName, tool, err)
	return string(out), err
}

// SignedIn reports whether you are signed in to a service.
func (t *Tools) SignedIn(ctx context.Context, serviceName string) (bool, error) {
	e, svc, err := t.engineFor(serviceName)
	if err != nil {
		return false, err
	}
	ok, err := e.SignedIn(ctx, svc)
	if err == nil && !ok {
		t.note(serviceName, "", engine.NotSignedIn(svc))
	} else if ok {
		t.note(serviceName, "", nil)
	}
	return ok, err
}

// note keeps sign_ins_needed to what Harbor last found: a call or check
// that found you signed out writes its row, and one that got through
// deletes the rows it passed.
func (t *Tools) note(serviceName, tool string, err error) {
	var needed *engine.SignInNeeded
	switch {
	case err == nil:
		t.db.Exec(`DELETE FROM sign_ins_needed WHERE service = ? AND tool IN ('', ?)`, serviceName, tool)
	case errors.As(err, &needed):
		t.db.Exec(`INSERT INTO sign_ins_needed (service, tool, heading, message) VALUES (?, ?, ?, ?)
			ON CONFLICT (service, tool) DO UPDATE SET heading = excluded.heading, message = excluded.message`,
			needed.Service, needed.Tool, needed.Heading, needed.Message)
	}
}

// SignIn signs in to the service, and to the tool's own login if tool is
// named: on its own, with a saved login, or by showing you the login page
// in Harbor's window. It returns once you are signed in (true), or
// cancelled or waited five minutes (false).
func (t *Tools) SignIn(ctx context.Context, serviceName, tool string) (bool, error) {
	e, svc, err := t.engineFor(serviceName)
	if err != nil {
		return false, err
	}
	ok, err := e.SignIn(ctx, svc, tool)
	if ok {
		t.note(serviceName, tool, nil)
	}
	return ok, err
}

// Browse shows the service's tab in Harbor's window, to look around or to do
// what Harbor cannot do for you, until you are done.
func (t *Tools) Browse(ctx context.Context, serviceName string) error {
	e, svc, err := t.engineFor(serviceName)
	if err != nil {
		return err
	}
	return e.Browse(ctx, svc)
}

// CloseView closes a live view, by its key in live_views: a sign-in ends
// signed out, and looking around ends.
func (t *Tools) CloseView(key string) error {
	e, name, err := t.viewEngine(key)
	if err != nil {
		return err
	}
	return e.CloseView(name)
}

// Answer answers what a sign-in asks you beside the page, in the live view
// by its key in live_views: a password the site asked for over HTTP, or
// whether to use a passkey.
func (t *Tools) Answer(key string, a engine.Answer) error {
	e, name, err := t.viewEngine(key)
	if err != nil {
		return err
	}
	return e.Answer(name, a)
}

// Test runs a service's tool in the agent's browser, as Call does in
// yours, for the agent to test what it wrote. With no tool, it tests the
// sign-in from the start, and says what the service's page then shows,
// whether it signed in or not (engine.TestSignIn).
func (t *Tools) Test(ctx context.Context, serviceName, tool, input string) (string, error) {
	svc, err := t.service(serviceName)
	if err != nil {
		return "", err
	}
	e := t.agent()
	if tool == "" {
		r, err := e.TestSignIn(ctx, svc)
		page, _ := json.Marshal(r)
		if err != nil {
			if r.URL == "" {
				return "", testHint(svc, err)
			}
			return "", fmt.Errorf("%w. The service's page now: %s", testHint(svc, err), page)
		}
		if svc.Manifest.Login == nil {
			return string(page), nil
		}
		out, err := json.Marshal(struct {
			engine.SignInTest
			Note string `json:"note"`
		}{r, "Harbor cleared what your browser kept for the site, and login.check said not signed in; then it signed in, and login.check said signed in. This is the service's page now, where tools run. If it asks to sign in, login.check is wrong."})
		return string(out), err
	}
	out, err := e.Call(ctx, svc, tool, json.RawMessage(input))
	if err != nil {
		return "", testHint(svc, err)
	}
	return string(out), nil
}

// testHint adds to a failed test what the agent would otherwise have to
// guess: whether the service's login check may be what is wrong.
func testHint(svc engine.Service, err error) error {
	if svc.Manifest.Login == nil {
		return err
	}
	if errors.Is(err, engine.ErrSignInNeeded) {
		return fmt.Errorf("%w. Harbor showed the user the login page, and login.check never said they were signed in. If they did sign in, login.check is wrong: look at the signed-in page with the browser_* tools, where your browser now is, and try a new check with browser_evaluate", err)
	}
	var jsErr *browser.JSError
	if errors.As(err, &jsErr) && !errors.Is(err, engine.ErrCannotTell) {
		return fmt.Errorf("%w. login.check said the user was signed in. If the site sent the tool to its login page, or answered 401 or 403, login.check is wrong", err)
	}
	return err
}

// SaveLogin saves the login Harbor types in for you on a site, such as
// https://tidewater.localhost:8443, when the site signs you out.
func (t *Tools) SaveLogin(site string, l engine.Login) error {
	u, err := url.Parse(strings.TrimSpace(site))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%q is not a site's address, such as https://example.com", site)
	}
	return secrets{t.db}.SaveLogin(u.Scheme+"://"+u.Host, l)
}

// DeleteLogin forgets the login saved for a site.
func (t *Tools) DeleteLogin(site string) error {
	_, err := t.db.Exec(`DELETE FROM logins WHERE origin = ?`, site)
	return err
}

// UsePasskeyWithoutAsking sets whether Harbor uses one of its passkeys, by
// its ID in base64url, without asking you, while it signs in to the site.
func (t *Tools) UsePasskeyWithoutAsking(id string, auto bool) error {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return fmt.Errorf("%q is not a passkey's ID", id)
	}
	return secrets{t.db}.UsePasskeyWithoutAsking(raw, auto)
}

// DeletePasskey deletes one of Harbor's passkeys. The site still lists it
// until you remove it there.
func (t *Tools) DeletePasskey(id string) error {
	_, err := t.db.Exec(`DELETE FROM passkeys WHERE id = ?`, id)
	return err
}
