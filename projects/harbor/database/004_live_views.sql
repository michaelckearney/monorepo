-- The live views Harbor's window shows: a service's tab in Harbor's browser,
-- shown so you can sign in, confirm it's you before a tool runs, or look
-- around. However one starts (a service's page, a dashboard, a tool call, or
-- an agent), its row makes the window show it. heading is the view's own,
-- such as "Sign in to Tidewater Bank"; browse is 1 for one you opened to
-- look around, which ends when you are done. A row lives as long as its
-- view; one left by a crash is cleared when Harbor starts.
CREATE TABLE live_views (
    service TEXT PRIMARY KEY,
    heading TEXT NOT NULL,
    browse  INTEGER NOT NULL DEFAULT 0,
    started TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
