<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watchEffect } from "vue";
import { ChevronDown, X } from "lucide-vue-next";
import { Button } from "./button";
import { Input } from "./input";
import { Popover, PopoverContent, PopoverTrigger } from "./popover";
import { Textarea } from "./textarea";
import IconButton from "./IconButton.vue";
import { builtInThemes, colourVariables, complete, draft, parseThemeCSS, type Theme, type Tokens, type Variable } from "../themes";

// The theme editor: one field for each colour variable, in its group, for
// each mode. You start from a theme (Start from, or Import CSS for one from
// a theme maker such as tweakcn), and the window wears what you have typed
// until you save or cancel. The values you leave empty are calculated
// (complete) or fixed; their fields show the colour they will have.
//
// The themes are the app's (built-in and its own), for "Start from" and for
// checking a new name against the app's built-ins by name (appName). Saving is
// the app's: it passes a save function that stores the theme's colours (given
// as JSON) and resolves when they are kept. The editor calls it and, only on
// success, closes itself, so a rejected save leaves what you typed.
const props = defineProps<{
  /** Every theme the app has, built-in and its own. */
  themes: Theme[];
  /** The saved theme this editor changes, or "" when adding one. */
  original: string;
  /** The theme the fields start with. */
  initial: Theme;
  /** The app's name, for a clash with one of its built-in themes. */
  appName: string;
  /** Keeps the theme, given its colours as JSON; resolves once it is kept. */
  save: (original: string, name: string, tokens: string) => Promise<unknown>;
}>();
const emit = defineEmits<{ close: [] }>();

const name = ref(props.initial.name);
const modes = [
  { key: "light", label: "Light" },
  { key: "dark", label: "Dark" },
] as const;
type ModeKey = (typeof modes)[number]["key"];

const fields = (tokens: Tokens): Tokens => Object.fromEntries(colourVariables.map((v) => [v.name, tokens[v.name] ?? ""]));
const values = ref<{ light: Tokens; dark: Tokens }>({ light: fields(props.initial.light), dark: fields(props.initial.dark) });

const problem = ref("");
const importing = ref(false);
const css = ref("");
const cssProblem = ref("");

/** Only the variables you have set, in the list's order. */
const set = (tokens: Tokens): Tokens =>
  Object.fromEntries(colourVariables.flatMap((v) => (tokens[v.name] ? [[v.name, tokens[v.name]]] : [])));

// The window wears the draft while the editor is open, and not after.
watchEffect(() => {
  draft.value = { light: set(values.value.light), dark: set(values.value.dark) };
});
onBeforeUnmount(() => {
  draft.value = null;
});

const groups = computed(() => {
  const byGroup: { name: string; variables: Variable[] }[] = [];
  for (const v of colourVariables) {
    const last = byGroup[byGroup.length - 1];
    if (last?.name === v.group) last.variables.push(v);
    else byGroup.push({ name: v.group, variables: [v] });
  }
  return byGroup;
});

// What the field shows: your value, or the one the theme will calculate.
// Only what you have set goes into complete(), so a variable it leaves
// unset of "" does not stop it calculating from the next one.
const shown = (v: Variable, mode: ModeKey) =>
  values.value[mode][v.name] || (v.kind === "fixed" ? v.value : complete(set(values.value[mode]))[v.name]);

const picker = ref<HTMLInputElement>();
let picking: { name: string; mode: ModeKey } | null = null;

// <input type="color"> takes only #rrggbb: paint the value on a 1×1 canvas
// and read the pixel back to get one for it.
function toHex(value: string): string {
  const ctx = document.createElement("canvas").getContext("2d")!;
  ctx.fillStyle = value;
  ctx.fillRect(0, 0, 1, 1);
  const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
  return `#${[r, g, b].map((n) => n.toString(16).padStart(2, "0")).join("")}`;
}

function openPicker(v: Variable, mode: ModeKey) {
  picking = { name: v.name, mode };
  picker.value!.value = toHex(shown(v, mode) ?? "");
  picker.value!.click();
}

function picked(e: Event) {
  if (picking) values.value[picking.mode][picking.name] = (e.target as HTMLInputElement).value.toLowerCase();
}

function startFrom(themeName: string) {
  const t = props.themes.find((t) => t.name === themeName);
  if (t) values.value = { light: fields(t.light), dark: fields(t.dark) };
}

// The CSS sets the fields it has, and clears all the others.
function importCSS() {
  cssProblem.value = "";
  try {
    const t = parseThemeCSS(css.value);
    values.value = { light: fields(t.light), dark: fields(t.dark) };
    importing.value = false;
    css.value = "";
  } catch (e) {
    cssProblem.value = e instanceof Error ? e.message : String(e);
  }
}

function check(): string {
  if (!name.value.trim()) return "A theme needs a name.";
  if (builtInThemes.some((t) => t.name === name.value)) return `${props.appName} has a theme with this name.`;
  if (props.themes.some((t) => !t.builtIn && t.name === name.value && t.name !== props.original)) return "A theme with this name exists.";
  for (const v of colourVariables) {
    if (v.kind !== "required") continue;
    for (const m of modes) if (!values.value[m.key][v.name]) return `Set ${v.name} in ${m.label}.`;
  }
  for (const v of colourVariables) {
    for (const m of modes) {
      const value = values.value[m.key][v.name];
      if (value && !CSS.supports("color", value)) return `${v.name} in ${m.label} is not a colour.`;
    }
  }
  return "";
}

async function save() {
  problem.value = check();
  if (problem.value) return;
  await props.save(props.original, name.value, JSON.stringify({ light: set(values.value.light), dark: set(values.value.dark) }));
  emit("close");
}
</script>

<template>
  <form class="grid min-w-0 content-start gap-6" :aria-label="original ? 'Edit a theme' : 'Add a theme'" @submit.prevent="save">
    <div class="grid grid-cols-[minmax(0,1fr)_auto_auto] items-end gap-2">
      <label class="grid gap-1.5">
        <span class="font-medium">Name</span>
        <Input v-model="name" aria-label="Theme name" />
      </label>
      <label class="grid gap-1.5">
        <span class="font-medium">Start from</span>
        <span class="relative inline-flex">
          <select
            class="h-control appearance-none rounded-md border border-input bg-card py-0 pl-3 pr-8 text-body text-foreground outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
            aria-label="Start from"
            @change="startFrom(($event.target as HTMLSelectElement).value)"
          >
            <option v-for="t in themes" :key="t.name" :value="t.name">{{ t.name }}</option>
          </select>
          <ChevronDown class="pointer-events-none absolute right-2 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        </span>
      </label>
      <Popover v-model:open="importing">
        <PopoverTrigger as-child>
          <Button type="button" variant="outline">Import CSS</Button>
        </PopoverTrigger>
        <PopoverContent class="grid w-96 gap-2">
          <Textarea
            v-model="css"
            rows="8"
            class="font-mono text-small"
            aria-label="Theme CSS"
            placeholder=":root { --primary: #0e5e6f; … }&#10;.dark { --primary: #5fb3c4; … }"
          />
          <p v-if="cssProblem" class="text-destructive" role="alert">{{ cssProblem }}</p>
          <Button type="button" size="sm" @click="importCSS">Import</Button>
        </PopoverContent>
      </Popover>
    </div>

    <section v-for="g in groups" :key="g.name" class="grid gap-2">
      <h3 class="font-medium">{{ g.name }}</h3>
      <div class="grid grid-cols-[minmax(0,9rem)_minmax(0,1fr)_minmax(0,1fr)] gap-x-3">
        <span />
        <span v-for="m in modes" :key="m.key" class="text-small font-medium uppercase tracking-wide text-muted-foreground">{{ m.label }}</span>
      </div>
      <ul class="grid gap-y-1.5">
        <li v-for="v in g.variables" :key="v.name" class="grid grid-cols-[minmax(0,9rem)_minmax(0,1fr)_minmax(0,1fr)] items-center gap-x-3">
          <span class="truncate text-small text-muted-foreground">{{ v.name }}</span>
          <span v-for="m in modes" :key="m.key" class="flex min-w-0 items-center gap-1.5">
            <button
              type="button"
              class="size-3.5 shrink-0 rounded-sm border"
              :style="{ background: shown(v, m.key) }"
              :aria-label="`Choose the ${v.name} colour (${m.label})`"
              @click="openPicker(v, m.key)"
            ></button>
            <Input
              v-model="values[m.key][v.name]"
              :placeholder="v.kind === 'required' ? undefined : shown(v, m.key)"
              :aria-label="`${v.name} (${m.label})`"
              class="min-w-0 font-mono text-small"
            />
            <IconButton v-if="v.kind !== 'required' && values[m.key][v.name]" label="Clear" size="icon-xs" @click="values[m.key][v.name] = ''">
              <X />
            </IconButton>
          </span>
        </li>
      </ul>
    </section>

    <p v-if="problem" class="text-destructive" role="alert">{{ problem }}</p>

    <span class="flex items-center gap-2">
      <Button type="submit" size="sm">Save and use</Button>
      <Button type="button" size="sm" variant="ghost" @click="emit('close')">Cancel</Button>
    </span>

    <input ref="picker" type="color" class="sr-only" tabindex="-1" aria-hidden="true" @input="picked" />
  </form>
</template>
