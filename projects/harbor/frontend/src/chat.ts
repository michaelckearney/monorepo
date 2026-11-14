import type { Language } from "./highlight";

// A chat's messages, as Harbor stores them, made into what the chat shows:
// your messages, the agent's, and each tool the agent called.

export interface Message {
  kind: "you" | "agent";
  key: string;
  text: string;
}

export interface ToolStep {
  kind: "tool";
  key: string;
  /** The tool call's id, which an answer names. */
  id: string;
  name: string;
  args: Record<string, unknown>;
  result?: unknown;
  error?: string;
  /** The agent asked you before it runs the tool. */
  ask: boolean;
  /** You did not allow it. */
  refused: boolean;
}

export type Item = Message | ToolStep;

interface Part {
  text?: string;
  toolCall?: { id: string; name: string; input: Record<string, unknown> };
  toolResult?: { id: string; output?: unknown; error?: string };
  ask?: boolean;
  refused?: boolean;
  /** Text Harbor added for the agent, such as what you were looking at; the chat does not show it. */
  context?: boolean;
}

export function chatItems(rows: readonly { seq: number; role: string; parts: string }[]): Item[] {
  const items: Item[] = [];
  const steps = new Map<string, ToolStep>();
  for (const row of rows) {
    (JSON.parse(row.parts) as Part[]).forEach((part, i) => {
      const key = `${row.seq}-${i}`;
      if (part.text && !part.context) {
        items.push({ kind: row.role === "user" ? "you" : "agent", key, text: part.text });
      }
      const call = part.toolCall;
      if (call) {
        const step: ToolStep = { kind: "tool", key, id: call.id, name: call.name, args: call.input, ask: part.ask === true, refused: false };
        steps.set(call.id, step);
        items.push(step);
      }
      const result = part.toolResult;
      const step = result && steps.get(result.id);
      if (result && step) {
        step.refused = part.refused === true;
        step.error = result.error;
        step.result = result.error === undefined ? (result.output ?? null) : undefined;
      }
    });
  }
  return items;
}

// A tool step's words take the tense of where it is: what it did ("Ran"),
// what it is doing ("Running"), or what the agent asks to do ("Run").
export type Tense = "past" | "now" | "ask";
type Verb = Record<Tense, string>;
const verb = (past: string, now: string, ask: string): Verb => ({ past, now, ask });
const verbs = {
  list: verb("Listed", "Listing", "List"),
  read: verb("Read", "Reading", "Read"),
  save: verb("Saved", "Saving", "Save"),
  delete: verb("Deleted", "Deleting", "Delete"),
  run: verb("Ran", "Running", "Run"),
  test: verb("Tested", "Testing", "Test"),
  signIn: verb("Signed in to", "Signing in to", "Sign in to"),
  go: verb("Went", "Going", "Go"),
  look: verb("Looked at", "Looking at", "Look at"),
  take: verb("Took", "Taking", "Take"),
  click: verb("Clicked", "Clicking", "Click"),
  type: verb("Typed into", "Typing into", "Type into"),
  fill: verb("Filled in", "Filling in", "Fill in"),
  pick: verb("Picked", "Picking", "Pick"),
  press: verb("Pressed", "Pressing", "Press"),
  wait: verb("Waited for", "Waiting for", "Wait for"),
  answer: verb("Answered", "Answering", "Answer"),
  clear: verb("Cleared", "Clearing", "Clear"),
};

/** What a tool step does, in words, in the tense given. */
export function describe(step: ToolStep, tense: Tense): string {
  const [v, rest] = words(step);
  return v ? `${v[tense]} ${rest}`.trim() : rest;
}

/**
 * What a tool step was given that its words do not already say: for a
 * service's tool, only the tool's own input. A tool with no words here
 * shows all it was given.
 */
export function input(step: ToolStep): unknown {
  return words(step)[2];
}

// A step's verb, the rest of its words, and what it was given that they leave out.
function words(step: ToolStep): [Verb | null, string, unknown] {
  const a = step.args as Record<string, string | undefined>;
  const but = (...keys: string[]) => Object.fromEntries(Object.entries(step.args).filter(([k]) => !keys.includes(k)));
  switch (step.name) {
    case "list_services":
      return [verbs.list, "the services", but()];
    case "read_service":
      return [verbs.read, `the service ${a.name}`, but("name")];
    case "write_service":
      return [verbs.save, `the service ${serviceName(a.source) ?? ""}`, but()];
    case "delete_service":
      return [verbs.delete, `the service ${a.name}`, but("name")];
    case "call_service_tool":
      return [verbs.run, `${a.service} › ${a.tool}`, step.args.input];
    case "test_service_tool":
      return a.tool
        ? [verbs.test, `${a.service} › ${a.tool}`, step.args.input]
        : [verbs.signIn, `${a.service}, in the agent's browser`, step.args.input];
    case "list_dashboards":
      return [verbs.list, "the dashboards", but()];
    case "read_dashboard":
      return [verbs.read, `the dashboard ${a.name}`, but("name")];
    case "write_dashboard":
      return [verbs.save, `the dashboard ${a.name}`, but("name")];
    case "delete_dashboard":
      return [verbs.delete, `the dashboard ${a.name}`, but("name")];
    case "browser_navigate":
      return [verbs.go, `to ${a.url}`, but("url")];
    case "browser_navigate_back":
      return [verbs.go, "back", but()];
    case "browser_snapshot":
      return [verbs.look, "the page", but()];
    case "browser_take_screenshot":
      return [verbs.take, "a screenshot", but()];
    case "browser_click":
      return [verbs.click, `${a.element ?? a.ref}`, but("element", "ref")];
    case "browser_type":
      return [verbs.type, `${a.element ?? a.ref}`, but("element", "ref")];
    case "browser_fill_form":
      return [verbs.fill, "a form", but()];
    case "browser_select_option":
      return [verbs.pick, `an option in ${a.element ?? a.ref}`, but("element", "ref")];
    case "browser_press_key":
      return [verbs.press, `${a.key}`, but("key")];
    case "browser_wait_for":
      return [verbs.wait, "the page", but()];
    case "browser_handle_dialog":
      return [verbs.answer, "the page's dialog", but()];
    case "browser_evaluate":
      return [verbs.run, "code in the page", but()];
    case "browser_network_requests":
      return [verbs.list, "the page's requests", but()];
    case "browser_network_request":
      return [verbs.read, "one of the page's requests", but()];
    case "browser_clear_data":
      return [verbs.clear, a.site ? `${a.site} in the agent's browser` : "the agent's browser", but("site")];
    default:
      return [null, step.name, step.args];
  }
}

/** Which of what a step was given, and gave back, is code, and in what language. */
export function code(step: ToolStep): { input: Record<string, Language>; output: Record<string, Language> } {
  switch (step.name) {
    case "read_service":
      return { input: {}, output: { source: "typescript" } };
    case "write_service":
      return { input: { source: "typescript" }, output: {} };
    case "read_dashboard":
      return { input: {}, output: { source: "vue" } };
    case "write_dashboard":
      return { input: { source: "vue" }, output: {} };
    case "browser_evaluate":
      return { input: { function: "javascript" }, output: {} };
    default:
      return { input: {}, output: {} };
  }
}

/**
 * A part of what a step was given or gave back, as the chat shows it: short
 * values as a list of fields, text and lists of text as text, code in its
 * language, and the rest as JSON.
 */
export type Block =
  | { kind: "fields"; fields: [string, string][] }
  | { kind: "text"; label?: string; text: string; language?: Language };

// Text longer than this is a block of its own, not a field.
const fieldLength = 120;

/**
 * A step's input or output, in blocks; nothing, or an empty object, has none.
 * code says which fields are code, by name.
 */
export function blocks(value: unknown, code: Record<string, Language> = {}): Block[] {
  if (value === undefined || value === null || value === "") {
    return [];
  }
  if (typeof value === "string") {
    return [{ kind: "text", text: value }];
  }
  if (typeof value !== "object" || Array.isArray(value)) {
    return [{ kind: "text", text: json(value), language: "json" }];
  }
  const fields: [string, string][] = [];
  const rest: Block[] = [];
  for (const [label, v] of Object.entries(value)) {
    if (typeof v === "string" && code[label]) {
      rest.push({ kind: "text", label, text: v, language: code[label] });
    } else if (typeof v === "number" || typeof v === "boolean" || (typeof v === "string" && !v.includes("\n") && v.length <= fieldLength)) {
      fields.push([label, String(v)]);
    } else if (typeof v === "string") {
      rest.push({ kind: "text", label, text: v });
    } else if (Array.isArray(v) && v.length > 0 && v.every((x) => typeof x === "string")) {
      rest.push({ kind: "text", label, text: v.join("\n") });
    } else {
      rest.push({ kind: "text", label, text: json(v), language: "json" });
    }
  }
  return fields.length ? [{ kind: "fields", fields }, ...rest] : rest;
}

const json = (v: unknown) => JSON.stringify(v, null, 2);

function serviceName(source: string | undefined) {
  return source?.match(/\bname:\s*["']([a-z0-9-]+)["']/)?.[1];
}

/** Where a tool step is: waiting for you, done, failed, or running. */
export function status(step: ToolStep): "asking" | "refused" | "failed" | "done" | "running" {
  if (step.refused) {
    return "refused";
  }
  if (step.error !== undefined) {
    return "failed";
  }
  if (step.result !== undefined) {
    return "done";
  }
  return step.ask ? "asking" : "running";
}

/**
 * Where a tool step is, as the chat shows it: a step with no result is
 * running while the agent works, and stopped once it has stopped.
 */
export function state(step: ToolStep, working: boolean): ReturnType<typeof status> | "stopped" {
  const st = status(step);
  return st === "running" && !working ? "stopped" : st;
}

/** The tense a step's words take where it is. */
export function tense(step: ToolStep, working: boolean): Tense {
  switch (state(step, working)) {
    case "running":
      return "now";
    case "asking":
    case "refused":
      return "ask";
    default:
      return "past";
  }
}

/** A run of tool steps with no message between them, which the chat shows as one line. */
export interface Steps {
  kind: "steps";
  key: string;
  steps: ToolStep[];
}

/** A chat's items, with each run of tool steps gathered into one. */
export function grouped(items: readonly Item[]): (Message | Steps)[] {
  const out: (Message | Steps)[] = [];
  for (const item of items) {
    if (item.kind !== "tool") {
      out.push(item);
      continue;
    }
    const last = out.at(-1);
    if (last?.kind === "steps") {
      last.steps.push(item);
    } else {
      out.push({ kind: "steps", key: item.key, steps: [item] });
    }
  }
  return out;
}

/**
 * A run of steps in one line: the step running now, if one is, or a step
 * waiting for you; otherwise how many ran, and how many did not go well.
 */
export function summary(group: Steps, working: boolean): string {
  const states = group.steps.map((s) => state(s, working));
  const running = group.steps.find((_, i) => states[i] === "running");
  if (running) {
    return `${describe(running, "now")}…`;
  }
  if (states.includes("asking")) {
    return "Needs your permission";
  }
  const n = group.steps.length;
  const notes = (["failed", "refused", "stopped"] as const)
    .map((st) => [states.filter((s) => s === st).length, st] as const)
    .filter(([count]) => count > 0)
    .map(([count, st]) => `${count} ${st}`);
  return `Ran ${n} step${n === 1 ? "" : "s"}${notes.length ? ` (${notes.join(", ")})` : ""}`;
}
