import { describe, expect, it } from "vitest";
import type { Issue, Thread } from "@backend";
import { group, status } from "../items";

const issue = (number: number, extra: Partial<Issue> = {}): Issue =>
  ({ number, title: `Issue ${number}`, updatedAt: "2026-11-01T00:00:00Z", closes: [], ...extra }) as Issue;
const thread = (number: number, extra: Partial<Thread> = {}): Thread =>
  ({ repo: "o/r", number, state: "idle", pr: 0, waiting: 0, started: "2026-11-01T00:00:00Z", ...extra }) as Thread;

describe("group", () => {
  it("puts an issue, the bot's pull request, and its thread in one item", () => {
    const items = group([issue(42)], [issue(43, { title: "PR" })], [thread(42, { pr: 43, state: "running" })]);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ number: 42, title: "Issue 42", pr: { number: 43 }, thread: { number: 42 } });
  });

  it("joins a pull request the bot did not open to the issue it closes", () => {
    const items = group([issue(7)], [issue(8, { closes: [7] })], []);
    expect(items.map((i) => [i.number, i.pr?.number])).toEqual([[7, 8]]);
  });

  it("shows a pull request that closes no open issue on its own", () => {
    const items = group([issue(1)], [issue(2, { closes: [99] })], []);
    expect(items.map((i) => i.key).sort()).toEqual(["issue-1", "pr-2"]);
  });

  it("shows an open thread whose issue gh did not list", () => {
    const items = group([], [issue(5, { title: "The PR" })], [thread(4, { pr: 5 })]);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ number: 4, title: "The PR", issue: null });
  });

  it("lists what needs you first, then what the bot is working on, then the newest", () => {
    const items = group(
      [issue(1, { updatedAt: "2026-11-05T00:00:00Z" }), issue(2), issue(3), issue(4, { updatedAt: "2026-11-03T00:00:00Z" })],
      [],
      [thread(2, { state: "running" }), thread(3, { state: "idle" })],
    );
    expect(items.map((i) => i.number)).toEqual([3, 2, 1, 4]);
  });
});

describe("scope", () => {
  it("is the thread's, else the issue's or pull request's scope label, else none", () => {
    const items = group(
      [issue(1, { labels: ["bug", "project:harbor"] }), issue(2, { labels: ["framework"] }), issue(3, { labels: ["project:harbor"] }), issue(4)],
      [issue(6, { closes: [4], labels: ["project:slipway"] })],
      [thread(3, { scope: "project:slipway" })],
    );
    const byNumber = Object.fromEntries(items.map((i) => [i.number, i.scope]));
    expect(byNumber).toEqual({ 1: "project:harbor", 2: "framework", 3: "project:slipway", 4: "project:slipway" });
    expect(group([issue(9, { labels: ["bug"] })], [], [])[0].scope).toBe("");
  });
});

describe("status", () => {
  it("says an idle thread with nothing unanswered needs you", () => {
    const [i] = group([issue(1)], [], [thread(1)]);
    expect(status(i)).toBe("needs-you");
    const [j] = group([issue(1)], [], [thread(1, { waiting: 2 })]);
    expect(status(j)).toBe("idle");
  });
});
