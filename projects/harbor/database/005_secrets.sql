-- What you sign in to sites with, through Harbor's browser. Kept here, in
-- plain text, not in the Keychain: Harbor is not code-signed, and this works
-- the same on every platform. The browser profile beside this database keeps
-- your sign-ins no safer.

-- Passwords that sites ask for over HTTP, such as a router's admin page, by
-- site (such as https://router.home.localhost:8443) and realm, the site's
-- name for what the password opens.
CREATE TABLE http_passwords (
    origin   TEXT NOT NULL,
    realm    TEXT NOT NULL,
    username TEXT NOT NULL,
    password TEXT NOT NULL,
    PRIMARY KEY (origin, realm)
);

-- Harbor's own passkeys. id and user_id are as the site sees them, in
-- base64url; private_key is the P-256 key, PKCS #8, in base64. auto is 1
-- when you let Harbor use the passkey without asking you, while it signs in
-- to the site.
CREATE TABLE passkeys (
    id                TEXT PRIMARY KEY,
    rp_id             TEXT NOT NULL,
    user_id           TEXT NOT NULL,
    user_name         TEXT NOT NULL,
    user_display_name TEXT NOT NULL,
    private_key       TEXT NOT NULL,
    auto              INTEGER NOT NULL DEFAULT 0,
    created           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX passkeys_by_site ON passkeys (rp_id);

-- Logins Harbor types in for you when a site's session expires, by site
-- (such as https://tidewater.localhost:8443). totp_secret is the secret an
-- authenticator app keeps, in base32, for sites that ask for a code; Harbor
-- makes the code. Empty if the site asks for none.
CREATE TABLE logins (
    origin      TEXT PRIMARY KEY,
    username    TEXT NOT NULL,
    password    TEXT NOT NULL,
    totp_secret TEXT NOT NULL DEFAULT ''
);
