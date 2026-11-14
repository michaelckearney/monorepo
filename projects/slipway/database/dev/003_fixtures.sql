-- A dev build starts with the fixtures (backend/fixture): the made-up
-- server's address, and the token the made-up GitHub takes. They are
-- fixture.Server and fixture.Token.
INSERT INTO settings (key, value) VALUES ('server', 'http://server.fixture.localhost');
INSERT INTO settings (key, value) VALUES ('github_token', 'fixture-token');
