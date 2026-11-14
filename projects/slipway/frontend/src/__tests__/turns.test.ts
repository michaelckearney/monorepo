import { describe, expect, it } from "vitest";
import { sentence, summary, took, turns, type Line, type Steps } from "../turns";

let id = 0;
const line = (kind: string, text: string, time = "2026-11-08T10:00:00Z"): Line => ({ id: ++id, time, kind, text });

describe("turns", () => {
  it("starts a turn at each prompt, without the line that names the session", () => {
    const ts = turns([
      line("prompt", "=== 2026-11-08T10:00:00Z session s1\nWork on #10."),
      line("text", "On it."),
      line("prompt", "=== 2026-11-08T11:00:00Z session s1\nAgain."),
    ]);
    expect(ts.map((t) => t.prompt)).toEqual(["Work on #10.", "Again."]);
    expect(ts[0].items).toMatchObject([{ kind: "text", text: "On it." }]);
  });

  it("makes a tool's start and end one step, with the title it ends with", () => {
    const [t] = turns([
      line("prompt", "=== x session s\nGo."),
      line("tool", 'bash: {"command":"npm test","descr'),
      line("tool", "read: src/a.ts"),
      line("result", "bash: Runs the tests\n193 passed"),
      line("error", "read: src/a.ts\nno such file"),
    ]);
    const steps = (t.items[0] as Steps).steps;
    expect(steps).toMatchObject([
      { tool: "bash", title: "Runs the tests", output: "193 passed", state: "done" },
      { tool: "read", title: "src/a.ts", output: "no such file", state: "failed" },
    ]);
    expect(summary(t.items[0] as Steps, false)).toBe("Ran 2 steps (1 failed)");
  });

  it("says what runs now, and a step left running when the turn ended stopped", () => {
    const [t] = turns([line("tool", 'bash: {"command":"harness test"}')]);
    const g = t.items[0] as Steps;
    expect(summary(g, true)).toBe("Running harness test…");
    expect(summary(g, false)).toBe("Ran 1 step (1 stopped)");
  });

  it("keeps the bot's messages between runs of steps, and leaves its reasoning out", () => {
    const [t] = turns([line("tool", "read: a"), line("result", "read: a\nx"), line("reasoning", "Hmm."), line("text", "Done.")]);
    expect(t.items.map((i) => i.kind)).toEqual(["steps", "text"]);
  });

  it("shows an error that is not a tool's as the turn's", () => {
    const [t] = turns([line("error", '{"name":"ContextOverflowError","data":{"message":"too long"}}')]);
    expect(t.items).toMatchObject([{ kind: "text", error: true }]);
  });
});

describe("sentence", () => {
  it("words opencode's tools", () => {
    const [t] = turns([
      line("result", "read: frontend/src/themes.ts\n…"),
      line("result", "grep: parseThemeCSS\n6 matches"),
      line("result", "bash: git status --porcelain\n"),
      line("result", "bash: Runs the unit tests\nok"),
    ]);
    expect((t.items[0] as Steps).steps.map((s) => sentence(s, "past"))).toEqual([
      "Read frontend/src/themes.ts",
      "Searched for parseThemeCSS",
      "Ran git status --porcelain",
      "Runs the unit tests",
    ]);
  });
});

describe("took", () => {
  it("says how long", () => {
    expect(took("2026-11-08T10:00:00Z", "2026-11-08T10:00:40Z")).toBe("40 s");
    expect(took("2026-11-08T10:00:00Z", "2026-11-08T12:04:00Z")).toBe("2 h 4 min");
  });
});
