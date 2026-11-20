// Package browser runs one headless Chromium for the app, and its tabs, over
// the DevTools protocol: for an app that uses websites for its user, signed
// in as them. Set browser: true in desktop.yaml, and Desktop downloads the
// Chromium it pins.
//
// Headless Chromium shows nothing of its own: no window, no password box, no
// passkey dialog, no file picker. The app shows a tab in its own window with
// LiveView and useTab, from @desktop, and answers what a page asks the
// browser for through the hooks in Options.
//
// There is no mode with a window. One had to restart Chromium, which lost
// every tab, and with them each site's sessionStorage and page state.
package browser

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/michaelckearney/monorepo/framework"
	"github.com/michaelckearney/monorepo/framework/internal/pin"
)

// Options are the browser's folder, and the hooks that answer what pages ask
// the browser for. A nil hook refuses.
type Options struct {
	// Dir is the Chrome profile: Chrome's own folder, where it keeps cookies,
	// local storage, and the rest. Empty is browser/ in desktop.DataDir().
	Dir string

	// HTTPAuth answers a site's request for an HTTP password. Return nil to
	// refuse: the site then shows its own error page.
	HTTPAuth func(ctx context.Context, r AuthRequest) *Credentials

	// Passkey answers a page's navigator.credentials.create or .get: the
	// credential, as PublicKeyCredential's toJSON makes it, or a *DOMError
	// for the page to throw. ctx ends if the page gives up first.
	Passkey func(ctx context.Context, r PasskeyRequest) (json.RawMessage, error)

	// Dialog answers a page's alert, confirm, or prompt, or its "leave this
	// page?". The page waits for the answer; ctx ends if the dialog goes
	// first, as when the page navigates.
	Dialog func(ctx context.Context, tab *Tab, d Dialog) (accept bool, text string)

	// ChooseFiles answers a page's <input type=file>: the paths of the files
	// to give it. An error, or no paths, gives it none.
	ChooseFiles func(ctx context.Context, tab *Tab, r FileRequest) ([]string, error)

	// Downloaded is told of each download that finished, at d.Path, in a
	// folder of Desktop's that is emptied each time Chromium starts: move
	// the file to keep it.
	Downloaded func(tab *Tab, d Download)

	// HostRules send the browser's connections for some hosts elsewhere, as
	// Chromium's --host-resolver-rules do: "MAP *.localhost:8443
	// 127.0.0.1:52011" sends every name under localhost, on port 8443, to
	// port 52011. The page still sees the URL's own host and port, in its
	// address, its Host header, and its cookies. This is for test sites,
	// which keep their URLs wherever they listen.
	HostRules []string

	// TrustedCAs are CAs whose certificates the browser trusts as well as
	// the system's, for test sites. A site must send its CA's certificate
	// with its own: Chromium matches only the certificates a site sends.
	TrustedCAs []*x509.Certificate
}

// ErrClosed is returned once Close has been called.
var ErrClosed = errors.New("the browser is closed")

// Browser is one Chromium process at a time, on one profile. It starts on
// the first Open, and again on the next Open if it quits or crashes.
type Browser struct {
	opts Options
	// exec is Chromium's program, found when Chromium first starts. Tests
	// set it.
	exec string

	agentOnce sync.Once
	agent     string
	agentErr  error

	mu     sync.Mutex
	proc   *process // the running Chromium, or nil
	closed bool

	// front is the tab in front, as far as the browser has put one there.
	frontMu sync.Mutex
	front   *Tab
}

// process is one run of Chromium. Its events are handled with its own lock,
// never the browser's: the browser's is held while Chromium starts, and an
// event handler that waits stops Chromium's events, the answers that start
// it among them.
type process struct {
	ctx  context.Context
	stop func()

	mu sync.Mutex
	// tabs are the tabs that the browser knows, by target: those Open
	// made, and the tabs they opened.
	tabs map[target.ID]*Tab
	// downloads are the downloads under way, by their GUID.
	downloads map[string]download
}

// Open returns a browser with o's folder and hooks. Nothing starts until the
// first tab is opened.
func Open(o Options) *Browser {
	return &Browser{opts: o}
}

// Close stops Chromium for good.
func (b *Browser) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.halt()
}

func (b *Browser) dir() string {
	if b.opts.Dir != "" {
		return b.opts.Dir
	}
	return filepath.Join(desktop.DataDir(), "browser")
}

// running is the running Chromium, started if it is not. b.mu is held.
func (b *Browser) running() (*process, error) {
	if b.closed {
		return nil, ErrClosed
	}
	if b.proc != nil && b.proc.ctx.Err() == nil {
		return b.proc, nil
	}
	b.halt()
	if b.exec == "" {
		exec, err := Executable(desktop.Dep(pin.ChromiumDep))
		if err != nil {
			return nil, err
		}
		b.exec = exec
	}
	dir := b.dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := forgetSession(dir); err != nil {
		return nil, err
	}
	if err := clearSingleton(dir); err != nil {
		return nil, err
	}
	agent, err := b.userAgent()
	if err != nil {
		return nil, err
	}
	ext, err := writeExtension(dir)
	if err != nil {
		return nil, fmt.Errorf("could not write Desktop's extension: %w", err)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(b.exec),
		chromedp.UserDataDir(dir),
		chromedp.Flag("headless", true),
		chromedp.WindowSize(1200, 900),
		// Sites treat a browser that says it is automated differently, and
		// some refuse to sign it in.
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-infobars", true),
		chromedp.Flag("disable-sync", true),
		// The profile outlives every restart; without these, Chromium
		// offers to restore the tabs the last process had open.
		chromedp.Flag("hide-crash-restore-bubble", true),
		chromedp.Flag("disable-session-crashed-bubble", true),
		// Headless Chromium says HeadlessChrome in its User-Agent, and sites
		// that tie a session to the browser, or block bots, treat it so.
		chromedp.UserAgent(agent),
		// chromedp's defaults turn extensions off.
		chromedp.Flag("disable-extensions", false),
		chromedp.Flag("load-extension", ext),
	)
	if len(b.opts.HostRules) > 0 {
		opts = append(opts, chromedp.Flag("host-resolver-rules", strings.Join(b.opts.HostRules, ", ")))
	}
	if len(b.opts.TrustedCAs) > 0 {
		hashes := make([]string, len(b.opts.TrustedCAs))
		for i, ca := range b.opts.TrustedCAs {
			sum := sha256.Sum256(ca.RawSubjectPublicKeyInfo)
			hashes[i] = base64.StdEncoding.EncodeToString(sum[:])
		}
		opts = append(opts, chromedp.Flag("ignore-certificate-errors-spki-list", strings.Join(hashes, ",")))
	}
	alloc, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(alloc)
	fail := func(format string, err error) (*process, error) {
		cancel()
		cancelAlloc()
		return nil, fmt.Errorf(format, err)
	}
	// The first Run starts the process, with an empty first tab.
	if err := chromedp.Run(ctx); err != nil {
		return fail("could not start Chromium: %w", err)
	}
	// The tabs a tab opens, and a tab the page closed itself: chromedp does
	// not notice either on its own.
	err = chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return target.SetDiscoverTargets(true).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
	}))
	if err != nil {
		return fail("could not follow Chromium's tabs: %w", err)
	}
	p := &process{ctx: ctx, tabs: map[target.ID]*Tab{}, downloads: map[string]download{}}
	chromedp.ListenBrowser(ctx, b.onBrowserEvent(p))
	if err := b.allowDownloads(ctx, dir); err != nil {
		return fail("could not set where downloads go: %w", err)
	}
	if err := b.openBridge(ctx); err != nil {
		cancel()
		cancelAlloc()
		return nil, err
	}
	p.stop = func() {
		// Closed rather than killed, so Chromium writes its cookies to
		// the profile before it exits.
		closeCtx, done := context.WithTimeout(ctx, 5*time.Second)
		defer done()
		chromedp.Cancel(closeCtx)
		cancel()
		cancelAlloc()
	}
	b.proc = p
	return p, nil
}

// halt stops the running Chromium, if any. b.mu is held.
func (b *Browser) halt() {
	if b.proc == nil {
		return
	}
	b.proc.stop()
	b.proc = nil
}

// forgetSession stops Chromium restoring the last process's tabs, which it
// would otherwise do after each restart.
func forgetSession(profile string) error {
	dir := filepath.Join(profile, "Default")
	for _, pattern := range []string{"Current Session", "Current Tabs", "Last Session", "Last Tabs", "Sessions/Session_*", "Sessions/Tabs_*"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, m := range matches {
			if err := os.Remove(m); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

// singleton are the files Chromium puts in the profile to keep a second
// Chromium off it: the lock, a symlink to "hostname-pid", and its siblings.
var singleton = []string{"SingletonLock", "SingletonSocket", "SingletonCookie"}

// clearSingleton lets a start go ahead after the last Chromium died without
// cleaning up. A Chromium that crashed or was killed leaves its singleton
// files, and every later start aborts rather than share the profile. The
// lock's "hostname-pid" target names who held it: when that process is gone
// the files are left over and go; when it lives, another Chromium really has
// the profile, so they stay, and clearing fails rather than risk the profile
// corruption the lock guards against.
func clearSingleton(profile string) error {
	target, err := os.Readlink(filepath.Join(profile, "SingletonLock"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		// No lock to read: nothing can be holding it, so clear the rest.
		return removeSingleton(profile)
	}
	pid, err := singletonPID(target)
	if err != nil || !processAlive(pid) {
		return removeSingleton(profile)
	}
	return fmt.Errorf("could not start Chromium: another Chromium is using the profile %s; close it and start again", profile)
}

// removeSingleton drops the leftover singleton files, gone ones included.
func removeSingleton(profile string) error {
	for _, name := range singleton {
		if err := os.Remove(filepath.Join(profile, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// singletonPID is the pid a SingletonLock's "hostname-pid" target carries.
// A hostname can hold hyphens, so the pid is what follows the last one.
func singletonPID(target string) (int, error) {
	i := strings.LastIndexByte(target, '-')
	if i < 0 {
		return 0, fmt.Errorf("no pid in %q", target)
	}
	n, err := strconv.Atoi(target[i+1:])
	if err != nil {
		return 0, fmt.Errorf("no pid in %q: %w", target, err)
	}
	return n, nil
}

// processAlive reports whether a process with pid is running now.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 does nothing but report whether the process can be signalled,
	// which it cannot be once it is gone.
	return p.Signal(syscall.Signal(0)) == nil
}

// Open opens a tab at url, starting Chromium if it is not running.
func (b *Browser) Open(ctx context.Context, url string) (*Tab, error) {
	b.mu.Lock()
	p, err := b.running()
	b.mu.Unlock()
	if err != nil {
		return nil, err
	}
	tabCtx, cancel := chromedp.NewContext(p.ctx)
	// The first Run creates the tab, and the tab lives as long as the
	// context of that Run: it has to be the tab's own.
	if err := chromedp.Run(tabCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("could not open a tab: %w", err)
	}
	t := b.newTab(tabCtx, cancel, nil)
	b.opened()
	if err := t.setUp(ctx); err != nil {
		cancel()
		return nil, fmt.Errorf("could not open a tab: %w", err)
	}
	p.mu.Lock()
	p.tabs[t.id] = t
	p.mu.Unlock()
	// A new tab starts at about:blank, which back would go to.
	if err := t.run(ctx, chromedp.Navigate(url), page.ResetNavigationHistory()); err != nil {
		cancel()
		if reason, ok := isPageLoadError(err); ok {
			return nil, pageLoadError(url, reason)
		}
		return nil, fmt.Errorf("could not open %s: %w", url, err)
	}
	t.refresh()
	return t, nil
}

// Tab is one tab in the running Chromium. It is gone once Chromium stops.
type Tab struct {
	id      target.ID
	browser *Browser
	ctx     context.Context
	cancel  context.CancelFunc
	// opener is the tab that opened this one, if any; opened is when.
	opener *Tab
	opened time.Time

	mu sync.Mutex
	// children are the tabs this tab opened that are still open, oldest
	// first.
	children []*Tab
	state    State
	// changed is closed, and replaced, when state or the tabs it opened
	// change, or it closes.
	changed chan struct{}
	// frames are the frames from other sites the tab attached to
	// (frames.go).
	frames map[target.ID]*Tab
	// net is the tab's network activity, for Settle.
	net activity
	// requests are the requests its pages made, and saved what they saved
	// in their storage; seq orders them, for Mark (network.go).
	requests requestLog
	saved    savedLog
	seq      atomic.Uint64
	// scripts are the tab's script contexts, and what it exposes to them
	// (expose.go).
	scripts scripts
	// dialogUp is closed while the page has a dialog open, and dialogGone
	// while it has none; each is replaced when it opens or closes
	// (dialogs.go). Guarded by mu.
	dialogUp, dialogGone chan struct{}
	dialogOpen           bool
	// sending holds one token, taken while a sequence of inputs is sent,
	// such as a click's press and release, so another does not come
	// between them.
	sending chan struct{}
	// script is the script context Evaluate runs in, for a frame in the
	// tab's own process (frames.go); 0 for the page's.
	script runtime.ExecutionContextID
}

// opened notes that a new tab is in front, as Chromium puts it there.
func (b *Browser) opened() {
	b.frontMu.Lock()
	defer b.frontMu.Unlock()
	b.front = nil
}

// newTab is a tab, or a page's frame, in the target of ctx.
func (b *Browser) newTab(ctx context.Context, cancel context.CancelFunc, opener *Tab) *Tab {
	gone := make(chan struct{})
	close(gone)
	sending := make(chan struct{}, 1)
	sending <- struct{}{}
	return &Tab{
		id: chromedp.FromContext(ctx).Target.TargetID, browser: b, ctx: ctx, cancel: cancel,
		opener: opener, opened: time.Now(), changed: make(chan struct{}),
		dialogUp: make(chan struct{}), dialogGone: gone, sending: sending,
	}
}

// ID is what the frontend names the tab by, in useTab and LiveView.
func (t *Tab) ID() string { return string(t.id) }

// Done is closed when the tab is closed: by Close, by the page itself, or
// when Chromium stops.
func (t *Tab) Done() <-chan struct{} { return t.ctx.Done() }

// Close closes the tab.
func (t *Tab) Close() { t.cancel() }

// run runs actions in the tab, stopping early if ctx ends. Ending ctx does
// not close the tab.
func (t *Tab) run(ctx context.Context, actions ...chromedp.Action) error {
	runCtx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	err := chromedp.Run(runCtx, actions...)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil && t.ctx.Err() != nil {
		return errors.New("the tab was closed")
	}
	return err
}

// Navigate loads url in the tab, and waits for it to load.
func (t *Tab) Navigate(ctx context.Context, url string) error {
	if err := t.run(ctx, chromedp.Navigate(url)); err != nil {
		if reason, ok := isPageLoadError(err); ok {
			return pageLoadError(url, reason)
		}
		return err
	}
	return nil
}

// URL is the address the tab shows. Chromium says, not the page, so it
// answers while a page is loading, such as one waiting for a password.
func (t *Tab) URL(ctx context.Context) (string, error) {
	var info *target.Info
	err := t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		info, err = target.GetTargetInfo().WithTargetID(t.id).Do(cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Browser))
		return err
	}))
	if err != nil {
		return "", err
	}
	return info.URL, nil
}

// OnNewDocument runs script in the page now, and in every page the tab loads
// from now on, before the page's own scripts.
func (t *Tab) OnNewDocument(ctx context.Context, script string) error {
	return t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
		return err
	}), t.scripts.inPages(script))
}

// Cookie is a cookie as the browser keeps it, HttpOnly ones included.
type Cookie struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	Expires  float64 // seconds since 1970, or 0 for a session cookie
	HTTPOnly bool
	Secure   bool
}

// Cookies are the cookies the browser would send to url.
func (t *Tab) Cookies(ctx context.Context, url string) ([]Cookie, error) {
	var got []*network.Cookie
	err := t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		got, err = network.GetCookies().WithURLs([]string{url}).Do(ctx)
		return err
	}))
	if err != nil {
		return nil, err
	}
	return cookies(got), nil
}

func cookies(got []*network.Cookie) []Cookie {
	cookies := make([]Cookie, len(got))
	for i, c := range got {
		expires := c.Expires
		if c.Session || expires < 0 {
			expires = 0
		}
		cookies[i] = Cookie{c.Name, c.Value, c.Domain, c.Path, expires, c.HTTPOnly, c.Secure}
	}
	return cookies
}

// JSError is an error thrown in the page.
type JSError struct {
	Message string
}

func (e *JSError) Error() string { return e.Message }

// Evaluate calls fn, the source of a JavaScript function, in the page with
// args, waits for it if it returns a promise, and returns what it returned,
// as JSON. An error thrown in the page is a *JSError.
func (t *Tab) Evaluate(ctx context.Context, fn string, args ...any) (json.RawMessage, error) {
	if args == nil {
		args = []any{}
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	// The result crosses as a JSON string made in the page, so undefined,
	// dates, and the like arrive as JSON.stringify makes them.
	expr := fmt.Sprintf(`(async () => {
		const value = await (%s)(...%s);
		return JSON.stringify(value === undefined ? null : value);
	})()`, fn, argsJSON)
	var result *runtime.RemoteObject
	var exc *runtime.ExceptionDetails
	err = t.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		eval := runtime.Evaluate(expr).WithAwaitPromise(true).WithReturnByValue(true)
		if t.script != 0 {
			eval = eval.WithContextID(t.script)
		}
		result, exc, err = eval.Do(ctx)
		return err
	}))
	if err != nil {
		return nil, err
	}
	if exc != nil {
		return nil, &JSError{Message: exceptionMessage(exc)}
	}
	var s string
	if err := json.Unmarshal(result.Value, &s); err != nil {
		return nil, fmt.Errorf("the page returned something that is not JSON: %s", result.Value)
	}
	return json.RawMessage(s), nil
}

// exceptionMessage is what was thrown: an Error's "Name: message", without
// the stack, which points into Evaluate's wrapper rather than the caller's
// code.
func exceptionMessage(exc *runtime.ExceptionDetails) string {
	if exc.Exception != nil && exc.Exception.Description != "" {
		desc := exc.Exception.Description
		if i := strings.Index(desc, "\n    at "); i >= 0 {
			desc = desc[:i]
		}
		return desc
	}
	if exc.Exception != nil && len(exc.Exception.Value) > 0 {
		return "Uncaught " + string(exc.Exception.Value)
	}
	return exc.Text
}
