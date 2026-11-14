import { describe, expect, it } from "vitest";
import { blocks, chatItems, describe as describeStep, grouped, input, status, summary, type Steps, type ToolStep } from "../chat";

let seq = 0;
const row = (role: string, ...parts: Record<string, unknown>[]) => ({ seq: ++seq, role, parts: JSON.stringify(parts) });
const call = (id: string, name: string, input: Record<string, unknown>, ask = false) => ({ toolCall: { id, name, input }, ...(ask ? { ask } : {}) });
const result = (id: string, r: Record<string, unknown>, refused = false) => ({ toolResult: { id, ...r }, ...(refused ? { refused } : {}) });

describe("chatItems", () => {
  it("makes messages and steps, in order", () => {
    seq = 0;
    const items = chatItems([
      row("user", { text: "hi" }),
      row("assistant", { text: "Looking." }, call("c1", "list_services", {})),
      row("tool", result("c1", { output: { services: [] } })),
      row("assistant", { text: "You have none." }),
    ]);
    expect(items.map((i) => (i.kind === "tool" ? [i.kind, i.name, status(i)] : [i.kind, i.text]))).toEqual([
      ["you", "hi"],
      ["agent", "Looking."],
      ["tool", "list_services", "done"],
      ["agent", "You have none."],
    ]);
    expect((items[2] as ToolStep).result).toEqual({ services: [] });
  });

  it("does not show what Harbor told the agent you were looking at", () => {
    const items = chatItems([row("user", { text: "Make it weekly" }, { text: '(The user is looking at the dashboard "Money".)', context: true })]);
    expect(items.map((i) => i.kind === "you" && i.text)).toEqual(["Make it weekly"]);
  });

  it("follows a tool that needs permission until it runs", () => {
    const rows = [row("assistant", call("c1", "save_note", { text: "milk" }, true))];
    expect(status(chatItems(rows)[0] as ToolStep)).toBe("asking");
    rows.push(row("tool", result("c1", { output: { saved: true } })));
    expect(status(chatItems(rows)[0] as ToolStep)).toBe("done");
  });

  it("shows a refusal as a refusal, not a failure", () => {
    const step = chatItems([
      row("assistant", call("c1", "save_note", {}, true)),
      row("tool", result("c1", { error: "The user did not allow this tool call." }, true)),
    ])[0] as ToolStep;
    expect(status(step)).toBe("refused");
  });

  it("shows a tool's error, and a tool that has not finished", () => {
    const [failed, running] = chatItems([
      row("assistant", call("c1", "call_service_tool", { service: "gitea", tool: "list" }), call("c2", "list_services", {})),
      row("tool", result("c1", { error: "sign in needed" })),
    ]) as ToolStep[];
    expect(status(failed)).toBe("failed");
    expect(failed.error).toBe("sign in needed");
    expect(describeStep(failed, "past")).toBe("Ran gitea › list");
    expect(status(running)).toBe("running");
  });

  it("counts a tool that returned nothing as done", () => {
    const step = chatItems([row("assistant", call("c1", "test_service_tool", { service: "gitea" })), row("tool", result("c1", {}))])[0] as ToolStep;
    expect(status(step)).toBe("done");
  });
});

describe("describe", () => {
  it("names the service a file saves", () => {
    const step: ToolStep = { kind: "tool", key: "k", id: "c1", name: "write_service", args: { source: 'defineService({\n  name: "gitea",' }, ask: true, refused: false };
    expect(describeStep(step, "past")).toBe("Saved the service gitea");
  });

  it("says what a step did, is doing, or asks to do", () => {
    const step: ToolStep = { kind: "tool", key: "k", id: "c1", name: "delete_dashboard", args: { name: "Inbox" }, ask: true, refused: false };
    expect(["past", "now", "ask"].map((t) => describeStep(step, t as "past"))).toEqual([
      "Deleted the dashboard Inbox",
      "Deleting the dashboard Inbox",
      "Delete the dashboard Inbox",
    ]);
  });
});

describe("grouped", () => {
  const items = () =>
    chatItems([
      row("user", { text: "hi" }),
      row("assistant", call("c1", "list_services", {}), call("c2", "call_service_tool", { service: "gitea", tool: "list" })),
      row("tool", result("c1", { output: {} }), result("c2", { error: "sign in needed" })),
      row("assistant", { text: "Signing in." }, call("c3", "browser_snapshot", {})),
    ]);

  it("gathers each run of steps between messages", () => {
    expect(grouped(items()).map((i) => (i.kind === "steps" ? i.steps.map((s) => s.id) : i.kind))).toEqual(["you", ["c1", "c2"], "agent", ["c3"]]);
  });

  it("sums up a run in one line", () => {
    const [, done, , last] = grouped(items()) as Steps[];
    expect(summary(done, true)).toBe("Ran 2 steps (1 failed)");
    expect(summary(last, true)).toBe("Looking at the page…");
    expect(summary(last, false)).toBe("Ran 1 step (1 stopped)");
    const asking = grouped(chatItems([row("assistant", call("c1", "save_note", {}, true))]))[0] as Steps;
    expect(summary(asking, true)).toBe("Needs your permission");
  });
});

describe("input", () => {
  const step = (name: string, args: Record<string, unknown>): ToolStep => ({ kind: "tool", key: "k", id: "c1", name, args, ask: false, refused: false });

  it("leaves out what the step's words say", () => {
    expect(blocks(input(step("list_services", {})))).toEqual([]);
    expect(blocks(input(step("read_service", { name: "gitea" })))).toEqual([]);
    expect(blocks(input(step("browser_navigate", { url: "https://a.test/" })))).toEqual([]);
    expect(input(step("browser_click", { element: "Save", ref: "e3", doubleClick: true }))).toEqual({ doubleClick: true });
  });

  it("is a service tool's own input", () => {
    expect(input(step("call_service_tool", { service: "gitea", tool: "list", input: { state: "open" } }))).toEqual({ state: "open" });
    expect(blocks(input(step("call_service_tool", { service: "gitea", tool: "list" })))).toEqual([]);
  });

  it("is everything, for a tool with no words", () => {
    expect(input(step("read_kit", { name: "Table" }))).toEqual({ name: "Table" });
  });
});

describe("blocks", () => {
  it("shows short values as fields, text as text, and the rest as JSON", () => {
    expect(
      blocks({ url: "https://a.test/", truncated: false, events: ["went to /", "set cookie sid"], snapshot: "- heading\n- button", rows: [{ a: 1 }] }),
    ).toEqual([
      { kind: "fields", fields: [["url", "https://a.test/"], ["truncated", "false"]] },
      { kind: "text", label: "events", text: "went to /\nset cookie sid" },
      { kind: "text", label: "snapshot", text: "- heading\n- button" },
      { kind: "text", label: "rows", text: '[\n  {\n    "a": 1\n  }\n]', language: "json" },
    ]);
    expect(blocks("<template></template>")).toEqual([{ kind: "text", text: "<template></template>" }]);
    expect(blocks(null)).toEqual([]);
  });

  it("shows code in its language, however short", () => {
    expect(blocks({ name: "Mail", source: "<template></template>" }, { source: "vue" })).toEqual([
      { kind: "fields", fields: [["name", "Mail"]] },
      { kind: "text", label: "source", text: "<template></template>", language: "vue" },
    ]);
  });
});
