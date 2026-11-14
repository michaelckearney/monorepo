# The Slipway app

A desktop app, on the desktop framework (`framework/`), for watching
the bots: it reads each server's harness API on the tailnet, asks GitHub's
API with your own token, and runs a monorepo's projects on this computer with
`harness run`. The README says what it does.

- `harness test` here is lint, unit tests, then e2e tests.
- The dev build, which the e2e tests and `harness record` run, asks the
  made-up server and GitHub in `backend/fixture`. Show a change with them:
  what the app reads from the server or GitHub, they must answer as the real
  ones do, so add to them what a change needs.
- The harness's API is versioned (`apiVersion` in `backend/server.go`): a
  change to what it reads from the API is a change to the harness too.
