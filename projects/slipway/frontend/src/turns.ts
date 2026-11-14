// A bot's thread as the issue's timeline shows it: its turns, each one the
// bot's messages and runs of steps, the way Harbor's agent panel shows a
// chat. The lines come from the server's live stream (see internal/bot's
// describe): a turn starts with a prompt line; a tool has a tool line when
// it starts, "tool: title", and a result or error line when it ends, "tool:
// title" then its output; text and reasoning lines are whole. Reasoning is
// left out, as in Harbor's panel: the bot's whole log still has it.

export type Line = { id: number; time: string; kind: string; text: string };

export type StepState = "running" | "done" | "failed";

export interface Step {
  kind: "step";
  key: string;
  /** opencode's tool. */
  tool: string;
  title: string;
  output: string;
  state: StepState;
}

export interface Message {
  kind: "text";
  key: string;
  text: string;
  /** An error of the turn itself, such as the model's, rather than a tool's. */
  error?: boolean;
}

export interface Steps {
  kind: "steps";
  key: string;
  steps: Step[];
}

export interface Turn {
  key: string;
  start: string;
  end: string;
  /** What the bot was asked, without the line that names its session. */
  prompt: string;
  items: (Message | Steps)[];
}

/** Splits a line into its tool and the rest. */
function split(text: string): { tool: string; title: string; output: string } {
  const nl = text.indexOf("\n");
  const head = nl < 0 ? text : text.slice(0, nl);
  const output = nl < 0 ? "" : text.slice(nl + 1);
  const m = head.match(/^([A-Za-z_][\w.-]*): (.*)$/);
  if (!m) {
    return { tool: "", title: head, output };
  }
  return { tool: m[1], title: m[2], output };
}

/** A thread's lines, oldest first, as turns. Lines before the first prompt are a turn of their own. */
export function turns(lines: readonly Line[]): Turn[] {
  const out: Turn[] = [];
  let turn: Turn | null = null;
  const begin = (l: Line, prompt: string) => {
    turn = { key: `t${l.id}`, start: l.time, end: l.time, prompt, items: [] };
    out.push(turn);
    return turn;
  };
  const push = (t: Turn, item: Message | Step) => {
    if (item.kind === "text") {
      t.items.push(item);
      return;
    }
    const last = t.items.at(-1);
    if (last?.kind === "steps") {
      last.steps.push(item);
    } else {
      t.items.push({ kind: "steps", key: item.key, steps: [item] });
    }
  };
  // The steps of a turn still waiting for their result, by tool.
  let open: Step[] = [];

  for (const l of lines) {
    if (l.kind === "prompt") {
      // "=== <time> session <id>" then the prompt.
      const nl = l.text.indexOf("\n");
      begin(l, (l.text.startsWith("===") ? (nl < 0 ? "" : l.text.slice(nl + 1)) : l.text).trim());
      open = [];
      continue;
    }
    const t: Turn = turn ?? begin(l, "");
    t.end = l.time;
    const key = `l${l.id}`;
    switch (l.kind) {
      case "text":
        if (l.text.trim()) push(t, { kind: "text", key, text: l.text });
        break;
      case "tool": {
        const s = split(l.text);
        const step: Step = { kind: "step", key, tool: s.tool, title: s.title, output: "", state: "running" };
        open.push(step);
        push(t, step);
        break;
      }
      case "result":
      case "error": {
        const s = split(l.text);
        if (!s.tool) {
          // Not a tool's: the turn's own error.
          if (l.kind === "error") push(t, { kind: "text", key, text: l.text, error: true });
          break;
        }
        const i = open.findIndex((o) => o.tool === s.tool);
        const state: StepState = l.kind === "error" ? "failed" : "done";
        if (i >= 0) {
          const step = open[i];
          open.splice(i, 1);
          // The title a tool ends with says more than the input it started with.
          step.title = s.title || step.title;
          step.output = s.output;
          step.state = state;
        } else {
          push(t, { kind: "step", key, tool: s.tool, title: s.title, output: s.output, state });
        }
        break;
      }
    }
  }
  return out;
}

export type Tense = "past" | "now";
const verb = (past: string, now: string) => ({ past, now });
// opencode's tools, in words.
const verbs: Record<string, { past: string; now: string }> = {
  bash: verb("Ran", "Running"),
  read: verb("Read", "Reading"),
  edit: verb("Edited", "Editing"),
  write: verb("Wrote", "Writing"),
  patch: verb("Patched", "Patching"),
  grep: verb("Searched for", "Searching for"),
  glob: verb("Found files matching", "Finding files matching"),
  list: verb("Listed", "Listing"),
  webfetch: verb("Fetched", "Fetching"),
  task: verb("Ran a subagent:", "Running a subagent:"),
  todowrite: verb("Updated the to-do list", "Updating the to-do list"),
  todoread: verb("Read the to-do list", "Reading the to-do list"),
};

/**
 * What a step was about. A tool that has not ended yet has only its input,
 * cut to 200 characters, so it may not be whole JSON: the field is found by
 * name.
 */
function subject(step: Step): { text: string; code: boolean } {
  const t = step.title.trim();
  if (!t.startsWith("{")) {
    return { text: t, code: false };
  }
  for (const field of ["command", "filePath", "path", "pattern", "url", "description"]) {
    const m = t.match(new RegExp(`"${field}"\\s*:\\s*"((?:[^"\\\\]|\\\\.)*)`));
    if (m) {
      let v = m[1];
      try {
        v = JSON.parse(`"${v}"`);
      } catch {
        // Cut off in the middle of an escape: show it as it is.
      }
      return { text: v, code: field !== "description" };
    }
  }
  return { text: t, code: true };
}

/** A step in words: a verb, then what it was about, which may be code. */
export function describe(step: Step, tense: Tense): { verb: string; text: string; code: boolean } {
  const s = subject(step);
  const v = verbs[step.tool];
  if (step.tool === "bash" && !s.code && /^[A-Z][^/|;=$"'`]*$/.test(s.text)) {
    // bash's title is its description when it has one, a sentence of its own.
    return { verb: "", text: s.text, code: false };
  }
  if (step.tool.startsWith("todo")) {
    return { verb: v[tense], text: "", code: false };
  }
  if (!v) {
    return { verb: step.tool, text: s.text, code: s.code };
  }
  // Paths and patterns read as code.
  return { verb: v[tense], text: s.text, code: s.code || ["bash", "read", "edit", "write", "patch", "grep", "glob", "list"].includes(step.tool) };
}

/** A step as one string, for a summary or a tooltip. */
export function sentence(step: Step, tense: Tense): string {
  const d = describe(step, tense);
  return [d.verb, d.text].filter(Boolean).join(" ") || step.tool;
}

/** A step's state, given whether the bot is still working: one left running when the turn ended stopped. */
export function state(step: Step, working: boolean): StepState | "stopped" {
  return step.state === "running" && !working ? "stopped" : step.state;
}

/**
 * A run of steps in one line: the step running now, if one is; otherwise
 * how many ran and how many failed.
 */
export function summary(group: Steps, working: boolean): string {
  const running = group.steps.findLast((s) => state(s, working) === "running");
  if (running) {
    return `${sentence(running, "now")}…`;
  }
  const tools = group.steps;
  const failed = tools.filter((s) => state(s, working) === "failed").length;
  const stopped = tools.filter((s) => state(s, working) === "stopped").length;
  const notes = [failed && `${failed} failed`, stopped && `${stopped} stopped`].filter(Boolean);
  const n = tools.length;
  return `Ran ${n} step${n === 1 ? "" : "s"}${notes.length ? ` (${notes.join(", ")})` : ""}`;
}

/** How long something took, from its start and end: "40 s", "6 min", "2 h 4 min". */
export function took(start: string, end: string): string {
  const s = Math.max(0, Math.round((Date.parse(end) - Date.parse(start)) / 1000));
  if (s < 60) return `${s} s`;
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h${m % 60 ? ` ${m % 60} min` : ""}`;
}
