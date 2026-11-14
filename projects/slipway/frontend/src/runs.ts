import { Local } from "@backend";
import type { Run } from "@backend";

// This computer's runs: each project of a monorepo in its own harness run,
// all at once, each running one branch at a time, the default branch or a
// pull request. pr is 0 for the default branch, which is the clone's own
// checkout, whose key is "".

/** A project's run, of its monorepo's. */
export const runOf = (runs: Run[] | undefined, project: string) => runs?.find((r) => r.project === project) ?? null;

/** Whether a run is going, or this app started it. */
export const active = (r: Run | null | undefined) => !!r && (r.running || r.started || busy(r));

/**
 * The run an item's view shows: its project's, or, for an item for no one
 * project, the one that runs, else the first.
 */
export const runFor = (runs: Run[] | undefined, project: string) =>
  project ? runOf(runs, project) : (runs?.find(active) ?? runs?.[0] ?? null);

/** Whether the run is still being cloned or started. */
export const busy = (r: Run | null | undefined) => r?.phase === "cloning" || r?.phase === "starting";

/**
 * Whether this pull request, or with 0 the default branch, is what runs;
 * with a project, only if it is that project that runs.
 */
export const runsHere = (r: Run | null | undefined, pr: number, project = "") =>
  !!r?.running && (pr ? r.pr === pr : r.current === "") && (!project || r.project === project);

/** What runs, in a word: "#11", the default branch's name, or "another branch". */
export function running(r: Run | null | undefined, base: string): string {
  if (!r?.running) return "";
  if (r.pr) return `#${r.pr}`;
  return r.current ? "another branch" : base;
}

/** The project an item is for, from its scope, such as project:harbor; "" for any. */
export const projectOf = (scope: string | undefined) => (scope?.startsWith("project:") ? scope.slice("project:".length) : "");

/**
 * Runs a pull request, or with 0 the default branch, in place of what runs
 * now. A fork's pull request runs its code on this computer, so it asks
 * first; it returns false when you say no.
 */
export async function runHere(repo: string, pr: number, fork: boolean, project = "", mode = ""): Promise<boolean> {
  if (fork && !confirm(`Pull request #${pr} is from a fork. Running it runs its Go code and npm install scripts on this computer. Run it?`)) {
    return false;
  }
  await Local.Run(repo, project, mode, pr, fork);
  return true;
}
