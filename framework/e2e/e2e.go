// Package e2e drives the running app from a browser, for the Go tests in a
// backend's e2e/ folder. `harness test -e2e` builds and starts the app, then
// runs those tests with go test; Open gives each test a WebKit page on it.
//
//	func TestGreeting(t *testing.T) {
//		page := e2e.Open(t)
//		e2e.Do(t, page.GetByPlaceholder("Your name").Fill("Ada"))
//		e2e.Do(t, page.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Greet"}).Click())
//		e2e.Expect(t, page.GetByText("Hello, Ada!")).ToBeVisible()
//	}
//
// Every test in a run shares one app, and so one database: give what a test
// creates names of its own.
package e2e

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mxschmitt/playwright-go"

	"github.com/michaelckearney/monorepo/framework/internal/appstate"
)

// How long an action or an assertion waits for the page, in milliseconds.
const (
	actionTimeout = 10_000
	expectTimeout = 5_000
)

var (
	startOnce sync.Once
	browser   playwright.Browser
	startErr  error
)

// start installs the Playwright driver and WebKit if they are missing, and
// launches WebKit once for every test in the package.
func start() (playwright.Browser, error) {
	startOnce.Do(func() {
		opts := &playwright.RunOptions{Browsers: []string{"webkit"}, Verbose: false, Stdout: io.Discard}
		if startErr = playwright.Install(opts); startErr != nil {
			return
		}
		var pw *playwright.Playwright
		if pw, startErr = playwright.Run(opts); startErr != nil {
			return
		}
		// Wails draws the window with WKWebView on macOS and WebKitGTK on
		// Linux. WebKit is the browser closest to what users see.
		browser, startErr = pw.WebKit.Launch()
	})
	return browser, startErr
}

// Open opens the app's first page in a new browser context, for this test
// alone. The context is closed when the test ends. If the test failed, its
// trace and a screenshot are kept in .desktop/test-results/<test>/.
func Open(t testing.TB) playwright.Page {
	t.Helper()
	url := os.Getenv(appstate.EnvE2EURL)
	if url == "" {
		t.Fatal("e2e tests need the running app; run them with: harness test -e2e")
	}
	b, err := start()
	if err != nil {
		t.Fatalf("could not start WebKit: %v", err)
	}
	ctx, err := b.NewContext(playwright.BrowserNewContextOptions{
		BaseURL:  playwright.String(url),
		Viewport: viewport(),
		// Pinned so a screenshot or a formatted date is the same on every
		// machine.
		ColorScheme: playwright.ColorSchemeLight,
		Locale:      playwright.String("en-US"),
		TimezoneId:  playwright.String("UTC"),
	})
	if err != nil {
		t.Fatalf("could not open a browser context: %v", err)
	}
	ctx.SetDefaultTimeout(actionTimeout)
	if err := ctx.Tracing().Start(playwright.TracingStartOptions{
		Screenshots: playwright.Bool(true),
		Snapshots:   playwright.Bool(true),
	}); err != nil {
		t.Fatalf("could not start the trace: %v", err)
	}
	page, err := ctx.NewPage()
	if err != nil {
		t.Fatalf("could not open a page: %v", err)
	}
	t.Cleanup(func() {
		t.Helper()
		finish(t, ctx, page)
	})
	if _, err := page.Goto("/"); err != nil {
		t.Fatalf("could not open the app: %v", err)
	}
	return page
}

func finish(t testing.TB, ctx playwright.BrowserContext, page playwright.Page) {
	t.Helper()
	defer ctx.Close()
	dir := os.Getenv(appstate.EnvE2EResults)
	if !t.Failed() || dir == "" {
		ctx.Tracing().Stop()
		return
	}
	dir = filepath.Join(dir, safeName(t.Name()))
	os.MkdirAll(dir, 0o755)
	page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(filepath.Join(dir, "screenshot.png"))})
	ctx.Tracing().Stop(filepath.Join(dir, "trace.zip"))
	if wd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(filepath.Dir(filepath.Dir(wd)), dir); err == nil {
			dir = rel
		}
	}
	t.Logf("trace and screenshot in %s", dir)
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func safeName(name string) string {
	return strings.Trim(unsafe.ReplaceAllString(name, "-"), "-")
}

func viewport() *playwright.Size {
	w, h, ok := strings.Cut(os.Getenv(appstate.EnvE2EViewport), "x")
	width, werr := strconv.Atoi(w)
	height, herr := strconv.Atoi(h)
	if !ok || werr != nil || herr != nil {
		return &playwright.Size{Width: 1024, Height: 768}
	}
	return &playwright.Size{Width: width, Height: height}
}

// Do fails the test now if an action returned an error, so a test needs no
// error check after each click.
//
//	e2e.Do(t, page.GetByRole("button", playwright.PageGetByRoleOptions{Name: "Save"}).Click())
func Do(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Expect makes assertions about what l finds. Each waits up to five seconds
// for the page to agree, then fails the test.
func Expect(t testing.TB, l playwright.Locator) Assertions {
	return Assertions{t: t, a: playwright.NewPlaywrightAssertions(expectTimeout).Locator(l)}
}

// Assertions are the common Playwright locator assertions. Not() reverses
// them. Anything missing here is in playwright.NewPlaywrightAssertions.
type Assertions struct {
	t testing.TB
	a playwright.LocatorAssertions
}

func (a Assertions) check(err error) {
	a.t.Helper()
	if err != nil {
		a.t.Fatal(err)
	}
}

// Not reverses the assertion that follows: Expect(t, l).Not().ToBeVisible().
func (a Assertions) Not() Assertions { return Assertions{t: a.t, a: a.a.Not()} }

func (a Assertions) ToBeVisible()  { a.t.Helper(); a.check(a.a.ToBeVisible()) }
func (a Assertions) ToBeHidden()   { a.t.Helper(); a.check(a.a.ToBeHidden()) }
func (a Assertions) ToBeEnabled()  { a.t.Helper(); a.check(a.a.ToBeEnabled()) }
func (a Assertions) ToBeDisabled() { a.t.Helper(); a.check(a.a.ToBeDisabled()) }
func (a Assertions) ToBeChecked()  { a.t.Helper(); a.check(a.a.ToBeChecked()) }

// ToHaveText expects the whole text: a string, a *regexp.Regexp, or a slice
// of either, one for each element l finds.
func (a Assertions) ToHaveText(want any) { a.t.Helper(); a.check(a.a.ToHaveText(want)) }

// ToContainText expects part of the text, as ToHaveText takes it.
func (a Assertions) ToContainText(want any) { a.t.Helper(); a.check(a.a.ToContainText(want)) }

func (a Assertions) ToHaveCount(n int)    { a.t.Helper(); a.check(a.a.ToHaveCount(n)) }
func (a Assertions) ToHaveValue(want any) { a.t.Helper(); a.check(a.a.ToHaveValue(want)) }
func (a Assertions) ToHaveAttribute(name string, want any) {
	a.t.Helper()
	a.check(a.a.ToHaveAttribute(name, want))
}
