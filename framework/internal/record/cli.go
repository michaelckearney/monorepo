package record

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/michaelckearney/monorepo/framework/internal/home"
	"github.com/michaelckearney/monorepo/framework/internal/pin"
	"github.com/michaelckearney/monorepo/framework/internal/steps"
)

// playwrightCLI returns the pinned playwright-cli, installing it and its
// WebKit into desktop's cache on first use. Nothing goes into the project:
// recording is a desktop feature, not a dependency of the app.
func playwrightCLI(ctx context.Context) (string, error) {
	dir, err := home.Path("tools", "playwright-cli-"+pin.PlaywrightCLI)
	if err != nil {
		return "", err
	}
	browsers, err := home.BrowsersEnv()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "node_modules", ".bin", "playwright-cli")
	if _, err := os.Stat(bin); err != nil {
		steps.Logf("installing Playwright CLI %s (first run only)", pin.PlaywrightCLI)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, "npm", "install", "--prefix", dir, "--no-fund", "--no-audit",
			"--save-exact", "@playwright/cli@"+pin.PlaywrightCLI)
		if out, err := cmd.CombinedOutput(); err != nil {
			steps.PrintBlock("[npm] ", out)
			return "", steps.Fail("could not install Playwright CLI; see the npm output above")
		}
	}
	// The CLI's WebKit is a different build from the one @playwright/test uses
	// in tests/, so it has its own download. The stamp saves asking Playwright
	// on every start.
	stamp := filepath.Join(dir, "webkit-installed")
	if _, err := os.Stat(stamp); err != nil {
		steps.Logf("installing WebKit for recordings (first run only)")
		cmd := exec.CommandContext(ctx, bin, "install-browser", "webkit")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), browsers...)
		if out, err := cmd.CombinedOutput(); err != nil {
			steps.PrintBlock("[playwright] ", out)
			if bytes.Contains(out, []byte("missing dependencies")) {
				return "", steps.Fail("WebKit needs system libraries this machine lacks; the [playwright] lines above name them")
			}
			return "", steps.Fail("could not install WebKit; see the output above")
		}
		if err := os.WriteFile(stamp, nil, 0o644); err != nil {
			return "", err
		}
	}
	return bin, nil
}

// result is one playwright-cli command's output, taken apart.
type result struct {
	out      string
	code     string // the Playwright code the command ran
	snapshot string // the screen list after it, if the command gave one
	failed   bool
}

var (
	codeBlock    = regexp.MustCompile("(?s)### Ran Playwright code\\n```js\\n(.*?)\\n```")
	snapshotLink = regexp.MustCompile(`\[Snapshot\]\(([^)]+)\)`)
	// The snapshot command prints the list inline; the others link to a file.
	snapshotInline = regexp.MustCompile("(?s)### Snapshot\n```yaml\n(.*?)\n```")
	errorBlock     = regexp.MustCompile(`(?s)### Error\n(.*)`)
)

// cli runs playwright-cli in a session, from dir, which is where it writes
// snapshots and other files.
type cli struct {
	bin, session, dir string
}

func (c cli) run(ctx context.Context, args ...string) result {
	cmd := exec.CommandContext(ctx, c.bin, append([]string{"-s=" + c.session}, args...)...)
	cmd.Dir = c.dir
	// The session's browser is started by the first command, and finds its
	// WebKit through this.
	if browsers, err := home.BrowsersEnv(); err == nil {
		cmd.Env = append(os.Environ(), browsers...)
	}
	out, err := cmd.CombinedOutput()
	r := result{out: string(out), failed: err != nil}
	if m := codeBlock.FindStringSubmatch(r.out); m != nil {
		r.code = m[1]
	}
	if m := snapshotInline.FindStringSubmatch(r.out); m != nil {
		r.snapshot = m[1] + "\n"
	} else if m := snapshotLink.FindStringSubmatch(r.out); m != nil {
		if raw, err := os.ReadFile(filepath.Join(c.dir, m[1])); err == nil {
			r.snapshot = string(raw)
		}
	}
	return r
}

// errorText is the part of a failed command's output worth showing.
func (r result) errorText() string {
	if m := errorBlock.FindStringSubmatch(r.out); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(r.out)
}

// runCode runs a snippet of Playwright code against the page.
func (c cli) runCode(ctx context.Context, body string) result {
	return c.run(ctx, "run-code", fmt.Sprintf("async page => { %s }", body))
}
