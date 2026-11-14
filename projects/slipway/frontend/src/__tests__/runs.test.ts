import { describe, expect, it } from "vitest";
import type { Run } from "@backend";
import { runFor, runOf, runsHere } from "../runs";

const run = (project: string, over: Partial<Run> = {}) => ({ project, running: false, started: false, phase: "", pr: 0, current: "", ...over }) as Run;

describe("each project's run", () => {
  const runs = [run("harbor"), run("slipway", { running: true, pr: 18, current: "harness-17" })];

  it("is found by its project", () => {
    expect(runOf(runs, "slipway")?.pr).toBe(18);
    expect(runOf(runs, "nope")).toBeNull();
    expect(runOf(undefined, "harbor")).toBeNull();
  });

  it("is the one that runs for an item for no one project", () => {
    expect(runFor(runs, "")?.project).toBe("slipway");
    expect(runFor(runs, "harbor")?.project).toBe("harbor");
    expect(runFor([run("harbor"), run("slipway")], "")?.project).toBe("harbor");
  });

  it("runs a pull request only where it runs", () => {
    expect(runsHere(runOf(runs, "slipway"), 18)).toBe(true);
    expect(runsHere(runOf(runs, "harbor"), 18)).toBe(false);
  });
});
