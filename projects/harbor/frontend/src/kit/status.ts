// What a widget's Bound tells the Widget frame inside it (runtime.ts).
import { inject, type InjectionKey } from "vue";
import type { SignInNeeded } from "./data";

/** What a Bound tells the Widget frame inside it. */
export interface Status {
  state: "ready" | "loading" | "signIn" | "failed";
  /** Why it failed. */
  problem: string;
  /** What to sign in to, when state is signIn. */
  signIn: SignInNeeded | null;
  /** Signs in to a service (or, without one, to what signIn says), then reads everything again. */
  signInNow(service?: string): Promise<void>;
  /** The services its tools are from, by name. */
  services: string[];
  /** A read is loading again, or a write is running. */
  busy: boolean;
  /** Reads that could not load again, and writes that failed, a line each. */
  notices: string[];
  /** What a destructive write asks before it runs, or "". */
  asking: string;
  answer(yes: boolean): void;
}

export const statusKey: InjectionKey<Status> = Symbol("status");

/** The Bound a Widget frame is in, or null outside a dashboard. */
export const useStatus = () => inject(statusKey, null);

