import { computed } from "vue";
import { sql, useQuery } from "@desktop";
import { applyTheme as apply, builtInThemes, type Mode, type Theme, type Tokens } from "@ui/themes";

export { builtInThemes, colourVariables, complete, dark, parseThemeCSS } from "@ui/themes";
export type { Kind, Mode, Theme, Tokens, Variable } from "@ui/themes";

// Harbor's themes are the built-in ones and those you add, which it keeps
// in its database with the one in use and the mode.

/** The themes, the one in use, and the mode, kept current. */
export function useThemes() {
  const settings = useQuery<{ key: string; value: string }>(
    () => sql`SELECT key, value FROM settings WHERE key IN ('theme', 'mode')`,
  );
  const saved = useQuery<{ name: string; tokens: string }>(() => sql`SELECT name, tokens FROM themes ORDER BY name`);
  const themes = computed<Theme[]>(() => [
    ...builtInThemes,
    ...saved.value.map((t) => ({ name: t.name, ...(JSON.parse(t.tokens) as { light: Tokens; dark: Tokens }), builtIn: false })),
  ]);
  const value = (key: string) => settings.value.find((s) => s.key === key)?.value;
  const current = computed(() => themes.value.find((t) => t.name === value("theme")) ?? builtInThemes[0]);
  const mode = computed(() => (value("mode") ?? "system") as Mode);
  return { themes, current, mode };
}

/** Keeps the window in the theme and mode you chose, or the editor's draft while you edit one. */
export function applyTheme() {
  const { current, mode } = useThemes();
  apply(() => current.value, () => mode.value);
}
