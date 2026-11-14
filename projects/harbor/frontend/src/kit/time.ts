// Dates and times as people say them: "Today", "Thu, Nov 5", "in 2 days".
//
// A tool's "2026-11-05" is that day wherever you are, and its
// "2026-11-05T09:30:00" is 9:30 as the site said it, so both are read as
// written rather than through a time zone. Only a time with a zone or a
// number of milliseconds is an instant, shown in your own time.

/** A date or time a tool gave, as its day and, if it has one, its time. */
export interface When {
  /** YYYY-MM-DD. */
  day: string;
  /** HH:MM, or "" for a whole day. */
  time: string;
}

const pad = (n: number) => String(n).padStart(2, "0");
const isoDay = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

/** Today where you are, as YYYY-MM-DD. */
export const today = () => isoDay(new Date());

/** The day n days after day. */
export function addDays(day: string, n: number): string {
  const d = new Date(`${day}T12:00:00`);
  d.setDate(d.getDate() + n);
  return isoDay(d);
}

/** Whole days from a to b: 1 when b is the day after a. */
export function daysBetween(a: string, b: string): number {
  return Math.round((Date.parse(`${b}T12:00:00Z`) - Date.parse(`${a}T12:00:00Z`)) / 86_400_000);
}

/** A tool's date or time as a When, or null if it is not one. */
export function when(v: unknown): When | null {
  if (v === null || v === undefined || v === "") {
    return null;
  }
  if (typeof v === "string") {
    const m = /^(\d{4}-\d{2}-\d{2})(?:[T ](\d{2}:\d{2})(?::[\d.]+)?)?$/.exec(v);
    if (m) {
      return { day: m[1], time: m[2] ?? "" };
    }
  }
  const d = new Date(v as string | number);
  if (Number.isNaN(d.getTime())) {
    return null;
  }
  return { day: isoDay(d), time: `${pad(d.getHours())}:${pad(d.getMinutes())}` };
}

/** A When as milliseconds, for sorting and for how long ago. */
export const instant = (w: When) => Date.parse(`${w.day}T${w.time || "00:00"}:00`);

const sameYear = (day: string) => day.slice(0, 4) === today().slice(0, 4);

/** "Nov 5", or "Nov 5, 2025" in another year. */
export function dateText(day: string): string {
  return new Date(`${day}T12:00:00`).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: sameYear(day) ? undefined : "numeric",
  });
}

/** "9:30 AM". */
export function timeText(time: string): string {
  return new Date(`2000-01-01T${time}:00`).toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" });
}

/** "Today", "Tomorrow", "Yesterday", "Thu, Nov 5". */
export function dayText(day: string): string {
  const n = daysBetween(today(), day);
  if (n === 0) {
    return "Today";
  }
  if (n === 1) {
    return "Tomorrow";
  }
  if (n === -1) {
    return "Yesterday";
  }
  return new Date(`${day}T12:00:00`).toLocaleDateString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    year: sameYear(day) ? undefined : "numeric",
  });
}

/**
 * How far from now: "just now", "5m ago", "3h ago", "in 2 days",
 * "yesterday". A whole day counts days.
 */
export function relativeText(w: When): string {
  if (!w.time) {
    const n = daysBetween(today(), w.day);
    if (n === 0) {
      return "today";
    }
    if (n === 1) {
      return "tomorrow";
    }
    if (n === -1) {
      return "yesterday";
    }
    if (Math.abs(n) < 7) {
      return n > 0 ? `in ${n} days` : `${-n} days ago`;
    }
    return dateText(w.day);
  }
  const minutes = Math.round((instant(w) - Date.now()) / 60_000);
  const m = Math.abs(minutes);
  const say = (s: string) => (minutes < 0 ? `${s} ago` : `in ${s}`);
  if (m < 1) {
    return "just now";
  }
  if (m < 60) {
    return say(`${m}m`);
  }
  if (m < 24 * 60) {
    return say(`${Math.round(m / 60)}h`);
  }
  return relativeText({ day: w.day, time: "" });
}
