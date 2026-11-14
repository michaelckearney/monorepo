// What a status means, as a colour: one vocabulary for every piece that
// shows one, so "missing" is the same red on every dashboard.
import { get, type Getter } from "./data";

export type Tone = "neutral" | "good" | "warning" | "bad" | "info";

const tones: Tone[] = ["neutral", "good", "warning", "bad", "info"];
export const isTone = (v: unknown): v is Tone => tones.includes(v as Tone);

/**
 * How a piece finds an item's tone: a tone ("bad"), a field or a function
 * that gives one, or a map from the item's status to a tone
 * ({ missing: "bad", late: "warning" }).
 */
export type ToneGetter = Getter | Partial<Record<string, Tone>>;

// The tone a status word has when nothing says otherwise. Checked in order:
// "not paid" is bad before "paid" is good.
const guesses: [RegExp, Tone][] = [
  [/\b(fail|error|missing|overdue|late\b|offline|down\b|cancel|declin|reject|denied|abnormal|out of range|critical|urgent|expired|blocked|not paid|unpaid)/i, "bad"],
  [/\b(pending|due|warn|review|waiting|requested|low\b|delayed|soon|high\b|flagged|partial|degraded|draft)/i, "warning"],
  [/\b(done|complete|paid|delivered|approved|online|ok|normal|up|active|success|resolved|submitted|graded|final|healthy|connected|available|read)\b/i, "good"],
  [/\b(progress|shipped|transit|scheduled|new\b|open\b|assigned|todo|to do|out for delivery|processing|info)/i, "info"],
];

/** The tone a status word suggests, or neutral. */
export function guessTone(status: unknown): Tone {
  if (status === null || status === undefined || status === "") {
    return "neutral";
  }
  const word = String(status).replace(/_/g, " ");
  return guesses.find(([re]) => re.test(word))?.[1] ?? "neutral";
}

/**
 * An item's tone: from tone if it is set, else guessed from its status.
 * status is the item's status value, for a map or a guess.
 */
export function toneOf(item: unknown, tone: ToneGetter | undefined, status?: unknown): Tone {
  if (tone === undefined) {
    return guessTone(status);
  }
  if (typeof tone === "object") {
    return tone[String(status)] ?? guessTone(status);
  }
  if (isTone(tone)) {
    return tone;
  }
  const v = get(item, tone);
  return isTone(v) ? v : guessTone(v);
}

/** Text in a tone's colour. */
export const toneText: Record<Tone, string> = {
  neutral: "text-muted-foreground",
  good: "text-success",
  warning: "text-warning",
  bad: "text-destructive",
  info: "text-primary",
};

/** A tone's colour as a background, for a dot or a bar. */
export const toneFill: Record<Tone, string> = {
  neutral: "bg-muted-foreground/60",
  good: "bg-success",
  warning: "bg-warning",
  bad: "bg-destructive",
  info: "bg-primary",
};

/** A tone's soft background and its text, for a badge. */
export const toneSoft: Record<Tone, string> = {
  neutral: "bg-muted text-muted-foreground",
  good: "bg-success/15 text-success",
  warning: "bg-warning/15 text-warning",
  bad: "bg-destructive/15 text-destructive",
  info: "bg-primary/15 text-primary",
};
