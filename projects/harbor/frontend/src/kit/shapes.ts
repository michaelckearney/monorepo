// The data each widget in the catalog shows. A dashboard makes it from what
// its services' tools return, in a computed: the widgets know nothing of
// where it is from.
import type { Format } from "./data";
import type { Tone } from "./tone";

/** A task, issue, assignment, or chore. */
export interface Task {
  title: string;
  /** A short id, such as "WEB-142". */
  key?: string;
  /** Its state, shown as a badge coloured by what it says, unless tone says. */
  status?: string;
  /** "good", "warning", "bad", "info", or "neutral". */
  tone?: Tone;
  /** When it is due: a date ("2026-11-05") or a time. */
  due?: string;
  who?: string;
  priority?: string;
  detail?: string;
  link?: string;
  /** The heading it goes under. Tasks with the same group come together. */
  group?: string;
}

/** A card on a board: a task in a column. */
export interface BoardCard {
  title: string;
  /** The id of the column it is in. */
  column: string;
  key?: string;
  who?: string;
  priority?: string;
  due?: string;
  link?: string;
}

/** A board's column. */
export interface BoardColumn {
  id: string;
  title: string;
}

/** A message, email, or mention. */
export interface Message {
  from: string;
  subject?: string;
  /** A line of what it says. */
  preview?: string;
  /** When it was sent. */
  at?: string;
  unread?: boolean;
  link?: string;
  /** The sender's picture, a URL. */
  avatar?: string;
}

/** Something at a time: an appointment, a class, a deadline, time off. */
export interface AgendaEvent {
  title: string;
  /** A date ("2026-11-05", all day) or a time ("2026-11-05T09:30:00"). */
  start: string;
  /** A later date or time, for a range. */
  end?: string;
  detail?: string;
  where?: string;
  /** Who it is for, shown as an avatar. */
  who?: string;
  /** The bar beside it: "good", "warning", "bad", "info" (unless set), or "neutral". */
  tone?: Tone;
  link?: string;
}

/** Money in (above zero) or out (below). */
export interface LedgerEntry {
  title: string;
  amount: number;
  date?: string;
  account?: string;
  category?: string;
  link?: string;
}

/** A row of a list. */
export interface ListItem {
  title: string;
  detail?: string;
  /** Shown on the right, as the list's format says. */
  value?: number | string;
  link?: string;
  group?: string;
  /** From 0 to 100: a bar under it. */
  meter?: number;
}

/** How full one thing is: percent, or value out of max. */
export interface Gauge {
  label: string;
  percent?: number;
  value?: number;
  max?: number;
  /** What it says under the label; with value and max, "1,610 of 2,000 GB used" unless set. */
  detail?: string;
}

/** One figure among a few. */
export interface Stat {
  label: string;
  value: number | string;
  format?: Format;
  /** A small line under it. */
  note?: string;
}

/** A thing and its state. */
export interface StatusItem {
  label: string;
  /** Its state, shown as a dot and a word coloured by what it says, unless tone says. */
  status?: string;
  /** "good", "warning", "bad", "info", or "neutral". */
  tone?: Tone;
  detail?: string;
  /** Shown on the right. */
  value?: string;
  link?: string;
}

/** A picture. */
export interface Picture {
  src: string;
  /** What it shows. */
  alt: string;
  caption?: string;
  link?: string;
}

/** A line of facts: a label and its value. */
export interface Fact {
  label: string;
  value: unknown;
  format?: Format;
}

/** A point of a chart's series. */
export interface Point {
  /** Where along the bottom: a label, a date, or a month. */
  x: string | number;
  y: number;
}

/** A line or a set of bars on a chart. */
export interface Series {
  name: string;
  points: Point[];
}
