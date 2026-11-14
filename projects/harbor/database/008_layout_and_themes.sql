-- The logins Harbor last found you are not through: a service's (tool '')
-- or one tool's. A row is written when a call, a check, or a sign-in finds
-- you signed out, and deleted when one finds you through, so the top bar
-- can say which services need you without checking each one. heading and
-- message are the engine's, such as "Sign in to Tidewater Bank".
CREATE TABLE sign_ins_needed (
    service TEXT NOT NULL,
    tool    TEXT NOT NULL DEFAULT '',
    heading TEXT NOT NULL,
    message TEXT NOT NULL,
    PRIMARY KEY (service, tool)
);

-- The services each dashboard called the last time it ran, so the sidebar
-- can mark a dashboard whose service needs you. Saving a dashboard forgets
-- them, since its calls may have changed.
CREATE TABLE dashboard_services (
    dashboard_id INTEGER NOT NULL REFERENCES dashboards (id) ON DELETE CASCADE,
    service      TEXT NOT NULL,
    PRIMARY KEY (dashboard_id, service)
);

-- Themes you added. tokens is JSON, {"light": {...}, "dark": {...}}: CSS
-- variables by name without the dashes, such as "primary". The built-in
-- themes are in the frontend (themes.ts). settings gains theme, the theme in
-- use; mode, which is light, dark, or system; last_dashboard, the id of the
-- dashboard the window opens to; and agent_panel, which is open, wide, or
-- closed.
CREATE TABLE themes (
    name   TEXT PRIMARY KEY,
    tokens TEXT NOT NULL CHECK (json_valid(tokens))
);
