// The Benchmarks page's data: runs and trials, as the benchmark_ tables
// hold them, and a trial's result, as backend/benchmark stores it.

export interface Run {
  id: number;
  grader: string;
  status: "running" | "done" | "cancelled" | "stopped";
  started_at: string;
}

export interface TrialRow {
  id: number;
  scenario: string;
  model: string;
  n: number;
  status: "waiting" | "running" | "done" | "failed" | "cancelled" | "stopped";
  score: number | null;
  seconds: number | null;
  input_tokens: number | null;
  output_tokens: number | null;
  error: string;
  /** When the agent's turn started; null until it has. */
  started_at: string | null;
}

export interface Scored {
  id: string;
  title: string;
  weight: number;
  score: number;
  rationale: string;
  evidence: string[] | null;
  issues: string[] | null;
}

export interface Call {
  tool: string;
  output?: unknown;
  error?: string;
}

export interface Result {
  transcript: { role: string; parts: unknown[] }[];
  evidence: {
    services: { name: string; source: string; problem?: string; signedIn: Call[] | null; signedOut: Call[] | null }[] | null;
    dashboards: { name: string; source: string }[] | null;
    prompts: { tool: string; input: Record<string, unknown> }[] | null;
    signIns: { browser: string; service: string; heading: string; steps: string[] | null; outcome: string }[] | null;
    asked?: string[];
  };
  grade?: { score: number; summary: string; criteria: Scored[] };
  notGraded?: string;
}

/** One scenario on one model, over its repeats. */
export interface Summary {
  scenario: string;
  model: string;
  /** Its trials, in the run's order. */
  rows: TrialRow[];
  trials: number;
  /** The trials that ran to the end, and of those, the ones graded. */
  done: number;
  graded: number;
  mean: number | null;
  lowest: number | null;
  /** Means over the trials that ran to the end. */
  inputTokens: number | null;
  outputTokens: number | null;
  seconds: number | null;
}

/** Each scenario on each model, in the run's order. */
export function summarise(trials: readonly TrialRow[]): Summary[] {
  const groups = new Map<string, TrialRow[]>();
  for (const t of trials) {
    const key = `${t.scenario}\u0000${t.model}`;
    groups.set(key, [...(groups.get(key) ?? []), t]);
  }
  return [...groups.values()].map((ts) => {
    const done = ts.filter((t) => t.status === "done");
    const scores = done.flatMap((t) => (t.score === null ? [] : [t.score]));
    const mean = (xs: number[]) => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : null);
    return {
      scenario: ts[0].scenario,
      model: ts[0].model,
      rows: ts,
      trials: ts.length,
      done: done.length,
      graded: scores.length,
      mean: mean(scores),
      lowest: scores.length ? Math.min(...scores) : null,
      inputTokens: mean(done.map((t) => t.input_tokens ?? 0)),
      outputTokens: mean(done.map((t) => t.output_tokens ?? 0)),
      seconds: mean(done.map((t) => t.seconds ?? 0)),
    };
  });
}

/** A number as the page shows it; none is a dash. */
export function shown(x: number | null | undefined): string {
  return x === null || x === undefined ? "–" : Math.round(x).toLocaleString();
}

/** Where a trial is, in words, and whether that is a problem. */
export function trialState(t: TrialRow): { label: string; bad: boolean } {
  if (t.status === "done") {
    return t.score === null ? { label: "Not graded", bad: true } : { label: "Graded", bad: false };
  }
  const labels: Record<TrialRow["status"], string> = {
    waiting: "Waiting",
    running: "Running…",
    done: "",
    failed: "Failed",
    cancelled: "Cancelled",
    stopped: "Stopped",
  };
  return { label: labels[t.status], bad: t.status === "failed" };
}

/** Where a scenario on a model is: its one trial's state, or over several, the one running, or how many were graded. */
export function summaryState(s: Summary): { label: string; bad: boolean } {
  if (s.rows.length === 1) {
    return trialState(s.rows[0]);
  }
  const running = s.rows.find((t) => t.status === "running");
  if (running) {
    return { label: `Running trial ${running.n}…`, bad: false };
  }
  if (s.rows.every((t) => t.status === "waiting")) {
    return { label: "Waiting", bad: false };
  }
  const ended = s.rows.filter((t) => t.status !== "waiting").length;
  return { label: `${s.graded} of ${s.trials} graded`, bad: s.graded < ended };
}

/** A trial opens once it has started: one going shows its chat so far. */
export function opens(t: TrialRow): boolean {
  return t.status !== "waiting";
}

/** A trial's seconds: the agent's turn's once it ended, and while the trial goes, how long since the turn started. now is Date.now(). */
export function trialSeconds(t: TrialRow, now: number): number | null {
  if (t.status !== "running" || t.started_at === null) {
    return t.seconds;
  }
  // SQLite's strftime writes UTC, with no zone.
  return Math.max(0, (now - Date.parse(t.started_at.replace(" ", "T") + "Z")) / 1000);
}
