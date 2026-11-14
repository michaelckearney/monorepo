import * as Vue from "vue";
import type { Component } from "vue";
import { Tools } from "@backend";
import { fileURL } from "./files";
import { format, type SignInNeeded } from "./kit/data";
import { computed, runtime, type Use } from "./kit/runtime";

export type { SignInNeeded } from "./kit/data";
export type { Use } from "./kit/runtime";

/**
 * The kit: every component in kit/'s folders, by its file's name. Adding a
 * file adds it to "harbor"; the backend reads the same files to search them
 * (kit/kit.go).
 */
export const kit: Record<string, Component> = Object.fromEntries(
  Object.entries(import.meta.glob<Component>("./kit/*/*.vue", { eager: true, import: "default" })).map(([path, c]) => [
    path.slice(path.lastIndexOf("/") + 1, -".vue".length),
    c,
  ]),
);

/** What the page showing a dashboard gives its "harbor" module. */
export interface Page {
  /** The dashboard's id, so Harbor knows which dashboard calls. */
  dashboard: number;
  /** Offers a sign-in that a write found you need, above the dashboard. */
  needsSignIn(n: SignInNeeded): void;
  /** Takes back a sign-in that a read's widget shows itself. */
  shown(n: SignInNeeded): void;
}

/**
 * A dashboard's Harbor: the "harbor" module it imports, and what the page
 * does with it. The module has useService, which reaches the services'
 * tools (kit/runtime.ts), format and fileURL, and the kit's components.
 */
export function harbor(page: Page) {
  const r = runtime({
    call: async (service, tool, input) =>
      JSON.parse(await Tools.CallForDashboard(page.dashboard, service, tool, JSON.stringify(input))),
    needsSignIn: page.needsSignIn,
    shown: page.shown,
  });
  return {
    module: { useService: r.useService, format, fileURL, Bound: r.Bound, ...kit },
    /** Runs every read again, keeping what each shows until it has. */
    refresh: r.refresh,
    dispose: r.dispose,
    /** Loads a compiled dashboard: its component, which uses these tools. */
    load(js: string, uses: Use[]): Component {
      r.setUses(uses);
      return load(js, this.module);
    },
  };
}

/**
 * The component in a compiled dashboard: a CommonJS module, from
 * Documents.CompiledDashboard, that gets "vue" and "harbor" with require.
 * The backend's compiler allows it nothing else. Its "vue" has the runtime's
 * computed, which throws each time it is read while what it read is loading.
 */
function load(js: string, harborModule: Record<string, unknown>): Component {
  const modules: Record<string, unknown> = { vue: { ...Vue, computed }, harbor: harborModule };
  const module = { exports: {} as { default?: Component } };
  const require = (name: string) => modules[name];
  new Function("require", "module", "exports", js)(require, module, module.exports);
  return module.exports.default!;
}
