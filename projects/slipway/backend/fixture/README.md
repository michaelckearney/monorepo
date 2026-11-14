# Slipway's fixtures

A made-up server and a made-up GitHub, in package `fixture`, which Slipway
Dev asks instead of the real ones. `harness run`, the e2e tests
(`tests/dev/`), and `harness record` all run Slipway Dev, so each shows the
same made-up monorepo on any machine, with no tailnet and no GitHub
account.

They run in the app's own process, from `fixture.Start` to `Stop`, on a
port of their own. The app's transport sends its requests for
`api.github.com`, `server.fixture.localhost`, and GitHub's signed links to
attachments, `private-user-images.githubusercontent.com`, there. The dev database
starts with the server's address set to `http://server.fixture.localhost` and
the token set to `fixture-token` (`database/dev/003_fixtures.sql`), so the
app's own code asks the fixtures as it asks the real ones: the same
GraphQL queries and REST calls, the same server API, and the same answers.

## The monorepo

`example/monorepo`, with the projects `harbor` and `slipway`. You are
`alex-morgan`; the bots are the GitHub App `slipway-bot`.

| # | | What it shows |
|---|---|---|
| 11 | issue, project:harbor | a thread that is idle, its pull request ready |
| 12 | pull request, closes #11 | checks passed |
| 13 | issue, project:slipway | a thread whose turn is running (Claude Code); the bot's comment attaches a screenshot |
| 14 | draft pull request, closes #13 | a check in progress |
| 15 | issue, project:harbor | a thread waiting for you: the bot asked a question |
| 16 | issue, framework | no thread yet |
| 18 | pull request, no scope | yours, a check failed |
| 19 | issue, no scope | a note no bot works |

What you do changes them, until the app quits:

- a reply is a comment, and starts the thread's bot on a short turn, which
  the live stream shows
- a new issue is added, and its scope label made if the repository has
  none
- Another turn, and Stop turn, change the thread as the server would

Each start is the same data again, with every time as long ago as it was:
`seed` in `data.go`. The made-up GitHub refuses any token but
`fixture-token` with GitHub's 401, so a wrong token shows as it would.

## Adding to them

A change to what the app asks the server or GitHub needs the fixtures to
answer it too: the same paths, the same JSON, as the real ones write it.
Copy the shape from a real answer (`gh api graphql`, or the server's API on
the tailnet), not from the app's own types. `TestTheAppReadsAndWritesTheFixtures`
in `backend/` drives the app's code against them.
