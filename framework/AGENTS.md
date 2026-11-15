# The desktop framework

Desktop apps with a Go backend and a Vue + TypeScript frontend, on Wails v3.
The README is the reference for a project's layout and what each verb does.
The projects that use it are in `projects/`: see `harness.yaml`.
[DESIGN.md](DESIGN.md) is the design philosophy every app shares: read it
before any change to UI, in `ui/` or in a project's frontend.

## Layout

- `framework.yaml`, `cmd/desktop/`: the framework's command, which the
  harness runs for each verb. `internal/`: what the verbs do; the README's
  Layout lists the packages.
- `desktop.go`, `data.go`, `generated/`, `browser/`, `e2e/`: the library a
  project imports, as package `desktop`.
- `ui/`: the frontend every app shares, imported as `@ui`: the design
  system, themes, and components. A change to it changes every app, so
  check each one that uses what you changed.

## Tests

- `harness test` here runs the framework's own Go tests, then each project's
  whole test. It takes minutes; run it once before you are done.
- While you work, run what covers your change: `harness test -run <regexp>`
  here passes `-run` to `go test` on the framework's packages, or
  `harness test -p <project> -unit` for one project.
- Read the tests already there to learn what a test can use, rather than
  writing a throwaway test to find out: each run of the suite costs minutes.

## Changes

- A breaking change here lands with the changes every project needs, in the
  same pull request, so `harness test` here passes as a whole.
- Do not weaken a test to make it pass.

## Evidence

Record every change a person can see in an app with `harness record` before
`harness done`, and check the recording yourself. For a change nobody can
see, such as the backend alone or tests, record nothing and say so in the
pull request's description. The README's `harness record` section has every
command.

- `harness record start` builds and starts the app and lists what is on
  screen. Act on it with the names from that list.
- `harness record see "<text>"` for each thing the change should put on
  screen, and `harness record shot "<what it shows>"` at each state that
  matters.
- `harness record stop` replays the steps on a fresh app and prints the path
  of the video, of each screenshot with its caption, and of the report. If
  the replay stops early, the report says at which step: fix the change or
  the steps, and record again.
- Look at every screenshot with your read tool and check that it shows what
  its caption says. You cannot watch the video, so the screenshots are your
  proof; the video is for the owner. Claim only what you looked at.

Put them in `.harness/pr.md` as Markdown images, and they are shown in
place. `stop` prints paths from the project's folder, and `.harness/pr.md`
takes them from the worktree's root, so put `projects/<project>/` before
each. A video plays only on a line of its own:

```markdown
![the greeting after sign-in](projects/<project>/.desktop/recordings/<time>/01-greeting.png)

![](projects/<project>/.desktop/recordings/<time>/video.webm)
```

The harness uploads the files. Never commit a recording, and never link one
by a URL of your own: if an upload fails, say so in your reply, with the
error.
