# RULES

- do not worry about backwards compatibility for existing users when making "breaking changes", there are no users yet so there's really no such thing as "breaking changes"
- the database is the exception: the dev database holds benchmark history worth keeping, so never edit a migration in `database/` that has run; change the schema or data with a new migration, numbered next across `database/` and `database/<mode>/` together. Migrations may be consolidated later, by exporting what is kept, resetting the database, and importing it
- do not implement fallbacks in code for conditions that should never exist
- UI text is for someone using the app, not someone building it: label things and say what's wrong when something fails, but don't explain how Harbor works, what the agent is or isn't allowed to do, or why a design choice was made. That belongs in comments and `docs/`. When in doubt, leave the text out: no intro paragraphs on pages, no explanatory empty states, short placeholders, and icon-only buttons (with a tooltip) where the icon is clear

# BENCHMARKS

- run and read the benchmarks with `harness cli -mode=dev benchmark` (it lists its commands): `start` returns a run's id at once and the run goes on in the background; follow it with `wait <run>`, run in the background, since a run takes minutes; read results with `list`, `show <run>`, and `trial <trial>`
- a run serves the fixture sites itself, and uses the providers set up in the dev app (Providers)
- an agent working an issue on its own, such as the bot on the Spark, does not run the benchmarks: they need the providers set up in the dev app. If a change needs them, say so in the description

# TESTS

- `harness test` is everything: lint, then unit tests, then e2e tests. It takes minutes; run it once before you are done, not to find things out
- while you work, run only what covers your change: `harness test -unit -run <regexp>` (Go test names and vitest test names), or `harness test <path>...` (a `.test.ts` file, or a folder of Go tests). These pass `-short`, which skips `TestABenchmarkTrial`
- the unit tests are go test, as a server build, and vitest, in jsdom. the desktop framework generates their configuration into `.desktop/`, so there is no vitest config in the repository to read
- in a frontend test, there are no Node types, so `import "node:fs"` fails lint. Read a `.vue` or `.ts` file's text with `import.meta.glob(..., { query: "?raw", import: "default", eager: true })`, as `tokens.test.ts` does; `?raw` gives an empty string for a `.css` file

# RELEASES

- every change reaches `main` through a squash-merged pull request whose title is a conventional commit (`feat:`, `fix:`, `docs:`, `chore:`…, with `!` for a breaking change): the title says what the merge releases. There is no version to edit; `harness release` takes it from the `harbor/v*` tags (the harness's README, Releases)
- a pull request's checks (the monorepo's `.github/workflows/`) run `harness ci pr`: `harness check`, then `harness test` on what the change affects; `main` runs `harness ci release` and `harness ci test`. Nothing deploys: the servers follow the harness `harness.yaml` pins
