# The Slipway app

One desktop app for the monorepos the bots work on. Each is a monorepo
whose harness serve runs on the server the app is pointed at: the app adds
none of its own. Settings, at the bottom of the sidebar, has the server's
address, your GitHub token, and the theme, and a dot when the server or
GitHub needs you.

Each project is a list of items. An item is one piece of work, shown as
one: an issue, the pull request that answers it, and the bot's thread on
it. A pull request the bot did not open joins the issue it says it closes;
one that closes no open issue is an item of its own. The default branch is
an item too, at the top. The sidebar has a section for each project of the
monorepo, then its framework, each with its items under it, by their scope
label (`project:<name>` or `framework`), and a + for a new issue for it.
Items with no scope label, which no bot works, show under no scope. Each
section folds, and a folded one keeps a dot when an item in it waits for
you.

An item is one timeline, in the order things happened: the issue, the
pull request's opening, the comments on both, and the bot's turns among
them. An image attached on GitHub shows, though a private repository's
needs GitHub's credentials: the app fetches it with your token, from the
signed link in GitHub's rendering of the conversation. A turn shows the bot's messages, and each run of steps as one line
that opens to the steps and what each gave back, the way Harbor's agent
panel shows a chat; the bot's reasoning is left out, and its whole log is
a button away. Above the timeline, one line says what the bot is doing,
the pull request and its checks, and whether it runs on this computer,
with Run here when it does not. A reply or a new issue comes from your
account, not from the bots' GitHub App. New issue asks what the issue is
for, a project or the framework, and gives it that label; one for nothing
is a note no bot works.

The App output panel, on the right of the window, shows what runs on this
computer, whichever item you are looking at. It starts collapsed to a strip:
open it there, with ⌘J, or from Runs here on an item. Both side panels
resize by dragging their inner edge, and collapse (⌘B for the sidebar). Each project of a
monorepo runs on its own, beside the others, one branch at a time: the
picker lists each project with its default branch and the pull requests for
it, and picking one runs it in place of that project's branch, in the same
window, without closing it first. The panel shows the output of the
project you picked last, and the project's modes, from `harness modes`,
pick which mode it runs in. The window's title says what it runs, such as
`Slipway Dev — #18 fix: … · harness/17`, so a branch's window is never
taken for the app you started it from. The app keeps its own clone of each
monorepo, in `clones/` in its data folder, cloned with your GitHub token
the first time you run one of its projects, and runs each project with
`harness run -p <project> -mode=<mode>`; the clone's default branch is
brought up to date only when none of its projects runs. Your own
checkouts are never touched. Running a bot's pull request runs the project
its issue is for, and a pull request that fails to start says why under
its item. The harness is the version the clone's `harness.yaml` pins,
which the app installs with `go install` into `harness/` in its data
folder the first time.

A released app installs each new release itself, then offers to open
again: the monorepo's `slipway/v*` releases, with the app zipped for each
platform. Slipway Dev, or any build that is not a release, only says that
one is out. The app a release replaced is kept beside it, as
`Slipway.previous.app`.

The UI is Harbor's: the framework's shared frontend (`framework/ui`,
imported as `@ui`), with its design system, components, and built-in
themes, so a change to it reaches both apps.

It is a project of the desktop framework, built from the framework in this
monorepo (`replace ../../framework` in `go.mod`).

```bash
harness run          # develop it: Slipway Dev
harness test         # lint, unit tests, then e2e tests
harness build        # build/darwin-arm64/Slipway.app and the rest
```

Slipway Dev, and so the e2e tests and `harness record`, asks a made-up
server and a made-up GitHub, in `backend/fixture`, and never the real ones:
see its README.

It needs:

- the server's API address, such as `http://server:7420`, set in the app.
  It is reached over the tailnet, and answers only the Tailscale logins in
  `api.allow` of the monorepo's harness.yaml
- a GitHub token, set in the app: a fine-grained token for the monorepos,
  with read and write on issues, and read on contents, pull requests,
  checks, and commit statuses. It reads and replies as you, clones the
  monorepos, and downloads the app's releases. `harness run` gets it too,
  as `GH_TOKEN`, for its list of pull requests
- to run a project here, Go and Node: the app installs the harness each
  monorepo pins

What it keeps, in `~/.slipway-app/data/prod/`: in `app.db`, the server's
address, your GitHub token, and the last lines of each bot's turns; in `clones/`, its clone of
each monorepo you have run; in `harness/`, each harness version they pin.
Everything else it asks for when it is shown.
