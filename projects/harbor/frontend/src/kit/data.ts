// What the dashboard kit's pieces share: how to find and show a value.

/** A login you are not through: a service's, or its tool's if tool is set. */
export interface SignInNeeded {
  service: string;
  tool?: string;
  heading: string;
  message: string;
}

/** A value in data by a path such as "account.balance" or "items.0.name". */
export function pick(data: unknown, path: string | undefined): unknown {
  if (!path) {
    return data;
  }
  let v = data;
  for (const key of path.split(".")) {
    v = v == null ? undefined : (v as Record<string, unknown>)[key];
  }
  return v;
}

/** How a value is shown. */
export type Format = "number" | "money" | "percent" | "date" | "month" | "datetime" | "time" | "text";

/** A value as text, as format says. A date is an ISO string, or a number of milliseconds. */
export function format(v: unknown, f?: Format, currency = "USD"): string {
  if (v === null || v === undefined || v === "") {
    return "—";
  }
  switch (f) {
    case "money":
      return new Intl.NumberFormat(undefined, { style: "currency", currency }).format(Number(v));
    case "percent":
      // 0.25 and 25 both read as 25%: a tool may give either.
      return new Intl.NumberFormat(undefined, { style: "percent", maximumFractionDigits: 1 }).format(
        Math.abs(Number(v)) > 1 ? Number(v) / 100 : Number(v),
      );
    case "number":
      return new Intl.NumberFormat().format(Number(v));
    case "date":
    case "month":
    case "datetime":
    case "time": {
      // A date without a time is that day, or month, wherever you are.
      const dateOnly = typeof v === "string" && /^\d{4}-\d{2}(-\d{2})?$/.test(v);
      const d = new Date(dateOnly ? `${v.length === 7 ? `${v}-01` : v}T00:00:00Z` : (v as string | number));
      if (Number.isNaN(d.getTime())) {
        return String(v);
      }
      const opts: Intl.DateTimeFormatOptions =
        f === "month"
          ? { month: "short", year: d.getFullYear() === new Date().getFullYear() ? undefined : "numeric" }
          : f === "date"
          ? { month: "short", day: "numeric", year: d.getFullYear() === new Date().getFullYear() ? undefined : "numeric" }
          : f === "time"
            ? { hour: "numeric", minute: "2-digit" }
            : { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" };
      return d.toLocaleString(undefined, dateOnly ? { ...opts, timeZone: "UTC" } : opts);
    }
  }
  return typeof v === "number" ? new Intl.NumberFormat().format(v) : String(v);
}

/** A field's name as a heading: "due_date" and "dueDate" are "Due date". */
export function label(key: string): string {
  const words = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_.-]+/g, " ")
    .trim()
    .toLowerCase();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/** A table's column: a field, its heading, and how its values are shown; value works one out. */
export interface Column {
  key: string;
  label?: string;
  format?: Format;
  value?: (row: any) => unknown;
}

/** A column from "amount", "amount:money", or { key, label, format }. */
export function column(c: string | Column): Required<Column> {
  if (typeof c === "string") {
    const [key, f] = c.split(":");
    return { key, label: label(key.split(".").pop()!), format: (f as Format) || "text", value: (r) => pick(r, key) };
  }
  return {
    key: c.key,
    label: c.label ?? label(c.key.split(".").pop()!),
    format: c.format ?? "text",
    value: c.value ?? ((r) => pick(r, c.key)),
  };
}

/** Whether a format's values line up on the right, as numbers do. */
export const numeric = (f: Format) => f === "number" || f === "money" || f === "percent";

/**
 * How a piece finds a value in an item: a field, with a format after a
 * colon ("due:date"), or a function of the item that returns the value.
 */
export type Getter = string | ((item: any) => unknown);

/** The value a getter finds in an item, as text. */
export function show(item: unknown, getter: Getter | undefined, f?: Format, currency?: string): string {
  if (getter === undefined) {
    return "";
  }
  if (typeof getter === "function") {
    return format(getter(item), f, currency);
  }
  const [key, own] = getter.split(":");
  return format(pick(item, key), (own as Format) || f, currency);
}

/** The value a getter finds in an item, as it is. */
export function get(item: unknown, getter: Getter | undefined): unknown {
  if (getter === undefined) {
    return undefined;
  }
  return typeof getter === "function" ? getter(item) : pick(item, getter.split(":")[0]);
}
