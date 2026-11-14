# Benchmarks

How well Harbor's agent does what users ask: one user message, then a
grade of what the agent did in reply. The e2e tests say Harbor does what
the agent decides correctly; benchmarks say whether the agent decides well.
They run on the fixture sites (`fixtures.md`), against a real model, so
they cost money and take minutes. They run from the developer bar's
Benchmarks page in a dev build (`harness run`), and nowhere else.

## A scenario

A scenario is one YAML file in `backend/benchmark/scenarios/`, which the
app embeds. The folder is flat: one file per scenario, and `setup/` for
the files a setup uses. A setup names files from `scenarios/`. Go cannot
embed files from outside the package, so a setup that uses one of the dev
fixtures' services has its own copy, as `setup/parcel.ts`.

```yaml
# backend/benchmark/scenarios/tasklane-open-issues.yaml
prompt: >
  Make me a dashboard of my open Tasklane issues.
  It's at https://tasks.northwind.localhost:8443.

# What exists before the message. Optional.
setup:
  services: []          # service files, from scenarios/
  dashboards: []        # dashboard files, named by their file's name
  signed_in: []         # services, by name, whose sites the user's
                        # browser is signed in to

# What the grader scores, each from 0 to 100.
criteria:
  - id: data
    title: Right issues
    weight: 0.4
    description: The service returns Alex's 17 open issues, such as APP-2 and OPS-5, and no done ones, such as WEB-2.
  - id: signed_out
    title: Signed out
    weight: 0.3
    description: Signed out, its tools say to sign in, and return no data.
  - id: dashboard
    title: Dashboard
    weight: 0.2
    description: The dashboard shows those issues, and no card shows an error.
  - id: scope
    title: Scope
    weight: 0.1
    description: It submitted no form but the sign-in, and made nothing it was not asked for.
```

A scenario is one message, not a conversation. A recorded conversation
would replay a world that is gone: refs from old snapshots, drafts that
were never saved, sign-ins that never happened. What a later turn needs
goes in `setup` instead. To test "the issues card is empty", the setup
saves a broken service, and the prompt says so.

When asking is right, as for "make me a dashboard of my emails", the
criteria say so: it asks which site, and browses and saves nothing until
it knows.

### Writing criteria

Criteria are written for the scenario, not shared between scenarios.
"Task completion" gets a vague grade; "returns the 17 open issues, and no
done ones" does not. Facts the grader checks against, such as counts, ids,
and names, come from the fixtures' seed data
(`backend/fixture/apps/*/data`), which is the same every run.

Weights are relative: Go divides by their sum.

## Grading

After the turn, the run gathers the evidence, and a grading model scores
each criterion against it.

### The evidence

The same for every scenario, so that the grader judges what the agent made,
not what it says it made:

- The prompt, the setup, and the criteria.
- The transcript: messages and tool calls, as the chat stores them.
- What the agent saved: each service's and dashboard's file.
- What each saved service returns: every read-only tool with no required
  input, called in the user's browser, signed in.
- The same calls after the user's browser is cleared for the site. A tool
  should say to sign in and return no data. Agents fail this most often
  (`fixtures.md`), and their own testing never shows it.
- Each dashboard, rendered: what each card shows. This needs what
  `preview_dashboard` needs, a dashboard rendered outside the app's window,
  and is built once, for both. Until then the grader sees the dashboard's
  file only.
- The permission prompts the agent caused.

A tool that needs input shows real output only where the agent called it
in the transcript. A grader that calls tools itself would close that gap,
but it varies more and costs more; it waits until the gap matters.

### The grader

As in the old Harbor's `pkg/benchmarks/evaluation.go`:

- It scores each criterion from 0 to 100, with a rationale, the evidence
  it relied on (quoted), and the issues it found, and writes a short
  summary of the trial.
- It does not give an overall score. Go computes the trial's score from
  the criteria's scores and weights.
- It answers by calling a `report` tool, whose input lists the criteria,
  keyed by id, in the scenario's order, each one's evidence, issues, and
  rationale before its score, and the summary last: the order to think in.
  The call is not held to the schema: tools are not strict, and the tool
  choice is left to the model, as a required call turns on vLLM's grammar.
  Go checks the report: every criterion scored, no unknown ids, each score
  from 0 to 100. A wrong report, or a reply with no call, goes back to the
  grader with what is wrong, and it tries again, up to three tries in all;
  then the trial is not graded, with what the grader sent last.
- Why not hold the report to the schema: on vLLM with Qwen, a strict tool
  call still came back with lists as strings, and structured output
  (`text.format`), which did hold, lets JSON have any whitespace between
  its tokens: Qwen 3.6 and 3.8, on two vLLM builds, wrote spaces for over
  an hour, a known, unfixed vLLM problem (issue #38696). vLLM's
  `disable_any_whitespace` stops that, but measurably empties answers.

Its instructions anchor the scale, so that scores mean the same between
runs and between models:

| Score | Means |
|---|---|
| 100 | Done, fully and correctly |
| 75 | Done, with a small flaw the user would hardly notice |
| 50 | Partly done, or done with a flaw the user would notice |
| 25 | Attempted, but mostly wrong |
| 0 | Not done, or wrong |

What changed from the old Harbor's grader:

- Criteria are the scenario's own, not four shared ones.
- The grader sees what the services return, signed in and out, not only
  the event log and the files.
- The scale has anchors.
- A grader that does not answer, or answers wrongly, leaves the trial
  **not graded**. The old one scored it 0, which put the grader's failures
  into the agent's averages.
- There is no `recommendation` (accept, inspect, reject). The score says
  the same.

The grading model is a strong one, and the same from run to run, so that a
change in scores is the agent's, not the grader's. Each run shows it.

## A run

The developer bar's **Benchmarks** button opens the page: the runs on the
left, newest first, and **New run**. A new run chooses:

- the scenarios, all of them to start with;
- the models to benchmark, and the grading model, each written
  provider/model, such as `openai/gpt-5` or
  `openai-compatible/nvidia/Qwen3.6-35B-A3B-NVFP4`. Each uses that
  provider as it is set up in Settings, under Providers, in use or not, so a model on a
  local server can be graded by one on OpenAI. The form starts from the
  model the agent uses;
- how many times each scenario runs on each model, since one score says
  little about a model that answers differently each time.

The run starts only when each model's provider is set up and the fixtures
are running. It goes on in the background, one trial at a time, and the page
follows it live. **Cancel the run** stops the trial going and the ones
waiting. One run goes at a time, in the app or in the command-line program
(below), and either can cancel the other's.

A run goes in the process that started it, and ends with it. While it goes,
it has a folder in the data folder, `benchmarks/<run>/`, holding its trials
and a file, `lock`, that its process holds an operating system lock on.
A run whose lock is free is one whose process quit or was killed before it
ended the run: the app at startup, the page every few seconds while a run
goes, and each command of the command-line program stop it and delete its
folder. It is shown as stopped. Cancelling marks the run `cancelled` in the
database, and its process, which looks twice a second, stops it.

A run is one table: a row for each scenario on each model, with its status,
score, and the agent's tokens in and out and seconds. A scenario run more
than once shows the means over its trials that ran to the end, and the
lowest score, with a row for each trial under it. Choosing a trial that
has started splits the page: under the table, its grade (the grader's
summary and each criterion's score, rationale, issues, and evidence) and
the evidence the grader judged; on the right, its chat, as the agent's
panel shows one, drawn by the chat's own `ChatTranscript`. A trial that
failed shows its error.

A trial going shows as it goes: its tokens so far, the seconds since the
agent's turn started, and its chat, which the panel follows. Its chat is in
the trial's own database, which only its run's process reads, so the run
copies it to the trial's row each time a message is added, from the same
loop that waits for the turn to end; the page's live queries show it from
there, whichever process the run is in.

Each trial, a scenario on a model, runs in-process with nothing shared:

1. A new folder in its run's, `benchmarks/<run>/<trial>/`, for its
   database and both browsers' profiles, deleted when the trial ends.
2. A new database there, from `desktop.NewDB`: Harbor's tables, with
   nothing in them, not the dev fixtures.
3. Harbor's backend on them: its MCP server, both engines, and the agent,
   on the model under test.
4. The setup: its services and dashboards saved, its sites signed in to.
5. The prompt sent, and the turn run until the agent replies or stops. A
   turn that takes more than 20 minutes is stopped.
6. The evidence gathered, and the grade.

The fixture sites run in the run's process: the dev app's own, or the one
`harness cli` started. Two processes share the sites' store, so a sign-in
in one is good in the other.

### From the command line

Harbor's command-line program, in `cli/`, runs and reads the benchmarks, for
an agent developing Harbor as much as for a person. Slipway builds and runs
it on the data folder `harness run` uses, so it shares the dev app's
database: its providers, and its runs, which the page shows live.

```bash
harness cli -mode=dev benchmark scenarios
harness cli -mode=dev benchmark start -scenario=parcel-last-delivery -model=openai/gpt-5 -repeat=3
harness cli -mode=dev benchmark wait 4
harness cli -mode=dev benchmark list
harness cli -mode=dev benchmark show 4
harness cli -mode=dev benchmark trial 16
harness cli -mode=dev benchmark cancel 4
```

- `start` takes the scenarios (all by default), the models and the grader
  (the agent's model by default), and the repeats, and starts the run in a
  process of its own, which goes on after `start` returns; `wait` follows
  it, a line for each trial as it changes, and shows it when it ends. `run`
  is `start` and `wait` in one process, for a person at a terminal: Ctrl-C
  cancels its run.
- `list` is the runs, newest first, with the trial each one going is on.
- `show` is a run's trials, with their ids, and the means of a scenario run
  more than once.
- `trial` is a trial's grade and transcript as Markdown, with a tool's
  result cut to 2,000 characters unless `-full`, and `-evidence` adds what
  the grader judged. A trial going shows its transcript so far.
- `show`, `list`, `scenarios`, and `trial` take `-json`.

### The user's part

Two things in a turn wait for the user. The run does both itself:

- **Sign-in.** When Harbor would show a live view, the run signs in in that
  tab instead (`backend/benchmark/signin.go`), with real clicks and typing, step
  by step, as the user would: it types the fixtures' email and password
  into empty sign-in fields, Tidewater's TOTP code, or the newest code the
  site emailed to `mail.localhost`, and presses Enter; with no field to
  fill, it clicks a button that moves on, such as "Sign in with Northwind
  ID", consent's "Accept", or the bank's "Yes, continue"; and it follows an
  emailed sign-in link. One driver serves every site, rather than a script
  each. It gives up after 15 steps or 90 seconds, and closes the view,
  which ends the sign-in signed out, as a user would. Harbor closes a
  service's sign-in when its check passes; the view `browser_sign_in`
  shows has no check, so the run clicks Done itself, once the site is back
  with nothing to do for three steps in a row. Tidewater, Parcel,
  and Tasklane are tested. It does not yet answer what Harbor asks beside
  the page (a passkey, an HTTP password) or choose a person, so Shoebox,
  the Homebase router, and Clearwater do not sign in yet.
- **Permission.** Every prompt is answered yes, and noted for the evidence.

### What it keeps

Runs and trials are in the app's database, in the dev build's own tables
(`database/dev/011_benchmarks.sql`, and `012_benchmark_progress.sql`):
`benchmark_runs`, and `benchmark_trials`, which has each trial's status,
score, seconds, and tokens, and, once it ends, the whole trial as JSON: the
transcript, the evidence, and the grade. While it goes, it has when the
agent's turn started and the transcript so far, which a trial cancelled or
stopped keeps. A trial that is not graded has no score, and says
why. Tokens and time are the agent model's only, not the grader's.

## How it fits Harbor

- `backend/benchmark` is the run: scenarios, the user's part, the
  evidence, and the grade. It cannot import `backend`, which serves it, so
  a trial's Harbor is an interface, `benchmark.Harbor`, and `backend`
  makes one with `openTrial`.
- `backend.Benchmarks` is the page's service. `Services` adds it only in a
  dev build (`desktop.Mode()`), the one with its tables.
- A trial's Harbor is the same backend `Services` serves, put together on
  the trial's database and folder, with the run in the user's place: it
  shows live views, and it allows every tool call, noting the ones Harbor
  would have asked about.

The tests run with `harness test`: the scenarios read, and the grade's
checks and arithmetic (`backend/benchmark`); when the fixtures are
running, a whole trial with scripted models, which signs in to Tidewater,
Tasklane, and Parcel (`backend/benchmarks_test.go`); and the page
(`tests/dev`).

## The first scenarios

| File | Prompt | Covers |
|---|---|---|
| `vague-email.yaml` | Make me a dashboard of my emails. | Asks which site; browses and saves nothing |
| `tasklane-open-issues.yaml` | (above) | GraphQL, a token in localStorage, OIDC with PKCE: the closest fixture to Outlook |
| `tidewater-transactions.yaml` | Show me my recent Tidewater Bank transactions on a dashboard. It's at https://tidewater.localhost:8443. | HTML with no API; TOTP; a 5-minute idle timeout, so signed out matters |
| `tasklane-fix-check.yaml` | Harbor keeps asking me to sign in to Tasklane, even right after I have. | Setup: the service with a `login.check` that looks for a cookie Tasklane does not set. Fixes the check |
| `parcel-last-delivery.yaml` | When was my last Parcel order delivered, and what was in it? | Setup: the dev fixtures' Parcel service, signed in. Answers with the service's tool, without the browser |

These are a start. Each failure seen on a real site, such as Outlook,
becomes a fixture behaviour if none has it, then a scenario.

## Not in this piece

- Several messages in one scenario. A setup covers most of what they would
  test.
- A grader that calls tools itself.
- Rendering dashboards for the grader (`preview_dashboard`'s work).
- Signing in where Harbor asks beside the page, or the site asks to
  choose a person: Shoebox, the Homebase router, Clearwater.
- Tokens from a provider other than OpenAI: usage is read by OpenAI's
  names for it.
- Deleting a run, and comparing runs.
- Running scenarios in parallel. Trials share the fixtures, and some
  fixtures allow one session per user (V10).
