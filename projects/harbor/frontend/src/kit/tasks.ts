// What Tasks and Board share: how a task's status, priority, and due
// date are shown.
import { label } from "./data";
import { daysBetween, today, when } from "./time";
import { guessTone, type Tone } from "./tone";

/** A status as words: "IN_PROGRESS", "in_progress", and "in progress" are "In progress". */
export function words(v: unknown): string {
  const s = v === null || v === undefined ? "" : String(v);
  const w = /^[A-Z0-9_]+$/.test(s) || s.includes("_") ? label(s) : s;
  return w.charAt(0).toUpperCase() + w.slice(1);
}

/** A priority's tone: urgent ones bad, high ones a warning, the rest quiet. */
export function priorityTone(v: unknown): Tone {
  const s = String(v ?? "").toLowerCase();
  if (/urgent|critical|highest|blocker|p0/.test(s)) {
    return "bad";
  }
  return /high|p1/.test(s) ? "warning" : "neutral";
}

/**
 * A due date as words and a tone: "due today" in amber, "due Nov 1" in red
 * once it has passed, unless the task is done.
 */
export function due(v: unknown, done: boolean): { text: string; tone: Tone } | null {
  const w = when(v);
  if (!w) {
    return null;
  }
  const n = daysBetween(today(), w.day);
  const d = new Date(`${w.day}T12:00:00`);
  const text =
    n === 0
      ? "due today"
      : n === 1
        ? "due tomorrow"
        : n === -1
          ? "due yesterday"
          : n > 1 && n < 7
            ? `due ${d.toLocaleDateString(undefined, { weekday: "short" })}`
            : `due ${d.toLocaleDateString(undefined, { month: "short", day: "numeric" })}`;
  return { text, tone: done ? "neutral" : n < 0 ? "bad" : n <= 1 ? "warning" : "neutral" };
}

/** Whether a status means the task is finished: one of done, or, without done, one whose words say so. */
export const doneBy = (done: unknown[] | undefined) => (status: unknown) =>
  done ? done.some((d) => String(d) === String(status)) : guessTone(status) === "good";

/** A priority's tone: from tones, where it has the priority, else as priorityTone says. */
export const priorityToneBy = (tones: Partial<Record<string, Tone>> | undefined) => (v: unknown) =>
  tones?.[String(v)] ?? priorityTone(v);
