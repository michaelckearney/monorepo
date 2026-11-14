import { describe, expect, it } from "vitest";
import { opens, shown, summarise, summaryState, trialSeconds, trialState, type TrialRow } from "../benchmarks";

let id = 0;
const trial = (scenario: string, model: string, status: TrialRow["status"], score: number | null = null): TrialRow => ({
  id: ++id,
  scenario,
  model,
  n: 1,
  status,
  score,
  seconds: status === "done" ? 60 : null,
  input_tokens: status === "done" ? 1000 : null,
  output_tokens: status === "done" ? 100 : null,
  error: "",
  started_at: status === "waiting" ? null : "2026-10-24 15:00:00.000",
});

describe("summarise", () => {
  it("sums up each scenario on each model, over the trials that ran", () => {
    const rows = summarise([
      trial("tasklane", "a", "done", 100),
      trial("tasklane", "b", "done", 40),
      trial("tasklane", "a", "done", 50),
      trial("tasklane", "a", "done", null),
      trial("tasklane", "a", "failed"),
    ]);
    expect(rows.map((r) => [r.model, r.trials, r.done, r.graded, r.mean, r.lowest, r.inputTokens])).toEqual([
      ["a", 4, 3, 2, 75, 50, 1000],
      ["b", 1, 1, 1, 40, 40, 1000],
    ]);
  });

  it("has no numbers before a trial ends", () => {
    const [row] = summarise([trial("parcel", "a", "running")]);
    expect([row.mean, row.seconds].map(shown)).toEqual(["–", "–"]);
  });
});

describe("states", () => {
  it("says a done trial with no score was not graded", () => {
    expect(trialState(trial("parcel", "a", "done", 80))).toEqual({ label: "Graded", bad: false });
    expect(trialState(trial("parcel", "a", "done"))).toEqual({ label: "Not graded", bad: true });
    expect(trialState(trial("parcel", "a", "failed"))).toEqual({ label: "Failed", bad: true });
  });

  it("is the trial's own state for a scenario run once", () => {
    const [row] = summarise([trial("parcel", "a", "running")]);
    expect(summaryState(row).label).toBe("Running…");
  });

  it("names the trial running, then counts the graded ones, over several", () => {
    const running = { ...trial("parcel", "a", "running"), n: 2 };
    expect(summaryState(summarise([trial("parcel", "a", "done", 90), running, trial("parcel", "a", "waiting")])[0]).label).toBe(
      "Running trial 2…",
    );
    expect(summaryState(summarise([trial("parcel", "a", "done", 90), trial("parcel", "a", "waiting")])[0])).toEqual({
      label: "1 of 2 graded",
      bad: false,
    });
    expect(summaryState(summarise([trial("parcel", "a", "done", 90), trial("parcel", "a", "done")])[0])).toEqual({
      label: "1 of 2 graded",
      bad: true,
    });
  });

  it("opens a trial that has started, but not one still to come", () => {
    expect(
      [trial("p", "a", "done"), trial("p", "a", "running"), trial("p", "a", "cancelled"), trial("p", "a", "waiting")].map(opens),
    ).toEqual([true, true, true, false]);
  });
});

describe("trialSeconds", () => {
  it("counts a trial going from its turn's start, in UTC, and is the turn's once it ended", () => {
    const now = Date.UTC(2026, 9, 24, 15, 1, 30);
    expect(trialSeconds(trial("p", "a", "running"), now)).toBe(90);
    expect(trialSeconds(trial("p", "a", "done"), now)).toBe(60);
    expect(trialSeconds(trial("p", "a", "waiting"), now)).toBeNull();
  });
});
