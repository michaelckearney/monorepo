-- Harbor's own settings. provider is the name of the provider the agent uses;
-- browser_visible is "true" when Harbor's browser always has a window.
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- What the user filled in for each provider, such as its API key. A setting
-- with no row has its default.
CREATE TABLE provider_settings (
    provider TEXT NOT NULL,
    key      TEXT NOT NULL,
    value    TEXT NOT NULL,
    PRIMARY KEY (provider, key)
);
