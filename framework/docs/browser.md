# Design: an embedded browser

Status: built, on 2026-10-17, moved from Harbor (`backend/browser`,
`backend/passkey`, `backend/securitykey`, and its live view). Where the build
differs from the proposal, the section says so, and "What changed in the
build" at the end lists it.

## Why

An app on Desktop should not need to know the DevTools protocol, Chrome
extensions, USB, or WebAuthn crypto to use a website for the user. Harbor
needed all four, and they make up about 3,000 lines that its agents must read
around. Desktop owns hard, general code so that an app's code is only its own
rules. A browser is the same kind of thing as the database: Desktop runs it,
and the app uses it.

Desktop gives primitives, not a product. It does not decide when to show a
tab, what to ask the user, or where to keep a password. The app does.

## The model

- **One Chromium for the app.** It is headless, always. It starts on the first
  `Open`, and again on the next `Open` if it quits or crashes.
- **One Chrome profile.** That is Chrome's own data folder, `browser/` in
  `desktop.DataDir()`. Chrome keeps the cookies, local storage, and everything
  else there itself. Desktop copies nothing out of it.
- **Tabs.** The app opens tabs. A page can open more, such as a pop-up or a
  link with `target=_blank`: those are tabs too, and each knows its opener.

There is no visible mode. Harbor had one, which restarted Chromium with a
window, lost every tab, and needed a lock so that calls waited for the
restart. The live view (below) replaces it.

## Go: package `github.com/michaelckearney/monorepo/framework/browser`

An app that does not import this package does not get Chromium or chromedp.

```go
b := browser.Open(browser.Options{...}) // see Options
defer b.Close()

tab, err := b.Open(ctx, "https://example.com/")

// Forgetting what sites keep
b.ClearData(ctx, "https://example.com") error // the cookies the site gets, its parent domain's too, and its storage
b.ClearData(ctx, "") error                    // everything: Chromium stops and its profile is deleted
```

A browser is one profile, `Options.Dir`. An app that wants two, such as one
for its user and one an agent can clear, opens two browsers.

### A tab

```go
tab.ID() string                 // what the frontend names the tab by
tab.Close()
tab.Done() <-chan struct{}      // closed when the tab closes: by Close, by the page, or a crash

// Moving around
tab.Navigate(ctx, url) error
tab.Back(ctx) error             // starts it; Settle waits for the page
tab.Forward(ctx) error
tab.Reload(ctx) error
tab.URL(ctx) (string, error)    // asks Chromium now, as State may be a moment behind
tab.State() browser.State       // URL, Title, Loading, CanGoBack, CanGoForward
tab.Changed() <-chan struct{}   // closed, and replaced, when State or Opened changes

// Tabs it opened
tab.Opened() []*browser.Tab     // every open tab it opened, directly or through another, oldest first
tab.Opener() *browser.Tab       // nil for a tab the app opened

// Running code in the page
tab.Settle(ctx) error           // wait until the page stops making requests
tab.Evaluate(ctx, fn string, args ...any) (json.RawMessage, error) // *browser.JSError if the page throws
tab.OnNewDocument(ctx, script string) error // run script in the page now, and in every page before the page's own
tab.Expose(ctx, name string, fn browser.ExposedFunc) error
// type ExposedFunc func(ctx context.Context, origin string, arg json.RawMessage) (any, error)
tab.Frame(ctx, origin string) (*browser.Tab, error) // an iframe from another site, to Evaluate in

// Using the page as a person does, by its elements' refs
tab.Snapshot(ctx, limit int) (browser.Snapshot, error) // the accessibility tree, a line an element: `button "Go" [ref=e12]`
tab.Find(ctx, selector string) (string, error)        // the ref of the first element a CSS selector matches
tab.Click(ctx, ref) error
tab.DoubleClick(ctx, ref) error
tab.Type(ctx, ref, text string) error        // in place of what the field had; "" clears it; a slider is set to text
tab.Select(ctx, ref, values ...string) error // a <select>'s options, by value or text
tab.Check(ctx, ref, checked bool) error      // a checkbox or radio, clicked only if it is not so
tab.Press(ctx, keys string) error            // where the focus is: Enter, Tab, ArrowDown, a, Control+a, Shift+Tab
tab.EvaluateOn(ctx, ref, fn string, args ...any) (json.RawMessage, error) // fn(element, ...args)

// Acting as the user
tab.Input(ctx, browser.InputEvent) error // a mouse event, a key, or typed text; browser.ErrDialogOpen while a dialog is
tab.Cookies(ctx, url string) ([]browser.Cookie, error)
b.AllCookies(ctx) ([]browser.Cookie, error)   // every cookie the profile keeps
tab.Screenshot(ctx, full bool) ([]byte, error) // a JPEG of the window, or the whole page

// What the tab's pages asked the network for, and saved
tab.Requests() []browser.Request // method, URL, type, status, headers, page; the newest thousand
tab.RequestBody(ctx, id) ([]byte, error)  // what a POST sent
tab.ResponseBody(ctx, id) ([]byte, error) // while Chromium still has it
tab.Mark() browser.Mark                   // now, to ask what came after
tab.RequestsSince(m) []browser.Request
tab.SavedSince(m) []browser.Saved         // localStorage and sessionStorage keys added, changed, removed
b.CookieMark(ctx) (browser.CookieMark, error)                         // the profile's cookies now
b.CookiesChangedSince(ctx, m) ([]browser.CookieChange, error)         // added, changed, removed, by name
```

An action is made of the events your mouse and keyboard make, sent with
`Input`, so the page cannot tell it from you: a page opens a file picker or
a pop-up only for a click a person made. A click goes to the element's
middle, scrolled into view, and is refused, naming what is in the way, if
another element covers it, such as a cookie banner. Refs come from
`Snapshot`, as Playwright MCP's `browser_snapshot` gives them, or from
`Find`; an element keeps its ref while it is on the page, and a ref of one
that is gone is an error that says to take a new snapshot. `Snapshot` reads
the page as it is: `Settle` first, to read it once it has loaded.

A click on a button that calls `alert` would leave Chromium's answer to the
click waiting until the alert is answered. `Input`, and every action, returns
once the page has a dialog open instead, and sends the rest of what it was
doing, such as letting go of the mouse, once `Options.Dialog` has answered
it. While a dialog is open, `Input` is `browser.ErrDialogOpen`.

`Requests` sees what a page's own scripts cannot: the page load, a worker's
requests, one sent before a script runs. A redirect's hops are listed each
with its own response, as `<id>-redirect-<n>`. A new page does not start the
list again: a sign-in goes through several pages, and what it did is in the
requests of the pages it left. Each request has the page that made it, and
a page load, and each redirect on the way, is marked `Navigation`.

`SavedSince` and `CookiesChangedSince` have the names, never the values: a
sign-in's token is a secret. Chromium tells of no cookie a page's script
sets, so `CookiesChangedSince` compares the profile's cookies with those at
the mark. With `RequestsSince`, they say what an action did, which is how
Harbor shows its agent where a site keeps its sign-in.

`Expose` adds `window[name](arg)` to the page, and every page the tab loads.
The page calls it and gets a promise of what Go returns, or an Error of Go's
error; Go is told the origin of the script that called, as Chromium knows it,
not as the page says it. Harbor's saved logins are built on this: its page script
reports what you type into a login form, and its `fill` asks Go to type a saved
password with `Input`. Neither the script nor the rules for it are Desktop's.

### Options

Headless Chromium shows none of its own boxes. When a page needs one, Desktop
calls a hook and the app answers. A nil hook refuses.

```go
browser.Options{
	// The Chrome profile. Empty is browser/ in desktop.DataDir(); tests give
	// each browser a folder of its own.
	Dir string

	// A site asked for an HTTP password. Return nil to refuse.
	HTTPAuth func(ctx context.Context, r browser.AuthRequest) *browser.Credentials

	// A page called navigator.credentials.create or .get. Return the
	// credential, as PublicKeyCredential's toJSON makes it, or a
	// *browser.DOMError for the page to throw.
	Passkey func(ctx context.Context, r browser.PasskeyRequest) (json.RawMessage, error)

	// A page called alert, confirm, or prompt, or asked "leave this page?".
	// The page waits for the answer.
	Dialog func(ctx context.Context, tab *browser.Tab, d browser.Dialog) (accept bool, text string)

	// A page wants files, from an <input type=file>. Return paths.
	ChooseFiles func(ctx context.Context, tab *browser.Tab, r browser.FileRequest) ([]string, error)

	// A download finished, at path, in a folder of Desktop's.
	Downloaded func(tab *browser.Tab, d browser.Download)

	// For test sites that keep their URLs wherever they listen: Chromium's
	// host-resolver rules, as "MAP *.localhost:8443 127.0.0.1:52011", and
	// CAs to trust besides the system's. A site sends its CA's certificate
	// with its own.
	HostRules  []string
	TrustedCAs []*x509.Certificate
}
```

`HTTPAuth` and `Passkey` go through a Chrome extension, as in Harbor today,
because that is the only way Chrome gives a password to just the site that
asked and checks a passkey's site for you. Requests that arrive that way have
the page's `Origin`, but not its tab. `Dialog`, `ChooseFiles` and `Downloaded`
come through the DevTools protocol, so they have the tab.

`Dialog`, `ChooseFiles` and `Downloaded` are new. Without them, a page that
used one during a sign-in was stuck.

### Helpers for passkeys

`Passkey` gives the app a request. Two packages answer one, and keep nothing:
the app saves what they return where it likes.

```go
import (
	"github.com/michaelckearney/monorepo/framework/browser/passkey"
	"github.com/michaelckearney/monorepo/framework/browser/securitykey"
)

// A passkey made in software (ES256)
c, _ := passkey.ParseCreation(r.Options)
c.CanMake()                             // false for a site that takes no ES256 key
key, response, err := passkey.Create(c) // the app saves key
req, _ := passkey.ParseRequest(r.Options)
response, err := passkey.Get(req, key)

// A passkey on a USB security key, such as a YubiKey (CTAP2)
devs, err := securitykey.Devices()
response, err := securitykey.Create(ctx, dev, c, asker) // asker asks the user for a PIN, and to touch the key
response, err := securitykey.Get(ctx, dev, req, asker)
```

## Frontend: from `@desktop`

```ts
import { LiveView, useTab } from "@desktop";

const t = useTab(() => id.value);
// reactive: t.url, t.title, t.loading, t.canGoBack, t.canGoForward,
//           t.opened (IDs of the tabs it opened), t.closed
t.navigate(url); t.back(); t.forward(); t.reload(); t.close();
```

```vue
<LiveView :tab="id" style="height: 70vh" @problem="(m) => (problem = m)" />
```

`LiveView` shows one tab and sends it your mouse and keys. It draws nothing
else. It has a default slot, drawn over the page, for the app's own prompts,
such as a password box for `HTTPAuth`; while it shows, it takes the mouse.
It is only in `@desktop` for an app with `browser: true`.

- **It sizes the tab to fit.** The page lays out at the size of the
  `LiveView`, as it would in a window that size, drawn at the screen's pixel
  ratio, up to 2. When it goes, the tab goes back to its own size.
- **Only one tab paints at a time.** Headless Chromium draws only the tab in
  front, so a `LiveView` brings its tab to the front. If two are on screen, the
  newer one gets the frames; the older one keeps its last frame.
- **The frontend can drive any tab by its ID.** That is the app's own code, in
  the app's own window.

It acts like a browser window, so you can use it without thinking about it:

- **It takes your keys when it shows**, unless you are typing elsewhere in the
  app or in its prompt. It takes them back when the prompt goes. The page has
  your focus only while the view has your keys. When your keys go elsewhere,
  the field you were in loses its focus, then gets it back when you return.
- **The mouse has the page's cursor**: a pointer over a link, a text cursor
  over text and fields, and the page's own CSS cursors. The view sends the
  last mouse move, so the page knows where the mouse stopped. When the mouse
  leaves the view, the page's hover ends.
- **Copy and cut reach your clipboard.** Chromium's clipboard is its own, so
  the selected text goes to yours as well. Only the shown tab can do this, so
  a page the app drives cannot write to your clipboard.
- **The macOS editing shortcuts work**: ⌘⇧Z, ⌘ and ⌥ with the arrows (with or
  without ⇧), ⌥⌫, ⌘⌫, and the Control shortcuts (⌃A, ⌃E, ⌃K, ⌃H, ⌃D).
  Input methods work too: dead keys for accents, and composing scripts such
  as Japanese. The page shows the text while you compose it.
- **A right-click opens the browser's native menu**, unless the page opens its
  own. The menu fits what you clicked: a link, selected text, a field, or the
  page itself (Back, Forward, Reload).
- **The frames are drawn on a canvas, not shown as an image.** WebKit offers
  to select an image on a right-click, and macOS Live Text reads the text in
  it. With an image, a right-click selected the whole page.

A script in each frame tells the view the cursor, the copied text, and what
was right-clicked. It runs in a world of its own, so the page can neither see
it nor call it (`browser/live.go`). It does not reach a frame from another
site, which runs in a process of its own. Over such a frame, the cursor is the
default arrow.

### Transport

Frames, and changes to a tab `useTab` follows, go from Go to the frontend as
Wails events. No request stays open. WebKit, the app window's engine, lets
only about six requests to Go be open at once. Harbor's live view used a long
poll for frames, and froze when a page full of waiting tool calls used the
rest. Events fix that for the live view. A Go method that the app holds open
for a long time is still the app's problem.

Input goes back as calls, one after another, not as events: an event from the
frontend is a request to Go too, so it saves nothing, and calls keep the order
and say when one fails. Each call ends at once.

The service behind both is `browser.Tabs`, which package `browser` registers
when it is imported (`internal/services`), so an app that does not import it
does not link it.

A frame is a JPEG, at quality 90 so small text stays sharp, in base64. The event bus has not been measured at 30 frames a
second yet.

## What an app builds: Harbor's sign-in browser

The live view should be a small browser inside Harbor, in a modal, with:

- the service's tab, and a tab bar of the tabs it opened, from `t.opened`;
- an address bar bound to `t.url`, for a sign-in link from an email;
- back, forward, and reload, from `t.back`, `t.forward`, and `t.reload`;
- Harbor's prompts in `LiveView`'s slot.

That is a Harbor component, `SignInBrowser.vue`, built on `LiveView` and
`useTab`. When the sign-in ends, Harbor closes the tabs the service's tab
opened. It has no other tabs: each service's tab is its own.

What stays in Harbor:

- one tab for each service, with calls to a service taking turns in it;
- loading a service's code and calling its tools;
- logins: check, auto sign-in, and when to show the sign-in browser;
- whether a call waits for a sign-in;
- the prompts, and what each answer does;
- saved logins, TOTP, and the page script for them;
- where passwords and passkeys are kept.

## Moving it from Harbor

Each step leaves Harbor's tests passing.

1. Move `harbor/backend/browser` here. Drop the visible mode: `Mode`,
   `SetVisible`, `Acquire`, and the cookie carry. Replace `HandleFill` and
   `HandleSeen` with `Expose`, and move the saved-login script into Harbor's
   `engine/saved.go`. The extension and its messages lose Harbor's name.
2. Move `passkey` and `securitykey` here, under `browser/`. Their messages
   lose Harbor's name.
3. Add navigation, `State`, `Changed`, `Opened`, and `Opener`. `Opened`
   replaces `Front`.
4. Add `LiveView`, `useTab`, and the event transport. Harbor drops `Frame`,
   `Input`, and its long poll, and builds `SignInBrowser.vue`.
5. Add `Dialog`, `ChooseFiles`, and `Downloaded`.

The browser's tests move with it. Desktop's tests serve their own pages; they
cannot use Harbor's fixtures.

## Decisions

1. **Desktop pins Chromium**, turned on with `browser: true` in `desktop.yaml`,
   in place of the app's own `deps` entry, which is then an error. The
   extension and the DevTools calls are written against one Chromium version,
   so Desktop picks it, with each download's sha256 (`internal/pin`).
2. **A missing hook refuses.**
3. **Frames are drawn in an `<img>`.** Whether text is sharp on a Retina
   screen has not been checked by eye yet.

## What changed in the build

- `browser.Hooks` is `browser.Options`, which also has `Dir`.
- `URL(ctx)` stays beside `State`: it asks Chromium at once, which a passkey
  or password request needs.
- `OnNewDocument` and `Expose` act on the page shown now too, not only on
  later ones, so a tab needs no set-up before its first page.
- An exposed function returns a value, or an error, to the page, as a
  promise: Harbor's `fill` waits to know the text was typed.
- `Back`, `Forward`, and `Reload` start the move and return; `Navigate` still
  waits for the page.
- A tab the app opens starts its history at its first page: the about:blank
  every new tab starts at is not something to go back to.
- Input goes as calls, not events (see Transport).
- `Input` brings the tab to the front first, as a tab you use is. A page in a
  tab behind others has no focus: it did not focus its fields, and Chromium
  held its passkey requests, which Harbor's tests found once the live view no
  longer brought the tab forward on its own.
- Harbor's emailed-link box only took a link to the site being signed in to.
  The address bar takes any address you type.
