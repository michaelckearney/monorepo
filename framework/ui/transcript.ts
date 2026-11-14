// What a transcript shows (components/Transcript.vue): your messages, the
// agent's, and runs of the steps it took. Each app makes its own record of
// a conversation into these.

export type TranscriptItem =
  | { kind: "you"; key: string; text: string }
  /** The agent's words, as Markdown; an error's are shown as they are. */
  | { kind: "agent"; key: string; text: string; error?: boolean }
  | TranscriptSteps;

/** A run of steps with nothing said between them, shown as one line that opens to them. */
export interface TranscriptSteps {
  kind: "steps";
  key: string;
  /** The line, such as "Ran 3 steps". */
  summary: string;
  /** It needs you: its line is in the primary colour. */
  attention: boolean;
  /** It starts open. */
  open: boolean;
  steps: TranscriptStep[];
}

export interface TranscriptStep {
  key: string;
  /** What it did, or is doing. */
  text: string;
  /** A word beside it, such as "failed"; "" for a step that went well. */
  state: string;
  tone: "primary" | "destructive" | "muted";
  /** It has more to show, in the transcript's step slot, and opens to it. */
  opens: boolean;
  /** It starts open. */
  open: boolean;
  /** It needs you: it is tinted. */
  attention: boolean;
}
