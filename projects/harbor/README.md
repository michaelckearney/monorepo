# Harbor

Harbor, built on the monorepo's [desktop framework](../../framework) one piece at a time. It connects
websites you are signed in to with the programs that use them: dashboards,
its own agent, and MCP clients such as Claude Code.

So far it keeps services and dashboards, lets you read, edit, and show them,
runs services' tools in its own Chromium, and has an agent that writes and
tests services and dashboards through Harbor's MCP server. `docs/` has the
spec for each piece.

The agent needs a provider: open Settings in the sidebar, and under Providers add an
OpenAI API key.

```bash
harness run
harness run -mode=dev
harness test
harness build
```

Harbor has two modes. `prod`, the default, is what users get. `dev` adds
the developer bar across the top of the window, with Tools for trying the
agent's tools by hand and Benchmarks for grading the agent
(`docs/benchmarks.md`), and starts from the fixtures
in `database/dev/`. `harness build` makes both: `Harbor.app` and
`Harbor Dev.app`, each with its own app id and data folder
(`~/.harbor/data/<mode>`).

Harbor is one Go module and one npm package, with `go.mod` and
`package.json` in `projects/harbor`, so `go get` and `npm install` run from
there. It is a project of the desktop framework, built from the framework
in this monorepo (`replace ../../framework` in `go.mod`).

## Layout

- `backend/`: the app's Go package. `documents.go` saves services and dashboards;
  `tools.go` runs tools for the frontend, in your browser or the agent's;
  `mcp.go` is Harbor's MCP server, `mcpbrowser.go` its browser tools, and
  `mcphttp.go` serves it to other agents; `chats.go` is the agent's
  binding; `providers.go` keeps the providers and their settings;
  `agenttools.go` runs the agent's tools for the Tools page;
  `preferences.go` keeps the theme, and where you left the window;
  `icons.go` keeps each service's favicon; `manifests.go` reads the
  manifest of a service that has none, such as a fixture.
- `backend/service/`: reads a service file.
- `backend/dashboard/`: compiles a dashboard, with Vue's compiler in goja.
- `backend/chat/`: the agent, and its chats in SQLite.
- `backend/provider/`: reads and runs a provider file.
- `backend/providers/`: the built-in providers, one TypeScript file each.
- `backend/engine/`: the browser engine, on the framework's package `browser`;
  `browse.go` is the agent's own browsing.
- `backend/favicon/`: finds a site's favicon as Chromium finds a tab's.
- `backend/fixture/`: the fixture sites, made-up web apps that dev's
  services, the e2e tests, and the benchmarks use. A dev build serves them
  while it runs; its README lists the sites.
- `database/`: the migrations every mode has.
- `database/dev/`: dev's own migration, `010_fixtures.sql`: a service for
  each site in `backend/fixture/`, and dashboards that use them, each its own
  file in `assets/`. A new dev database gets them, so after editing one,
  delete `.desktop/data/dev` to see it.
- `frontend/`: the Vue app, with Tailwind and shadcn-vue. Its design
  system, components, and built-in themes are the framework's shared
  frontend, `framework/ui`, imported as `@ui`, which Slipway uses too.
  `src/kit/` is the kit dashboards import from `"harbor"`; `src/themes.ts`
  keeps the themes you add.
- `cli/`: Harbor's command-line program, for developing it: the
  benchmarks, run and read from a terminal or by an agent. `harness cli
  -mode=dev benchmark` lists its commands; `docs/benchmarks.md` has more.
- `tests/`: e2e tests in Go, with a real Chromium and the running app. `tests/dev/` runs against a dev build, with the
  fixtures; `tests/prod/` against a prod build.
