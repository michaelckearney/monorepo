# Harbor fixtures

Made-up web apps for testing Harbor, in package `fixture`. Each app is small,
but signs you in and serves its data the way a real site of its kind does,
so an agent has to work out the same things it would on the real web. The
scenarios, and which app covers which, are in `docs/fixtures.md`.

Everything is about one made-up person, Alex Morgan. Every site signs in
with the email `alex@example.localhost` and the password
`password123`.

They run in Harbor's own process, from `fixture.Start` to `Stop`: a dev
build (`harness run -mode=dev`, and the e2e tests' dev app) serves them
from launch until it quits, and a benchmark run from `harness cli` serves
its own. A change here restarts them with Harbor.

Every site is at `https://<name>.localhost:8443`. The fixtures listen on
port 8443 when it is free, and on another port otherwise, as for a second
worktree's Harbor or a benchmark beside the dev app; Harbor's browsers
(`fixture.Browser`) and Go clients (`fixture.Transport`) send the URLs
there. When they have port 8443, <https://fixtures.localhost:8443> opens
in a browser of your own too. It lists every site, and how to sign in to
each.

## The sites

| Site | Host | Signs in with |
|---|---|---|
| Tidewater Bank | tidewater.localhost | `alex@example.localhost`, the password, then a TOTP code |
| Brightline Energy | brightline.localhost, api.brightline.localhost | A link emailed to `alex@example.localhost` |
| Parcel | parcel.localhost | `alex@example.localhost`, the password, then an emailed code |
| Clearwater Health | clearwater.localhost | `alex@example.localhost` and the password; choose a person; accept the terms |
| Maple Grove Schools | maplegrove.localhost, sso.maplegrove.localhost, gradebook.localhost | The district's SAML sign-in: `alex@example.localhost` and the password |
| Tasklane | tasks.northwind.localhost | Northwind ID (OIDC with PKCE): `alex@example.localhost` and the password, then consent |
| Payday | payroll.northwind.localhost | Northwind ID: no form if already signed in |
| Huddle | huddle.localhost | `alex@example.localhost` and the password, in a popup |
| Shoebox | shoebox.localhost, files.shoebox.localhost | `alex@example.localhost` and the password, then a passkey |
| Homebase | home.localhost, login.home.localhost | `alex@example.localhost` and the password, at the login portal |
| Homebase router | router.home.localhost | HTTP Basic: `alex@example.localhost` and the password |
| Mail | mail.localhost | Nothing: every email the sites send |

Tidewater Bank's TOTP secret is `TIDEWATERBANKHARBORFIXTURESALEXQ` (SHA-1,
6 digits, 30 seconds).

Each start's data comes from the files in `apps/*/data` again. What a
sign-in leaves is kept, in `fixtures/fixtures.db` in the data folder
(`.desktop/data/dev` under `harness run`): the sites' sessions, the tokens
that stand for a sign-in, their keys, Shoebox's passkeys, and the CA. So
Harbor restarting, as after each change to the backend, does not sign you
out. A sign-in still in progress, an emailed code, or the mail itself, is
not kept. Delete the file for a fresh start.

## Favicons

Each site declares its icon as a different kind of real site does, so
Harbor's favicon lookup is tried on each kind:

| Site | Declares | The icon is |
|---|---|---|
| Tidewater Bank | an ICO `sizes="32x32"`, and an SVG | the SVG |
| Brightline Energy | PNGs at 16, 32, and 192 px, and an `apple-touch-icon` | the 32 px PNG |
| Parcel | nothing | `/favicon.ico` |
| Clearwater Health | one SVG for `prefers-color-scheme: light`, one for dark | both |
| Maple Grove Schools | `rel="shortcut icon"`, a GIF, by a relative path | the GIF |
| Tasklane | a `mask-icon`, and an SVG; `/favicon.ico` is the app's HTML | the SVG |
| Payday | only an `apple-touch-icon` | the touch icon |
| Huddle | a WebP | the WebP |
| Shoebox | a 512 px PNG, and an SVG `sizes="any"`, on its sign-in page | the SVG |
| Homebase | nothing; every page is the login portal until you sign in | the portal's SVG |
| Homebase router | nothing; HTTP Basic | none |

The SVGs are the originals; the PNG, ICO, GIF, and WebP files were made
from them with Inkscape and Pillow.

## Host names

Every site has its own name under `.localhost`, and each organization has
its own domain, so its sites are a different site from every other
organization's, as on the real web. Apps on one host with different ports
would share cookies, and hide what is being tested.

Harbor's browsers, and a Go client with `fixture.Transport`, send these
names to where the fixtures listen. Other clients do not: macOS does not
resolve `*.localhost`, and port 8443 may not be the fixtures'. curl can be
told: `curl -k --connect-to ::127.0.0.1:<port> https://tidewater.localhost:8443/`,
with the port Harbor logs as it starts them.

## HTTPS

Every site is HTTPS, as real ones are: `Secure` and `__Host-` cookies need
it, and so do passkeys. The certificates come from the fixtures' own CA,
made once and kept in their store, limited to localhost and the names
under it. The fixtures make a certificate for each name as it is asked
for, so a new site needs nothing done. Harbor's browsers and
`fixture.Transport` trust the CA. A browser of your own does not: click
through the warning, or trust `https://fixtures.localhost:8443/ca.pem`,
which lasts until you delete the store.

## Sign-in services

- **Northwind ID** (`id.northwind.localhost`): OIDC for Tasklane and Payday.
- **Homebase's portal** (`login.home.localhost`): forward-auth in front of
  home.localhost. The front end asks it about every request to Homebase, as
  Traefik or Caddy would.

Both are `portal/`, which does what Authelia did for them, at Authelia's
paths, with its cookies and its timeouts: Authelia has no build for macOS.
It is only what the apps use: the authorization code flow with PKCE,
consent, `prompt=login`, and no ID token, since the apps ask userinfo.

The SAML sign-in for Maple Grove is crewjam/saml.

## Layout

- `fixture.go`, `tls.go`: Start and Stop, the front end, how Harbor
  reaches the sites, and the CA.
- `sites.go`: the host table, and the two portals' settings.
- `hosts/`: every host name, and the port in their URLs.
- `apps/<name>/`: one package per site, with its pages and its `data/`.
  The package comment lists the scenarios it covers.
- `portal/`: Northwind ID and Homebase's portal.
- `site/`: what the apps share: the store, sessions, pages, JWTs, TOTP,
  PDFs.
- `mail/`: the mail catcher.
- `index/`: the front page.
- `internal/fake/`: a browser for the apps' tests.

`harness test -unit` tests every app, the sign-ins through both portals,
and a sign-in lasting a restart.
