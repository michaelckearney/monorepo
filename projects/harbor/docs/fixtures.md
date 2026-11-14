# Fixture sites

Local web apps for testing, built for Harbor. They replace the third-party
stack the old Harbor used (Gitea, Grafana, Redmine, MinIO, GreenMail,
Roundcube). Those were chosen because they were available; these are chosen
for what Harbor must handle.

They are in `backend/fixture/`, whose README says how they run and how to sign in
to each.

## What they test

An agent browses a site with the `browser_*` tools, then writes a service
whose tools and `login.check` work. Each scenario below has three
outcomes to check:

1. The tools return the right data.
2. `login.check` is right before sign-in, during it, and after it.
3. A tool called after the session ends says to sign in, and does not return
   wrong data. Agents fail this one most often.

A green run says Harbor does what the agent decides correctly. Whether the
agent decides well is a question for benchmarks, on the same sites.

## How they run

- In Harbor's own process, on `https://*.localhost:8443`: a dev build
  serves them from launch until it quits, and a benchmark run from the CLI
  serves its own. They listen on port 8443 when it is free, and on another
  otherwise, as for a second worktree's Harbor; Harbor's browsers and Go
  clients (`fixture.Transport`) send the URLs there. Northwind ID and
  Homebase's portal are `backend/fixture/portal`, which does what Authelia
  did for them; Maple Grove's district sign-in is SAML from crewjam/saml.
- Each organization has its own domain under `.localhost`, such as
  `tidewater.localhost` and `tasks.northwind.localhost`. Separate domains
  make the apps separate
  sites, as on the real web: cookies, SameSite, and partitioned iframes
  behave as they would between real companies. Under one parent domain they
  would all be one site.
- Every site is HTTPS, as `__Host-` cookies and passkeys need, with a
  certificate from the fixtures' own CA. Harbor's browsers and
  `fixture.Transport` trust it.
- The seed data is in files, with fixed dates, and each start begins from
  it again. What a sign-in leaves, its sessions, tokens, keys, and passkeys,
  is in a SQLite store of the fixtures' own, in the data folder, so Harbor
  restarting does not sign you out. Sessions and timeouts use the real
  clock.
- Northwind ID has no logout endpoint for OIDC apps, as Authelia has none,
  so Tasklane and Payday ask it whether the browser is still signed in each
  time they refresh a token. That is how signing out of one signs you out
  of the other.

## The scenarios

### How the page gets its data

| # | Scenario | The agent must find | If it does not |
|---|---|---|---|
| D1 | Server-rendered HTML, no API | Fetch the page, parse the DOM | Looks for an API that does not exist |
| D2 | JSON REST, same origin, with a cookie | Endpoints, parameters | (the baseline) |
| D3 | JSON API on another subdomain, CORS with credentials | The API's origin; `credentials: "include"` | 401, or a CORS error |
| D4 | GraphQL | Query, variables, operation name | Wrong fields, or 400 |
| D5 | GraphQL with persisted queries (hash only) | The hash, from the page's requests or code | "PersistedQueryNotFound" |
| D6 | Data in the page (`__NEXT_DATA__`, JSON in a `<script>`, hydration state) | The embedded state | Misses it, or scrapes text |
| D7 | Batched RPC (tRPC, JSON-RPC, `?batch=1`) | The envelope, and the indexes in the reply | Malformed requests |
| D8 | Packed bodies (protobuf, gRPC-web, base64 JSON) | How to decode, or a JSON way in | Unreadable output |
| D9 | Anti-hijacking prefix (`)]}'`, `while(1);`) | Strip it before parsing | Parse errors |
| D10 | Required headers (`X-Requested-With`, client version, a key from page config) | Which headers the server insists on | 403, or a different reply |
| D11 | Offset pagination | Page size, the last page | Part of the data |
| D12 | Cursor pagination, or a `Link` header | Follow the cursor | Loops, or stops at page 1 |
| D13 | Infinite scroll | The request behind the scroll | The first 20 items only |
| D14 | List, then detail (N+1) | Two calls, joined by id | Missing fields |
| D15 | Async job: start an export, poll, download | The job's steps | Returns "job started" as data |
| D16 | Live updates over WebSocket | A REST snapshot, or the socket | No data, or a hang |
| D17 | Server-sent events, long polling | As D16 | A hang |
| D18 | File download (CSV, PDF, blob URL) | The bytes, as useful content | HTML, or a blob URL |
| D19 | File upload (multipart) | FormData, field names | 400 |
| D20 | Data only in a cross-origin iframe | Work in the iframe's origin | Empty data |
| D21 | Data only drawn (canvas) or in shadow DOM | Shadow roots, or the data's real source | Nothing found |
| D22 | Rate limits (429, `Retry-After`) | Back off, and say so | Hammering, or a vague error |
| D23 | Locale formats (dates, currency, time zones) | Normalized values | Wrong numbers or dates |

### Where the browser keeps credentials

| # | Scenario | The agent must find | `login.check` uses |
|---|---|---|---|
| S1 | HttpOnly session cookie (opaque id) | Requests carry it | Cookie names |
| S2 | Readable cookie, copied into a header by JavaScript | Copy it as the site does | Cookie and header |
| S3 | JWT in a cookie | As S1; `exp` inside | Cookie, maybe its expiry |
| S4 | Access token in localStorage | `Authorization: Bearer` | localStorage, not cookies |
| S5 | Token in sessionStorage (one tab) | It is gone in a new page | A real request |
| S6 | Token in memory only, got again on load by a refresh call | Call refresh first | A probe request |
| S7 | Token in IndexedDB (Firebase-style) | Read IndexedDB | IndexedDB |
| S8 | Cookie on the parent domain, shared by subdomains | Which host to call | The right domain's cookie |
| S9 | Path-scoped cookies | Call under that path | The path |
| S10 | `SameSite=Strict`, `__Host-`, `Secure` | They break on cross-site or http calls | |
| S11 | Partitioned cookies (CHIPS) in an iframe | Cookies differ by top-level site | |
| S12 | HTTP Basic, kept by the browser | No cookie at all | A probe request |

### How the server checks them

| # | Scenario | The tools must |
|---|---|---|
| V1 | Server-side session lookup | (the baseline) |
| V2 | Idle timeout, renewed on use | Say to sign in when it has ended |
| V3 | Absolute timeout | As V2, with no renewal |
| V4 | Short access token (about 5 minutes) and a refresh token | Refresh first, as the site does |
| V5 | Refresh-token rotation: each one works once | Not refresh alongside the page, which signs the user out |
| V6 | CSRF token from a `<meta>` tag or hidden field | Get it before a write |
| V7 | CSRF double-submit cookie | Copy the cookie into the header |
| V8 | Origin or Referer check | Call from the site's own page |
| V9 | Session bound to the browser or IP | Work only from Harbor's browser |
| V10 | One session per user: a new sign-in ends the old | Say so, and not loop on sign-in |
| V11 | Signed requests (HMAC of method, path, time; like AWS SigV4) | Sign as the site does |
| V12 | Pre-signed URLs | Ask for the URL, then fetch it |
| V13 | Sign in again for sensitive actions | Say to confirm the password |
| V14 | Signed out elsewhere | Say to sign in |

### How you sign in

| # | Scenario | Harbor and the agent must |
|---|---|---|
| A1 | One-page form | Set `login.url`; a check that changes after sign-in |
| A2 | Form with CSRF and hidden fields | As A1 |
| A3 | Username, then password | Use the first step as `login.url` |
| A4 | Sign-in by JavaScript; a token in the reply | Check where the token is kept (S4 to S7) |
| A5 | HTTP Basic | Probe with a request |
| A6 | TOTP second step | Not pass the check after step 1 |
| A7 | Email code, or magic link | Wait on mail (the fixture mail catcher) |
| A8 | Passkey (WebAuthn) | Use Chromium's virtual authenticator in tests |
| A9 | "Remember this device" | Tell the device cookie from the session |
| A10 | OIDC authorization code, server-side app | Follow redirects across sites; check on the app |
| A11 | OIDC with PKCE, single-page app | Tokens in storage, not cookies |
| A12 | Consent screen | One more stop in the chain |
| A13 | SSO: a second app on the same provider | Sign in with no form |
| A14 | Single logout | Both apps signed out |
| A15 | SAML, POST binding (a form that submits itself) | A form post between sites |
| A16 | A login portal in front of the app (forward-auth) | `login.url` on another host |
| A17 | Sign-in in a popup (`window.open`) | Harbor's browser allows popups |
| A18 | Sign-in in an iframe | Third-party cookie rules |
| A19 | Choose an account, org, or person after sign-in | Not pass the check early |
| A20 | Interstitial: terms, expired password, new device | Not pass the check early |
| A21 | Locked out after failed attempts | Say so, and not retry |
| A22 | Back to the first page after sign-in (`?next=`) | Tools work from deep links |

Not covered, on purpose: CAPTCHA and bot detection (Harbor hands these to
the user; it does not solve them), device-code sign-in, API tokens a user
pastes in, client certificates, Kerberos, and NTLM.

## The apps

Sites people would use Harbor for: everyday ones with useful data and no API
they can use. Real sites of each kind tend to use these patterns: banks have
short timeouts and re-authentication, school portals embed iframes.

All the seed data is about one made-up person, Alex: their bank, home, health,
children's school, and their job at Northwind. So dashboards across apps make
sense: bills due, or the week ahead.

### Home

**Tidewater Bank** (`bank`): accounts, transactions, statements, transfers.
Harbor would list transactions, download statements, flag large charges, and
move money (asking first).
D1, D11, D18, D23 · S1, S10 · V2 (5-minute idle timeout), V6, V8, V10, V13 ·
A3, A6, A9, A20, A21, A22

**Brightline Energy** (`energy`): bills, meter usage, autopay. Harbor would
show the bill due and its amount, monthly usage, and export usage.
D3 (usage API on `api.energy`), D15, D21 (usage chart in a canvas) · S8 · A7
(magic link)

**Parcel** (`shop`): an online shop. Harbor would list orders, track
deliveries, and start a return.
D6, D13, D14 · S2 · V7 · A1, A2, A7 (email code)

**Clearwater Health** (`health`): a patient portal. Harbor would show
appointments, lab results, and messages from the doctor.
D9, D10 · V3 · A19 (yourself or a family member), A20 (new terms)

**Maple Grove Schools** (`school`): a parent portal. Harbor would show each
child's grades, assignments due, and school events.
D20 (gradebook in an iframe from another origin) · S11 · A15 (district
sign-in over SAML), A18, A19 (choose a child)

### Work, at Northwind

**Tasklane** (`tasks`): an issue tracker. Harbor would list my open issues,
create and update them, and show a sprint.
D4, D5, D12, D16 · S4 · V4, V5 · A11, A12 (Northwind ID)

**Payday** (`payroll`): HR and payroll. Harbor would get payslips, the
time-off balance, and request time off.
D7, D8 · S6 · V13 (to change bank details) · A10, A13 (no form after
Tasklane), A14

**Huddle** (`chat`): team chat. Harbor would show unread mentions, search,
and post a message.
D17, D22 · S7 · V14 · A4, A17

### Personal tools

**Shoebox** (`photos`): photo and file storage. Harbor would list albums,
download, upload, and make share links.
D18, D19 · S3 · V11, V12 · A8

**Homebase** (`home`): a self-hosted home server: router, disks, sensors.
Harbor would list connected devices, disk space, and sensor readings.
D17 (readings) · S9, S12 · V9 · A5 (the router page), A16

The other rows (D2, V1, and so on) are in most apps.

### Sign-in services

| Service | Software | For |
|---|---|---|
| Northwind ID, the company's SSO | `backend/fixture/portal`, OIDC | Tasklane, Payday; consent and single logout |
| Homebase portal | `backend/fixture/portal`, forward-auth | Homebase |
| Maple Grove district sign-in | crewjam/saml, SAML | The school portal |
| Mail | A small mail catcher of our own | Magic links and email codes |
