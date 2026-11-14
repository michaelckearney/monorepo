import { computed, effectScope, type ComputedRef } from "vue";
import { sql, useQuery } from "@desktop";
import { Settings } from "@backend";
import { applyTheme as apply, builtInThemes, type Mode, type Theme, type Tokens } from "@ui/themes";
import type { Panel } from "@ui/panels";

// What the window keeps of its own, in the app's database: the theme and
// mode, the side panels' widths, and which of the sidebar's sections are
// folded. Settings.SetPreference writes them, and the live query brings them
// back. Themes you add are kept too, each as a `theme:<name>` row that
// Settings.SaveTheme writes; the built-in themes are in @ui/themes.

export type Side = "left" | "right";

/** The panels as they start: the sidebar open, the output collapsed until you want it. */
export const defaultPanels: Record<Side, Panel> = {
  left: { width: 260, collapsed: false },
  right: { width: 440, collapsed: true },
};

export interface Preferences {
  theme: Theme;
  /** The built-in themes and the ones you added, for the picker. */
  themes: Theme[];
  mode: Mode;
  panels: Record<Side, Panel>;
  folded: string[];
}

let preferences: ComputedRef<Preferences> | undefined;

/** The preferences, kept current: one query for the whole window, whichever component asks first. */
export function usePreferences(): ComputedRef<Preferences> {
  preferences ??= effectScope(true).run(() => {
    const rows = useQuery<{ key: string; value: string }>(
      () =>
        sql`SELECT key, value FROM settings
          WHERE key IN ('theme', 'mode', 'left_panel', 'right_panel', 'folded') OR key LIKE 'theme:%'`,
    );
    const value = (key: string) => rows.value.find((r) => r.key === key)?.value;
    const json = <T>(key: string, fallback: T): T => {
      try {
        return (JSON.parse(value(key) ?? "null") as T) ?? fallback;
      } catch {
        return fallback;
      }
    };
    return computed(() => {
      const yours: Theme[] = [];
      for (const r of rows.value) {
        if (!r.key.startsWith("theme:")) continue;
        try {
          const t = JSON.parse(r.value) as { light: Tokens; dark: Tokens };
          yours.push({ name: r.key.slice("theme:".length), light: t.light, dark: t.dark, builtIn: false });
        } catch {
          // A row that is not a theme cannot be worn, so it is skipped.
        }
      }
      yours.sort((a, b) => a.name.localeCompare(b.name));
      const themes = [...builtInThemes, ...yours];
      return {
        themes,
        theme: themes.find((t) => t.name === value("theme")) ?? builtInThemes[0],
        mode: (value("mode") ?? "system") as Mode,
        panels: { left: json("left_panel", defaultPanels.left), right: json("right_panel", defaultPanels.right) },
        folded: json<string[]>("folded", []),
      };
    });
  })!;
  return preferences;
}

export function setTheme(name: string) {
  void Settings.SetPreference("theme", name);
}

export function setMode(mode: Mode) {
  void Settings.SetPreference("mode", mode);
}

export function setPanel(side: Side, panel: Panel) {
  void Settings.SetPreference(`${side}_panel`, JSON.stringify(panel));
}

/** Opens a side panel collapsed to its strip, at the width it had. */
export function expand(side: Side) {
  const panel = usePreferences().value.panels[side];
  if (panel.collapsed) {
    setPanel(side, { ...panel, collapsed: false });
  }
}

/** Folds a section of the sidebar, or opens it. */
export function toggleFolded(key: string) {
  const folded = usePreferences().value.folded;
  void Settings.SetPreference("folded", JSON.stringify(folded.includes(key) ? folded.filter((k) => k !== key) : [...folded, key]));
}

/** Keeps the window in the theme and mode you chose. */
export function applyTheme() {
  const p = usePreferences();
  apply(() => p.value.theme, () => p.value.mode);
}
