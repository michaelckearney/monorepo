# monorepo

Harbor and the Slipway app, on the desktop framework, in one monorepo for
the [harness](https://github.com/michaelckearney/harness).

```
harness.yaml            projects, bots, servers
framework/              Wails v3 apps: Go backend, Vue + TypeScript frontend
projects/harbor/        Harbor
projects/slipway/       the Slipway app, for watching the bots
.github/workflows/      the pipelines, which call the harness's
```

```bash
harness check                 # harness.yaml, the framework, the projects
harness test                  # everything: the framework, then each project
harness test -p harbor -unit  # one project, one phase
harness run -p harbor         # develop Harbor
harness release -plan         # what each project would release
```

Issues are worked by the bots on the Spark: label one `project:harbor`,
`project:slipway`, or `framework` to say what it is for (an issue with no
such label is no bot's), and
`agent:claude-code` or `agent:openai-codex` to choose who works it (opencode, on
the Spark's model, otherwise). See the harness's README for the rest.
