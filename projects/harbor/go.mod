module github.com/michaelckearney/monorepo/projects/harbor

go 1.26.0

// Go does not skip node_modules, and some npm packages ship Go files.
ignore ./node_modules

require (
	github.com/coder/websocket v1.8.15
	github.com/crewjam/saml v0.5.1
	github.com/dop251/goja v0.0.0-20261012133230-fac44d5949be
	github.com/evanw/esbuild v0.28.2
	github.com/go-webauthn/webauthn v0.18.2
	github.com/graph-gophers/graphql-go v1.10.3
	github.com/michaelckearney/monorepo/framework v0.0.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/mxschmitt/playwright-go v0.6201.1
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	golang.org/x/net v0.60.0
	golang.org/x/sys v0.48.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.60.1
)

require (
	github.com/beevik/etree v1.5.0 // indirect
	github.com/chromedp/cdproto v0.0.0-20260321001828-e3e3800016bc // indirect
	github.com/chromedp/chromedp v0.15.1 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/deckarep/golang-set/v2 v2.8.0 // indirect
	github.com/dlclark/regexp2/v2 v2.8.1 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/go-stack/stack v1.8.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/golang-jwt/jwt/v4 v4.5.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/google/pprof v0.0.0-20260802141513-ef3492d7dac3 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jonboulle/clockwork v0.2.2 // indirect
	github.com/mattermost/xml-roundtrip-validator v0.1.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/russellhaering/goxmldsig v1.4.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/wailsapp/wails/v3 v3.1.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/oauth2 v0.37.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// The framework is in this monorepo, at the same commit.
replace github.com/michaelckearney/monorepo/framework => ../../framework
