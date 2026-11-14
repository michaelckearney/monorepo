-- Each service's favicon, found as Chromium finds a tab's (backend/favicon).
-- Keyed by origin, as new URL(url).origin gives it, so services on one site
-- share one fetch. A cache: losing it costs a refetch, not your data.
--
-- status is ok (bytes are there), none (the site answered and has no icon),
-- or error (no attempt has worked yet). A failure after a success keeps the
-- icon and only moves the retry: a network blip must not take it away.
-- The dark_ columns are set only when the site declares a different icon for
-- prefers-color-scheme: dark. etag, last_modified, and the icon URLs make a
-- refresh a conditional request. Times are RFC 3339, in UTC.
CREATE TABLE service_icons (
    origin             TEXT PRIMARY KEY,
    status             TEXT NOT NULL CHECK (status IN ('ok', 'none', 'error')),
    icon_url           TEXT,
    mime_type          TEXT,
    bytes              BLOB,
    etag               TEXT,
    last_modified      TEXT,
    dark_icon_url      TEXT,
    dark_mime_type     TEXT,
    dark_bytes         BLOB,
    dark_etag          TEXT,
    dark_last_modified TEXT,
    fetched_at         TEXT NOT NULL,
    next_attempt_at    TEXT NOT NULL,
    failure_count      INTEGER NOT NULL DEFAULT 0,
    last_error         TEXT
);
