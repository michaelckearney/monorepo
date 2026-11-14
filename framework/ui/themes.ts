import { ref, watchEffect } from "vue";

// The themes every app can wear, and what puts one on the window. Where an
// app keeps the theme and mode you chose is its own: applyTheme is given them.

/** A theme's CSS variables, by name without the dashes: "primary": "#0e5e6f". */
export type Tokens = Record<string, string>;

export interface Theme {
  name: string;
  light: Tokens;
  dark: Tokens;
  /** One of the built-in themes, which you cannot delete. */
  builtIn: boolean;
}

export type Mode = "light" | "dark" | "system";

/**
 * Fills in what a theme leaves out from what it has, as shadcn's themes do:
 * a theme needs only its main colours. Anything still missing is the default
 * theme's, from style.css.
 */
export function complete(t: Tokens): Tokens {
  const from = (k: string, v: string | undefined) => (t[k] || !v ? {} : { [k]: v });
  return {
    ...t,
    ...from("card", t.background),
    ...from("card-foreground", t.foreground),
    ...from("popover", t.card ?? t.background),
    ...from("popover-foreground", t.foreground),
    ...from("secondary", t.muted),
    ...from("secondary-foreground", t.foreground),
    ...from("accent-foreground", t.foreground),
    ...from("input", t.border),
    ...from("ring", t.primary),
    ...from("sidebar-foreground", t.foreground),
    ...from("sidebar-primary", t.primary),
    ...from("sidebar-primary-foreground", t["primary-foreground"]),
    ...from("sidebar-accent", t.accent),
    ...from("sidebar-accent-foreground", t["accent-foreground"] ?? t.foreground),
    ...from("sidebar-border", t.border),
    ...from("sidebar-ring", t.ring ?? t.primary),
  };
}

/** What a variable is: a theme must set it, complete() works it out, or style.css has its one value. */
export type Kind = "required" | "derived" | "fixed";

export interface Variable {
  name: string;
  /** The group the editor shows it in. */
  group: string;
  kind: Kind;
  /** A fixed variable's one default value, as style.css has it. */
  value?: string;
}

/**
 * Every colour variable a theme has. The editor (ThemeEditor.vue) shows one
 * field for each, grouped and in this order; parseThemeCSS keeps only these;
 * a saved theme that carries anything else loses it at the next save.
 */
export const colourVariables: Variable[] = [
  { name: "background", group: "Base", kind: "required" },
  { name: "foreground", group: "Base", kind: "required" },
  { name: "muted", group: "Surfaces", kind: "required" },
  { name: "muted-foreground", group: "Surfaces", kind: "required" },
  { name: "card", group: "Surfaces", kind: "derived" },
  { name: "card-foreground", group: "Surfaces", kind: "derived" },
  { name: "popover", group: "Surfaces", kind: "derived" },
  { name: "popover-foreground", group: "Surfaces", kind: "derived" },
  { name: "secondary", group: "Surfaces", kind: "derived" },
  { name: "secondary-foreground", group: "Surfaces", kind: "derived" },
  { name: "primary", group: "Actions", kind: "required" },
  { name: "primary-foreground", group: "Actions", kind: "required" },
  { name: "accent", group: "Actions", kind: "required" },
  { name: "accent-foreground", group: "Actions", kind: "derived" },
  { name: "ring", group: "Actions", kind: "derived" },
  { name: "destructive", group: "Status", kind: "required" },
  { name: "success", group: "Status", kind: "required" },
  { name: "warning", group: "Status", kind: "required" },
  { name: "destructive-foreground", group: "Status", kind: "fixed", value: "#ffffff" },
  { name: "border", group: "Lines", kind: "required" },
  { name: "input", group: "Lines", kind: "derived" },
  { name: "chart-1", group: "Charts", kind: "required" },
  { name: "chart-2", group: "Charts", kind: "required" },
  { name: "chart-3", group: "Charts", kind: "required" },
  { name: "chart-4", group: "Charts", kind: "required" },
  { name: "chart-5", group: "Charts", kind: "required" },
  { name: "sidebar", group: "Sidebar", kind: "required" },
  { name: "sidebar-foreground", group: "Sidebar", kind: "derived" },
  { name: "sidebar-primary", group: "Sidebar", kind: "derived" },
  { name: "sidebar-primary-foreground", group: "Sidebar", kind: "derived" },
  { name: "sidebar-accent", group: "Sidebar", kind: "derived" },
  { name: "sidebar-accent-foreground", group: "Sidebar", kind: "derived" },
  { name: "sidebar-border", group: "Sidebar", kind: "derived" },
  { name: "sidebar-ring", group: "Sidebar", kind: "derived" },
];

const listed = new Set(colourVariables.map((v) => v.name));

const theme = (name: string, light: Tokens, dark: Tokens): Theme => ({ name, light, dark, builtIn: true });

/** The built-in themes. Graphite is the default, and style.css has its light colours. */
export const builtInThemes: Theme[] = [
  theme(
    "Graphite",
    {
      background: "#ffffff", foreground: "#09090b", muted: "#f4f4f5", "muted-foreground": "#71717a", primary: "#18181b",
      "primary-foreground": "#fafafa", accent: "#f4f4f5", "accent-foreground": "#18181b", destructive: "#dc2626", success: "#16a34a",
      warning: "#ca8a04", border: "#e4e4e7", ring: "#a1a1aa", "chart-1": "#3f3f46", "chart-2": "#a1a1aa", "chart-3": "#52525b",
      "chart-4": "#71717a", "chart-5": "#d4d4d8", sidebar: "#fafafa", "sidebar-foreground": "#3f3f46",
    },
    {
      background: "#09090b", foreground: "#fafafa", card: "#111113", popover: "#18181b", muted: "#1c1c1f", "muted-foreground": "#a1a1aa",
      primary: "#fafafa", "primary-foreground": "#18181b", accent: "#232326", "accent-foreground": "#fafafa", destructive: "#ef4444",
      success: "#22c55e", warning: "#eab308", border: "#27272a", ring: "#52525b", "chart-1": "#e4e4e7", "chart-2": "#71717a",
      "chart-3": "#a1a1aa", "chart-4": "#52525b", "chart-5": "#d4d4d8", sidebar: "#0c0c0e", "sidebar-foreground": "#d4d4d8",
    },
  ),
  theme(
    "Harbor",
    {
      background: "#ffffff", foreground: "#141a1f", card: "#ffffff", muted: "#f1f4f5", "muted-foreground": "#5f6b73",
      primary: "#0e5e6f", "primary-foreground": "#f4fbfc", secondary: "#eef3f4", accent: "#e9eff1", "accent-foreground": "#0f2a33",
      destructive: "#c2413a", success: "#2f8a4e", warning: "#b7791f", border: "#e1e7ea", input: "#d5dde1", ring: "#0e5e6f",
      "chart-1": "#0e7d92", "chart-2": "#c8792a", "chart-3": "#5b6fd6", "chart-4": "#4c9a5f", "chart-5": "#b5508a", sidebar: "#f7f9fa",
      "sidebar-foreground": "#28343b",
    },
    {
      background: "#0f1417", foreground: "#e7eef1", card: "#151c20", popover: "#182126", muted: "#1c252a", "muted-foreground": "#8d9ba3",
      primary: "#4fb8cb", "primary-foreground": "#062027", secondary: "#1f2a30", accent: "#1f2a30", "accent-foreground": "#e7eef1",
      destructive: "#ec6a61", success: "#4cb873", warning: "#e0a63c", border: "#253137", input: "#2c3a41", ring: "#4fb8cb",
      "chart-1": "#4fb8cb", "chart-2": "#e3a35c", "chart-3": "#8b9cf0", "chart-4": "#6fc285", "chart-5": "#e07bb4", sidebar: "#0c1013",
      "sidebar-foreground": "#c9d4d9",
    },
  ),
  theme(
    "Tide",
    {
      background: "#fbfcfe", foreground: "#0f172a", card: "#ffffff", muted: "#f1f5f9", "muted-foreground": "#64748b",
      primary: "#2563eb", "primary-foreground": "#f8fafc", accent: "#eaf0fb", "accent-foreground": "#1e3a8a", destructive: "#dc2626",
      success: "#15803d", warning: "#b45309", border: "#e2e8f0", ring: "#2563eb", "chart-1": "#2563eb", "chart-2": "#0d9488",
      "chart-3": "#7c3aed", "chart-4": "#ea580c", "chart-5": "#db2777", sidebar: "#f4f7fc", "sidebar-foreground": "#334155",
    },
    {
      background: "#0b1120", foreground: "#e2e8f0", card: "#0f172a", popover: "#131c31", muted: "#16203a", "muted-foreground": "#94a3b8",
      primary: "#60a5fa", "primary-foreground": "#0b1120", accent: "#1a2540", "accent-foreground": "#e2e8f0", destructive: "#f87171",
      success: "#4ade80", warning: "#fbbf24", border: "#1e293b", ring: "#60a5fa", "chart-1": "#60a5fa", "chart-2": "#2dd4bf",
      "chart-3": "#a78bfa", "chart-4": "#fb923c", "chart-5": "#f472b6", sidebar: "#080d19", "sidebar-foreground": "#cbd5e1",
    },
  ),
  theme(
    "Rosewood",
    {
      background: "#fffcfc", foreground: "#2a1219", card: "#ffffff", muted: "#f8f0f2", "muted-foreground": "#7d6168",
      primary: "#be3a5c", "primary-foreground": "#fff7f9", accent: "#f6e9ed", "accent-foreground": "#5a1a2c", destructive: "#c22f2f",
      success: "#3f8a4a", warning: "#b8761c", border: "#eedfe3", ring: "#be3a5c", "chart-1": "#be3a5c", "chart-2": "#6b7d3a",
      "chart-3": "#8a5cc2", "chart-4": "#d08a3a", "chart-5": "#3a8a9a", sidebar: "#fbf5f6", "sidebar-foreground": "#4a2c34",
    },
    {
      background: "#160c0f", foreground: "#f4e6ea", card: "#1d1115", popover: "#22141a", muted: "#26171c", "muted-foreground": "#b0939b",
      primary: "#ef7f9c", "primary-foreground": "#2a0c15", accent: "#2c1a20", "accent-foreground": "#f4e6ea", destructive: "#f2716b",
      success: "#6fc27c", warning: "#e7a64b", border: "#33212a", ring: "#ef7f9c", "chart-1": "#ef7f9c", "chart-2": "#b5c47a",
      "chart-3": "#b99ae8", "chart-4": "#f0b36b", "chart-5": "#6cc3d1", sidebar: "#12090c", "sidebar-foreground": "#dcc5cb",
    },
  ),
  theme(
    "Nord",
    {
      background: "#eceff4", foreground: "#2e3440", card: "#f6f8fb", muted: "#e1e6ee", "muted-foreground": "#5b6577",
      primary: "#5e81ac", "primary-foreground": "#eceff4", accent: "#dde3ec", "accent-foreground": "#2e3440", destructive: "#bf616a",
      success: "#5f8f4e", warning: "#c08a2e", border: "#d4dbe6", ring: "#5e81ac", "chart-1": "#5e81ac", "chart-2": "#a3be8c",
      "chart-3": "#b48ead", "chart-4": "#d08770", "chart-5": "#88c0d0", sidebar: "#e5e9f0", "sidebar-foreground": "#3b4252",
    },
    {
      background: "#2e3440", foreground: "#eceff4", card: "#343b49", popover: "#3b4252", muted: "#3b4252", "muted-foreground": "#a5afc0",
      primary: "#88c0d0", "primary-foreground": "#2e3440", accent: "#3e4656", "accent-foreground": "#eceff4", destructive: "#bf616a",
      success: "#a3be8c", warning: "#ebcb8b", border: "#434c5e", ring: "#88c0d0", "chart-1": "#88c0d0", "chart-2": "#ebcb8b",
      "chart-3": "#b48ead", "chart-4": "#d08770", "chart-5": "#a3be8c", sidebar: "#292e39", "sidebar-foreground": "#d8dee9",
    },
  ),
];

/**
 * A theme from CSS, as shadcn's theme makers (such as tweakcn) give it: the
 * variables in `:root { ... }` are the light colours, and those in
 * `.dark { ... }` the dark ones. With no .dark block, both are the same.
 */
export function parseThemeCSS(css: string): { light: Tokens; dark: Tokens } {
  const block = (selector: RegExp) => {
    const tokens: Tokens = {};
    const body = css.match(selector)?.[1] ?? "";
    for (const [, name, value] of body.matchAll(/--([\w-]+)\s*:\s*([^;]+);/g)) {
      if (listed.has(name)) {
        tokens[name] = value.trim();
      }
    }
    return tokens;
  };
  const light = block(/:root\s*\{([^}]*)\}/);
  const dark = block(/\.dark\s*\{([^}]*)\}/);
  if (Object.keys(light).length === 0) {
    throw new Error("Paste CSS with a :root { ... } block of variables, such as --primary: #0e5e6f;");
  }
  return { light, dark: Object.keys(dark).length ? dark : light };
}

/** Whether the window is dark, as applyTheme last set it. */
export const dark = ref(false);

/**
 * The theme being edited, before it is saved: while it has a value the window
 * wears it instead of the theme in use (ThemeEditor.vue sets and clears it).
 * Apps read it in applyTheme, and the editor writes it, so it lives here.
 */
export const draft = ref<{ light: Tokens; dark: Tokens } | null>(null);

/**
 * Keeps the window in a theme and mode: the theme's variables on <html>, and
 * .dark on it when it is dark. theme and mode are read again whenever what
 * they read changes, as computed's are. While the editor has a draft, the
 * window wears that instead of the theme in use.
 */
export function applyTheme(theme: () => { light: Tokens; dark: Tokens }, mode: () => Mode) {
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  let systemDark = media.matches;
  let set: string[] = [];
  const apply = () => {
    const root = document.documentElement;
    const m = mode();
    const isDark = m === "dark" || (m === "system" && systemDark);
    dark.value = isDark;
    root.classList.toggle("dark", isDark);
    root.style.colorScheme = isDark ? "dark" : "light";
    for (const name of set) {
      root.style.removeProperty(`--${name}`);
    }
    const shown = draft.value ?? theme();
    const tokens = complete(isDark ? shown.dark : shown.light);
    set = Object.keys(tokens);
    for (const [name, value] of Object.entries(tokens)) {
      root.style.setProperty(`--${name}`, value);
    }
  };
  media.addEventListener("change", (e) => {
    systemDark = e.matches;
    apply();
  });
  watchEffect(apply);
}
