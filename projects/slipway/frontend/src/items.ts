import type { Issue, Thread } from "@backend";

/**
 * One piece of work: an issue, its pull request, and the bot's thread on
 * it, shown as one. A bot's thread is always one issue, and the pull
 * request it opened. A pull request the bot did not open joins the issue
 * it says it closes; one that closes no open issue is an item of its own.
 */
export type Item = {
  key: string;
  /** The issue's number, or the pull request's when there is no issue. */
  number: number;
  title: string;
  issue: Issue | null;
  pr: Issue | null;
  thread: Thread | null;
  updatedAt: string;
  /** What it is for: project:<name>, framework, or "" when it has no scope label. */
  scope: string;
};

const isScope = (l: string) => l === "framework" || l.startsWith("project:");

/** What an item is for: its thread's scope, else its issue's or pull request's scope label. */
export function scopeOf(i: Pick<Item, "issue" | "pr" | "thread">): string {
  return i.thread?.scope || i.issue?.labels?.find(isScope) || i.pr?.labels?.find(isScope) || "";
}

export type Status = "working" | "needs-you" | "idle" | "none";

/** What the bot is doing on an item, as the list and the view show it. */
export function status(i: Item): Status {
  switch (i.thread?.state) {
    case "running":
      return "working";
    case "blocked":
      return "needs-you";
    case "idle":
      // An idle thread with something unanswered waits for the bot's next
      // turn, not for you.
      return i.thread.waiting > 0 ? "idle" : "needs-you";
    default:
      return "none";
  }
}

const rank: Record<Status, number> = { "needs-you": 0, working: 1, idle: 2, none: 3 };

export function group(issues: Issue[], prs: Issue[], threads: Thread[]): Item[] {
  const items = new Map<number, Item>();
  const byPR = new Map<number, Item>();
  const later = (a: string, b: string) => (Date.parse(a) >= Date.parse(b) ? a : b);

  for (const issue of issues) {
    items.set(issue.number, {
      key: `issue-${issue.number}`,
      number: issue.number,
      title: issue.title,
      issue,
      pr: null,
      thread: null,
      updatedAt: String(issue.updatedAt),
      scope: "",
    });
  }
  for (const t of threads) {
    let item = items.get(t.number);
    if (!item) {
      // The thread is open but GitHub did not list its issue (closed, or past
      // the first hundred): show it anyway, by number.
      item = { key: `issue-${t.number}`, number: t.number, title: "", issue: null, pr: null, thread: null, updatedAt: String(t.started), scope: "" };
      items.set(t.number, item);
    }
    item.thread = t;
    if (t.pr) byPR.set(t.pr, item);
  }
  const alone: Item[] = [];
  for (const pr of prs) {
    const item = byPR.get(pr.number) ?? (pr.closes ?? []).map((n) => items.get(n)).find((i) => i && !i.pr);
    if (item) {
      item.pr = pr;
      item.title ||= pr.title;
      item.updatedAt = later(item.updatedAt, String(pr.updatedAt));
    } else {
      alone.push({ key: `pr-${pr.number}`, number: pr.number, title: pr.title, issue: null, pr, thread: null, updatedAt: String(pr.updatedAt), scope: "" });
    }
  }
  const all = [...items.values(), ...alone];
  for (const i of all) i.scope = scopeOf(i);
  return all.sort(
    (a, b) => rank[status(a)] - rank[status(b)] || Date.parse(b.updatedAt) - Date.parse(a.updatedAt),
  );
}
