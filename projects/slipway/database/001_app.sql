-- What the app keeps between launches. Everything else is asked of the
-- server's API, of gh, and of harness run, when it is shown.
CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

-- Where each project is cloned on this computer.
CREATE TABLE clones (
	repo TEXT PRIMARY KEY,
	path TEXT NOT NULL
);

-- What the bots did, from the server's live stream. The frontend reads it
-- with a live query, so a new line shows as it arrives.
CREATE TABLE lines (
	id INTEGER PRIMARY KEY,
	repo TEXT NOT NULL,
	number INTEGER NOT NULL,
	time TEXT NOT NULL,
	kind TEXT NOT NULL,
	text TEXT NOT NULL,
	UNIQUE (repo, number, time, kind)
);
CREATE INDEX lines_by_thread ON lines (repo, number, id);
