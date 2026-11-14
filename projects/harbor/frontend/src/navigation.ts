import { computed, effectScope, ref, type ComputedRef } from "vue";
import { sql, useQuery } from "@desktop";
import { Preferences } from "@backend";
import type { Panel } from "@ui/panels";

export type Kind = "service" | "dashboard";

/**
 * What the middle of the window shows. A document's id 0 is a new, unsaved
 * one, which is always shown in the editor. null shows the dashboard you
 * last opened, or your first.
 */
export type Place = { kind: Kind; id: number; editing: boolean } | { kind: "services" };

// One window, so one place: every component shares it.
export const place = ref<Place | null>(null);

/**
 * What the window shows in dev mode, as chosen in the developer bar: Harbor
 * itself, or a developer tool. Harbor keeps its place underneath, so going
 * back finds it as it was.
 */
export type DevView = "harbor" | "tools" | "benchmarks" | "components";
export const devView = ref<DevView>("harbor");

export function open(kind: Kind, id: number, editing = false) {
  place.value = { kind, id, editing: editing || id === 0 };
  if (kind === "dashboard" && id !== 0 && !editing) {
    void Preferences.OpenedDashboard(id);
  }
}

export function openServices() {
  place.value = { kind: "services" };
}

/** A part of Settings, which its nav lists. */
export type SettingsSection = "providers" | "appearance" | "logins" | "connections";

/**
 * The part of Settings open over the window, or null when Settings is closed.
 * Settings opens over the place, which stays as it was underneath.
 */
export const settings = ref<SettingsSection | null>(null);

export function openSettings(section: SettingsSection = "providers") {
  settings.value = section;
}

export function close() {
  place.value = null;
}

/** The chat in the agent's panel. "" is a new chat, saved when you send its first message. */
export const chat = ref("");

/** A side panel of the window, which you resize by dragging its inner edge. */
export type Side = "sidebar" | "agent";

export type { Panel };

export const defaultPanels: Record<Side, Panel> = {
  sidebar: { width: 240, collapsed: false },
  agent: { width: 384, collapsed: false },
};

let panels: ComputedRef<Record<Side, Panel>> | undefined;

export function sidePanels(): ComputedRef<Record<Side, Panel>> {
  // One query for the whole window, whichever component asks first.
  panels ??= effectScope(true).run(() => {
    const rows = useQuery<{ key: string; value: string }>(
      () => sql`SELECT key, value FROM settings WHERE key IN ('sidebar_panel', 'agent_panel')`,
    );
    const saved = (side: Side) => rows.value.find((r) => r.key === `${side}_panel`)?.value;
    return computed(() => ({
      sidebar: JSON.parse(saved("sidebar") ?? "null") ?? defaultPanels.sidebar,
      agent: JSON.parse(saved("agent") ?? "null") ?? defaultPanels.agent,
    }));
  })!;
  return panels;
}

export function setPanel(side: Side, panel: Panel) {
  void Preferences.SetPanel(side, Math.round(panel.width), panel.collapsed);
}

/** Opens a side panel collapsed to its strip, at the width it had. */
export function expand(side: Side) {
  const panel = sidePanels().value[side];
  if (panel.collapsed) {
    setPanel(side, { ...panel, collapsed: false });
  }
}

/** A message put in a new chat's box for you to send, as askAgent leaves it. */
export const prefill = ref("");

/** Starts a new chat with text in its box, unsent: you read it, and send it. */
export function askAgent(text: string) {
  prefill.value = text;
  openChat();
}

/** Shows a chat in the agent's panel, and opens the panel if it is collapsed. */
export function openChat(id = "") {
  chat.value = id;
  expand("agent");
}
