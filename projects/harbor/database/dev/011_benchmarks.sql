-- The developer bar's Benchmarks page: each run, and each trial in it, one
-- scenario run once on one model (backend/benchmark). The page reads them
-- with live queries. A dev build only: nothing in prod runs benchmarks.
--
-- A run's status: running, done, cancelled, or stopped (Harbor quit
-- during it).
CREATE TABLE benchmark_runs (
    id         INTEGER PRIMARY KEY,
    grader     TEXT NOT NULL,   -- the model that grades every trial
    status     TEXT NOT NULL DEFAULT 'running'
               CHECK (status IN ('running', 'done', 'cancelled', 'stopped')),
    started_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%d %H:%M:%f', 'now')),
    ended_at   TEXT
);

-- A trial's status: waiting, running, done (graded or not), failed (the run
-- itself failed, such as a setup that did not work), cancelled, or stopped.
-- result is the whole trial as JSON once it is done: transcript, evidence,
-- and grade. score is NULL when the trial was not graded.
CREATE TABLE benchmark_trials (
    id            INTEGER PRIMARY KEY,
    run_id        INTEGER NOT NULL REFERENCES benchmark_runs (id) ON DELETE CASCADE,
    scenario      TEXT NOT NULL,
    model         TEXT NOT NULL,
    n             INTEGER NOT NULL,   -- the scenario's repeat on the model, from 1
    status        TEXT NOT NULL DEFAULT 'waiting'
                  CHECK (status IN ('waiting', 'running', 'done', 'failed', 'cancelled', 'stopped')),
    score         REAL,
    seconds       REAL,
    input_tokens  INTEGER,
    output_tokens INTEGER,
    error         TEXT NOT NULL DEFAULT '',
    result        TEXT CHECK (result IS NULL OR json_valid(result))
);
CREATE INDEX benchmark_trials_by_run ON benchmark_trials (run_id, id);
