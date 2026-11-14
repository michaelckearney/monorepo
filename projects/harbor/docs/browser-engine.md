# The browser engine

Runs services' tools in Harbor's own Chromium, signed in as you. Dashboards,
MCP clients, and the agent all go through it.

## Chromium

Harbor's browser is Slipway's package `browser` (`browser: true` in
`desktop.yaml`; see Slipway's `docs/browser.md`). Slipway pins the Chromium
(Playwright's builds: Google's Chrome for Testing has no Linux arm64 build),
downloads it as a dependency, and `harness build` embeds it.

- Two headless Chromium processes, each a profile of its own:
  - **Yours**, `<data folder>/browser`: dashboards, the Tools page, and the
    agent's `call_service_tool` run here, signed in as you.
  - **The agent's**, `<data folder>/agent-browser`: the agent's `browser_*`
    tools and `test_service_tool` run here, while it builds a service. It
    can clear a site here (`browser_clear_data`) to test a sign-in from the
    start, without signing you out. You may sign in to a site in both.

  Both sign in with the same saved logins and passkeys. Each starts on its
  first call, and runs until Harbor quits. There is no mode with a window:
  you see a tab in Harbor's own window (see UI). A sign-in in the agent's
  browser says so in its heading.
- When Harbor starts, every service's tab opens, three at a time, so a page
  has loaded and settled before its first call.
- It says it is Chrome, with Chrome's own User-Agent and Chromium's major
  version, not `HeadlessChrome`: sites that tie a session to the browser,
  or block bots, treat a headless browser differently.
- Slipway's extension answers what sites ask the browser for, through the
  hooks Harbor gives it: see Passwords and passkeys.

## Layers

**Slipway's `browser`** knows nothing about services: tabs, where each is
and the tabs it opened, `Evaluate`, `Expose`, frames, `Settle`, input, a
page's snapshot with refs and actions on them, what an action did, and
hooks for HTTP passwords, passkeys, dialogs, file pickers, and downloads.
Its `passkey` and `securitykey` packages make and use passkeys, in software
and on a USB key, and `totp` makes an authenticator's codes; they keep
nothing.

**`backend/engine/browse.go`**: the agent's own tab, not a service's,
after Playwright MCP's tools, on Slipway's `Snapshot` and its actions: the
page as its accessibility tree, one element a line, each with a ref, and
clicks and keys by ref, made of real mouse and key events. Actions go to
the newest tab the agent's tab opened, such as a pop-up. A page's dialog
there waits for the agent's answer (`HandleDialog`).

**`backend/engine/events.go`**: what an action did, for the agent. Before
an action, Harbor marks each tab (Slipway's `Mark`) and the profile's
cookies (`CookieMark`); after it, it lists the requests, storage changes,
and cookie changes since, by name, in at most 20 lines.

**`backend/engine/session.go`**: one tab per service.

- `tabFor` opens the service's tab at its `url`, or reuses it.
- `load` injects the compiled service file as `window.__harbor`, unless the
  page has this version already. `invoke` calls a function in it, loading it
  again first if the page navigated.
- Calls to one service take turns. Calls to different services run at the
  same time.

**`backend/engine/engine.go`**: what callers use.

- `Call(service, tool, input)`: checks the input against `inputSchema`,
  gets you through the service's login and the tool's (see Logins), runs
  the tool, and checks the output against `outputSchema`. A tool without an
  `outputSchema` must still return an object. If the tool throws and a
  login's check now says no, Harbor gets you through it and runs the tool
  once more. A tool with `frame` runs in that frame (see Frames); files go
  in and out as in Files.
- `SignedIn(service)`: runs `login.check`. A service with no login is always
  signed in. A check that throws is asked again for up to ten seconds.
- `SignIn(service)`: gets you through the service's login. Calls to that
  service wait meanwhile.
- `Browse(service)`: shows the service's tab, where it is, until you click
  Done: to look around, or to do what Harbor cannot, such as add a passkey
  to an account you are signed in to.
- The live view (`view.go`): a row in `live_views` names the service's tab,
  and Harbor's window shows it (see UI); the window sends your mouse and
  keys to Slipway's browser itself. `Shown` is the tab and the prompt, for
  tests; `CloseView` cancels a sign-in, or ends browsing. The tabs the
  service's tab opened close when the view ends.
- `Answer`: your answer to a prompt the live view shows (`prompt.go`,
  `pages.go`), which the window reads from `live_views.prompt`.
- `SecurityKeys`: where Harbor finds security keys; tests set a key in
  software.

## Logins

A login (`login.go`) is a page, a `check`, and maybe a `run`: the
service's `login`, and a tool's own, for a site that asks you to confirm
it's you before an action, as a bank does before a transfer. Harbor gets
you through either the same way:

1. It runs `check`. If it passes, nothing more.
2. If there is a `run`, Harbor opens the login page and calls `run` on each
   page until `check` passes, for up to five pages: a username page, a
   password page, a code page. `run` types your saved login in with
   `fill(field, "username" | "password" | "code")`, from `"harbor"`.
3. Otherwise, or if `run` throws (as `fill` does when Harbor has nothing
   saved), Harbor shows you the page in the live view, from where `run`
   got to, until `check` passes, you cancel, or five minutes pass. The tab
   then goes back to the service's `url`. Signing in in the service's own
   tab keeps what a site keeps in sessionStorage.

So an agent's call, or one from a tool's Run button, to a signed-out
service shows you its login page and waits. Cancelling fails the call with
"sign in to X first".

A dashboard's call does not wait (`CallForDashboard`, which uses the
engine's `CallWithoutWaiting`): if Harbor cannot
sign in on its own, the call fails at once with a `SignInNeeded`, which
names the service, and the tool for a tool's login. The kit's card shows
it, or the dashboard's page lists it above the dashboard, with a button that
signs you in (`SignIn`), and then loads the dashboard again. Without this, a dashboard
that calls several signed-out services held a request open for each while
it waited, and Harbor's window (WebKit) has only about six: the live view's
frames and your clicks could not get through.

Saved logins (`saved.go`) are a site's username, password, and the secret
an authenticator app keeps, by site, in the `logins` table. Harbor makes
the codes (Slipway's `totp`, RFC 6238: SHA-1, 30 seconds, 6 digits). After you
sign in through the live view, Harbor offers to save the username and
password you typed; you add a login, or an authenticator secret, on the
Settings page.

`fill` works only while Harbor runs a `login.run`: any script on a page can
call it, and a tool's `run` or a stray script must not be able to type your
password into a field it reads. Harbor asks Chromium which site the calling
script belongs to (Slipway's `Expose`), and fills in that site's login
(`saved.go`). The same page script tells Harbor what you type into login
forms, to offer to save it.

A `check` runs while you sign in, every second, so it must not load the
login page: on Tidewater Bank, following the redirect to `/login` started
the sign-in over, and the form you were filling in expired. Use
`redirect: "manual"`.

## Frames

A tool with `frame: { site, page }` runs in the frame from `site` on the
page at `page`, which may be a function of the input. The page cannot read
a frame from another site; Maple Grove's grades are in one from GradeBook
Pro. Harbor opens the page, waits up to ten seconds for the frame, and
runs the tool there (Slipway's `Tab.Frame`): in the frame's own process, as a
target Harbor attaches to, for another site; in the page's process, in the
frame's own script context, for another origin of the same site. Its
`cookies` are the frame's site's.

## Files

A tool returns a file as a `File` or `Blob`, anywhere in its output. Harbor
keeps it in the `files` table and puts `{ file, name, type, size }` in its
place (`files.go`). A tool takes a file as an input property with
`format: "file"`, whose value is a file's id; its `run` gets a `File`. Files
cross from and to the page in base64, inside Harbor, but never in a tool's
JSON. MCP's `read_file` gives a file's content: text as text, an image as
an image. Harbor's own agent reads only text for now. Dashboards get a URL
for a file with `fileURL(file)`; the Run panel has a file picker for each
file input, and a Save button for each file in the output.

## Passwords and passkeys

Headless Chromium cannot show its password box or its passkey dialog. A
site that asks for an HTTP password, such as a router's admin page, got an
error page; a page that asked for a passkey waited for good. Harbor's
extension takes these requests instead, with two Chrome extension APIs:

- `webRequest.onAuthRequired`, with the `webRequestAuthProvider`
  permission, for HTTP passwords. Chrome gives the password only to the
  site that asked. The DevTools protocol's way, `Fetch.authRequired`,
  pauses every request of every tab.
- `webAuthenticationProxy`, for passkeys. Chrome first checks that the
  passkey's domain is the page's own, refuses a page that claims another
  site's, and adds the page's true origin. Harbor does not repeat that
  check, so cannot get it wrong.

The extension is Slipway's; it passes each request to the `HTTPAuth` and
`Passkey` hooks, which Harbor answers (`prompt.go`).

What Harbor answers:

- **An HTTP password.** One Harbor has saved for the site and realm is given
  at once, so tools run. Otherwise, while you sign in to that site, the live
  view asks you for it, and Harbor saves it. Outside a sign-in, the site
  gets no password: it shows its own refusal, and the service shows as
  signed out. A password the site refuses is asked for again, saying so.
- **Making a passkey.** While you sign in to that site, the live view asks
  whether to save one in Harbor, for the account the site names. Harbor
  makes a P-256 key (Slipway's `browser/passkey`) and saves it.
- **Signing in with a passkey.** While you sign in to that site, the live
  view lists Harbor's passkeys that the site takes, or says there are none.
  You can let Harbor use one without asking from then on, so a `login.run`
  that clicks the site's passkey button signs in on its own.
- **A security key**, such as a YubiKey, for either: see Security keys.
- Outside a sign-in to the page's site, in the live view or a `login.run`,
  a page gets no passkey: it throws `NotAllowedError`. So no page uses your
  passkeys without you.

Your click in the live view is the only check that you are there: Harbor
tells the site you were verified, as Touch ID or a PIN would. Anyone at
your unlocked computer could click it.

Harbor keeps the passwords and passkeys in its database, in
`http_passwords` and `passkeys`, in plain text, not in the Keychain or the
Secure Enclave: Harbor is not code-signed, which both need, and the
database works the same on every platform. The browser profile already
keeps your sign-ins no safer: chromedp starts Chromium with
`use-mock-keychain` and `password-store=basic`, so its cookies are
encrypted with a key anyone can look up.

To add a passkey to an account you are already signed in to, open the
service in Harbor's browser (`Browse`), and add it in the site's settings.

## Security keys

Headless Chromium cannot show the dialog Chrome talks to a security key
through, so Harbor talks to the key itself (Slipway's `browser/securitykey`), in Go,
with no library to install: CTAP2 over USB HID, with PIN protocol 2, or 1
for an older key. Only finding and opening the key differs between
platforms: IOKit on macOS (with cgo, so not in slipway's server mode, which
the e2e tests build), and `/dev/hidraw*` on Linux. The live view asks you
to plug the key in, for its PIN, and to touch it; Cancel stops it at any
step. A key with several passkeys for a site uses the one it lists first.
A key that speaks only U2F, the older protocol, cannot make passkeys.

Tested against a key in software (`securitykey/softkey`), and go-webauthn
as the site; not yet against a real key.

## Dialogs, files, and downloads

Headless Chromium shows none of these either, and a page that shows a
dialog stops until it is answered (`pages.go`).

- **A dialog**, an alert, confirm, prompt, or "leave this page?": in a live
  view, Harbor asks you over the page. Anywhere else, an alert is OK'd, a
  page is let go, and a question gets no.
- **A file picker**: in a live view, Harbor asks you for the files, and
  gives the page copies, kept in a temporary folder until Harbor quits.
  Anywhere else, the page gets none.
- **A download**: in a live view, the file goes to your Downloads folder,
  and Harbor says so. Anywhere else, it is dropped: a tool that downloads a
  file is not supported yet.

## Cookies

`login.check` and every tool's `run` get a context object as their last
argument:

```ts
check: async ({ cookies }) => cookies.some((c) => c.name === "session" && !c.expired)
async run(input, { cookies }) { ... }
```

`cookies` lists every cookie the browser would send to the service's `url`,
HttpOnly ones included: `name`, `domain`, `path`, `expires` (or null for a
session cookie), `expired`, `httpOnly`, and `secure`. A regular cookie also
has its `value`; an HttpOnly cookie does not, since the page's own scripts
are not meant to read it.

## UI

- A service with a login shows whether you are signed in, with a Sign in
  button. A call to a signed-out service signs in too (see Logins).
- A live view has a row in `live_views`, and the window shows it over the
  page as a small browser (`SignInBrowser.vue`): the service's tab, a tab
  bar of the tabs it opened, an address bar, for a sign-in link a site
  emailed you, back, forward, and reload, and Cancel, or Done when
  browsing. A tab the page opens comes to the front. Over the page, it
  shows what the site asks for: a password, a passkey, a security key's
  steps, a dialog, files, a download, or whether to save the login you
  typed. The tab is Slipway's `LiveView`, which streams frames as events,
  so it holds no request open.
- A service's page has "Open in Harbor's browser", which shows its tab
  until you click Done.
- The Settings page lists saved logins, to add, edit, and delete, and
  Harbor's passkeys, to delete or let Harbor use without asking.
- Each tool has a Run button. The input starts from the tool's required
  fields; Harbor shows the output, or the error.

## Tests

- `tests/dev/engine_test.go` drives the engine against a fake website
  with a real Chromium: signing in and out, cookies, keeping the tab,
  loading the service again after navigation, schema checks, errors, and
  calls taking turns.
- `tests/dev/tools_test.go` drives the Run panel and the sign-in status.
- `tests/dev/signin_test.go` signs in through the live view: input, a
  pop-up, an emailed link, and cancelling.
- `tests/dev/restart_test.go` quits Harbor's Chromium and starts a new
  one: a sign-in kept in a session cookie must survive.
- `tests/dev/prompts_test.go`: a site behind an HTTP password, a wrong
  password, a saved one, and a site that makes and takes passkeys, checked
  by go-webauthn as a real site would; and a page refused a passkey
  outside a sign-in.
- Slipway's `browser/passkey` and `browser/securitykey` are tested against
  go-webauthn without a browser; `prompts_test.go` also makes and uses a
  passkey on a key in software, through the live view. Slipway's own tests
  cover tabs, `Expose`, dialogs, files, and downloads.
- `tests/dev/logins_test.go`: a tool's own login, on its own and with
  you, a check that fails only once the tool tries, cancelling, and
  `Browse`.
- `tests/dev/saved_test.go`: signing in across three pages on its own,
  with you and then offering to save, with you for only the code, and
  `fill` refused in a tool's `run`.
- `tests/dev/frames_test.go` and `files_test.go`.
- The sign-in browser's tab bar and address bar are tested in the frontend
  unit tests, against a mock browser.

## Not in this piece

The interaction recorder, mouse and keyboard input outside a sign-in, the event log,
`auto_run_js`, an overlay in the page, passkeys on a phone, reading
codes and links from email, and images for Harbor's own agent.
