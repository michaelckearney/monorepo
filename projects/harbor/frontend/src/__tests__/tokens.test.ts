import { describe, expect, it } from "vitest";
import { builtInThemes, colourVariables } from "../themes";

// Harbor's colours, sizes and radii are its tokens, in style.css and
// themes.ts. Everything else uses them, so the app looks the same
// throughout and in every theme. These tests find what doesn't.
const sources = Object.entries(
  import.meta.glob<string>(["../**/*.{vue,ts}", "!../__tests__/**", "!../themes.ts"], {
    query: "?raw",
    import: "default",
    eager: true,
  }),
);

/** Each match of pattern in the sources, as "file: match". */
function found(pattern: RegExp, text: (source: string) => string = (s) => s) {
  return sources.flatMap(([file, source]) =>
    [...text(source).matchAll(pattern)].map((m) => `${file.replace("../", "")}: ${m[0]}`),
  );
}

const styles = (source: string) => [...source.matchAll(/<style[^>]*>([\s\S]*?)<\/style>/g)].map((m) => m[1]).join("\n");

describe("tokens", () => {
  it("sizes text only from Harbor's scale", () => {
    // text-caption, text-small, text-body, text-title, text-heading, text-figure.
    expect(found(/(?<![\w-])text-(xs|sm|base|lg|[0-9]?xl)(?![\w-])/g)).toEqual([]);
    expect(found(/(?<![\w-])text-\[[0-9.]/g)).toEqual([]);
  });

  it("colours only from the theme", () => {
    const palette =
      "slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose|black|white";
    expect(found(new RegExp(`(?<![\\w-])(bg|text|border|ring|fill|stroke|outline|from|to|divide)-(${palette})(?![\\w-])`, "g"))).toEqual(
      [],
    );
    expect(found(new RegExp(`(?<![\\w-])(bg|text|border|ring|fill|stroke|outline|from|to|divide)-(${palette})-[0-9]+`, "g"))).toEqual(
      [],
    );
    expect(found(/-\[(#|rgb|hsl|oklch)/g)).toEqual([]);
    expect(found(/#[0-9a-fA-F]{3,8}\b|\b(rgba?|hsla?|oklch)\(/g, styles)).toEqual([]);
  });

  it("spaces things only by Harbor's steps", () => {
    // The steps in style.css, or a named one: p-page, p-card, h-control, ...
    const steps = "0|px|0\\.5|1|1\\.5|2|3|4|6|8";
    const utility =
      "-?(?:p|px|py|pt|pb|pl|pr|ps|pe|m|mx|my|mt|mb|ml|mr|ms|me|gap|gap-x|gap-y|space-x|space-y|inset|inset-x|inset-y|top|bottom|left|right|start|end)";
    expect(found(new RegExp(`(?<![\\w-])${utility}-(?!(?:${steps})(?![\\w./-]))[0-9.]+(?![\\w/-])`, "g"))).toEqual([]);
    expect(found(new RegExp(`(?<![\\w-])${utility}-\\[`, "g"))).toEqual([]);
    // Scoped styles write a step as calc(var(--spacing) * 3).
    const allowed = new RegExp(
      `calc\\(var\\(--spacing\\) \\* (?:${steps.replace("px|", "")})\\)|var\\(--spacing-[a-z-]+\\)|(?<![\\w.])0(?![\\w.])|auto`,
      "g",
    );
    const declarations = found(/(?<![\w-])(?:padding|margin|gap|row-gap|column-gap)(?:-[a-z]+)*:[^;]+/g, styles);
    expect(declarations.filter((d) => d.split(":").slice(2).join(":").replace(allowed, "").trim() !== "")).toEqual([]);
  });

  it("draws surfaces with borders, and shadows only under what floats", () => {
    const floating = ["components/SettingsDialog.vue"];
    const shadows = found(/(?<![\w-])shadow(?:-[a-z0-9]+)?(?![\w-])|(?<![\w-])box-shadow:/g).filter(
      (f) => !floating.some((file) => f.startsWith(`${file}:`)),
    );
    expect(shadows).toEqual([]);
  });

  it("gives scoped styles Harbor's sizes and radii", () => {
    expect(found(/font-size:(?!\s*var\(--text-)[^;]+/g, styles)).toEqual([]);
    expect(found(/font-family:(?!\s*var\(--font-)[^;]+/g, styles)).toEqual([]);
    expect(found(/border-radius:(?!\s*(var\(--radius|50%))[^;]+/g, styles)).toEqual([]);
  });
});

// The shared style.css, framework/ui's, as text: ?raw gives nothing for CSS
// under vitest, so read it as it is, where desktop copies it.
const here = (import.meta as unknown as { filename: string }).filename;
const styleCss = (await import("node:fs" as string)).readFileSync(
  here.slice(0, here.lastIndexOf("/frontend/src/__tests__/")) + "/.desktop/ui/style.css",
  "utf8",
) as string;

describe("the colour variables", () => {
  const root = (styleCss.match(/:root\s*\{([^}]*)\}/) ?? ["", ""])[1];

  it("are the colours style.css declares", () => {
    const declared = [...root.matchAll(/--([\w-]+)\s*:/g)]
      .map((m) => m[1])
      .filter((n) => !["overlay", "radius", "font-sans", "font-mono"].includes(n))
      .sort();
    expect(declared).toEqual(colourVariables.map((v) => v.name).sort());
  });

  it("keeps a fixed variable's one value the one in style.css", () => {
    for (const v of colourVariables) {
      if (v.kind === "fixed") expect(root).toContain(`--${v.name}: ${v.value};`);
    }
  });

  it("has every built-in theme set each required variable in both modes", () => {
    for (const t of builtInThemes) {
      for (const mode of ["light", "dark"] as const) {
        for (const v of colourVariables) {
          if (v.kind === "required") expect(t[mode][v.name], `${t.name} ${mode} ${v.name}`).toBeTruthy();
        }
      }
    }
  });
});
