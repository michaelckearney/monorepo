-- The files services' tools take and return (backend/engine/files.go): a
-- statement a bank tool downloads, a photo one uploads. A tool's output names
-- a file by id. The bytes are kept here, so Harbor keeps no folder of its own
-- in step with the database.
CREATE TABLE files (
    id      TEXT PRIMARY KEY,
    name    TEXT NOT NULL,
    type    TEXT NOT NULL,
    size    INTEGER NOT NULL,
    data    BLOB NOT NULL,
    created TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
