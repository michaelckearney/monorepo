// Package pin holds the versions desktop is built against.
//
// It is separate from package desktop, which imports Wails: the CLI needs the
// version but must not link Wails itself, or building desktop on Linux would
// need the GTK and WebKit headers that only a desktop app should need.
package pin

// Wails is the one Wails release this version of desktop is built and tested
// against. The generated app requires it, the bindings generator runs it, and
// the frontend's @wailsio/runtime is the source that ships inside it.
const Wails = "v3.1.0"

// WailsRuntimeSource is where, in the Wails module, the TypeScript source of
// @wailsio/runtime is. The frontend is built from it, so it is always the
// runtime of the Wails the app is built with, and a project never installs
// the npm package.
const WailsRuntimeSource = "internal/runtime/desktop/@wailsio/runtime/src"

// PlaywrightCLI is the @playwright/cli release `harness record` drives. It is
// pre-1.0 and built on a pre-release Playwright, so it is pinned exactly and
// moved only after a recording has been checked against the new one.
const PlaywrightCLI = "0.1.22"

// Download is a file desktop pins: where it is, and its sha256.
type Download struct {
	URL    string
	SHA256 string
}

// Chromium is the Chromium that package browser drives, for each platform,
// as an app with browser: true in desktop.yaml gets it. The extension and
// the DevTools calls in package browser are written against this version, so
// it moves only after they have been checked against the new one.
//
// These are Playwright's builds: Google's Chrome for Testing has no Linux
// arm64 build, which Playwright makes itself. Neither makes one for Windows
// on Arm, so it gets the x64 build, which Windows 11 on Arm runs under
// emulation.
var Chromium = map[string]Download{
	"darwin/arm64":  {"https://cdn.playwright.dev/builds/cft/151.0.7922.34/mac-arm64/chrome-mac-arm64.zip", "01a23ef9501b2745e0c2944c2e583207e6f6132d8d91c3a87ff65b5079e438ef"},
	"darwin/amd64":  {"https://cdn.playwright.dev/builds/cft/151.0.7922.34/mac-x64/chrome-mac-x64.zip", "69bcc853db975a2380767e9ff36da17f1d7b782fbbe191a210f676d2d5967d3e"},
	"linux/amd64":   {"https://cdn.playwright.dev/builds/cft/151.0.7922.34/linux64/chrome-linux64.zip", "ae8736ac28bc69278551500f219fc749575648263c43ec5990749eff43b9fcf8"},
	"linux/arm64":   {"https://cdn.playwright.dev/builds/chromium/1234/chromium-linux-arm64.zip", "b5ad7d8fe70f230b34198ddb5626d717c016db2f627cb44b922babbcaf3479b9"},
	"windows/amd64": {"https://cdn.playwright.dev/builds/cft/151.0.7922.34/win64/chrome-win64.zip", "045621e45a9dd27002c7fc1d8e10fe9f5f71f4cadbf44ec6f397f56f0179725c"},
	"windows/arm64": {"https://cdn.playwright.dev/builds/cft/151.0.7922.34/win64/chrome-win64.zip", "045621e45a9dd27002c7fc1d8e10fe9f5f71f4cadbf44ec6f397f56f0179725c"},
}

// ChromiumDep is the name the pinned Chromium has among an app's
// dependencies, for desktop.Dep.
const ChromiumDep = "chromium"
