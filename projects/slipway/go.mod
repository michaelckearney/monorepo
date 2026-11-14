module github.com/michaelckearney/monorepo/projects/slipway

go 1.26.0

// Go does not skip node_modules, and some npm packages ship Go files.
ignore ./node_modules

require (
	github.com/michaelckearney/monorepo/framework v0.0.0
	github.com/mxschmitt/playwright-go v0.6201.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/coder/websocket v1.8.14 // indirect
	github.com/deckarep/golang-set/v2 v2.8.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/wailsapp/wails/v3 v3.1.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
	modernc.org/sqlite v1.60.1 // indirect
)

// The app is built from the slipway beside it, at the same commit, so the
// app and the command it drives never differ.
replace github.com/michaelckearney/monorepo/framework => ../../framework
