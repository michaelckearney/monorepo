-- Services and dashboards keep their source exactly as written, comments and
-- all. Harbor never rewrites it.

-- A service is one TypeScript file. manifest is what Harbor read from it, as
-- JSON: the name, title, URL, and tool definitions. Everything that lists or
-- describes a service reads the manifest, so nothing has to run the file
-- again. name is copied from the manifest, so it can be looked up.
--
-- manifest is a copy of what the source says, which Harbor keeps for itself:
-- it is written on save, and Harbor reads any that is NULL when it starts
-- (manifests.go). So a migration inserts only a name and source, and one for
-- a Harbor that reads manifests differently sets them all to NULL.
-- manifest_error is why the last read failed, with manifest left NULL.
CREATE TABLE services (
    id             INTEGER PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    source         TEXT NOT NULL,
    manifest       TEXT CHECK (manifest IS NULL OR (typeof(manifest) = 'text' AND json_valid(manifest))),
    manifest_error TEXT,
    created_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at     TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- A dashboard is one Vue file. It has no identity outside Harbor, so its name
-- is set in the UI rather than written in the file.
CREATE TABLE dashboards (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    source     TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
