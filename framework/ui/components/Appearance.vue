<script setup lang="ts">
import { computed, ref } from "vue";
import { Pencil, Plus } from "lucide-vue-next";
import { Button } from "./button";
import IconButton from "./IconButton.vue";
import DeleteButton from "./DeleteButton.vue";
import ThemePicker from "./ThemePicker.vue";
import ThemeEditor from "./ThemeEditor.vue";
import type { Mode, Theme } from "../themes";

// The Appearance part of Settings, the same for every app: the switch between
// light, dark and system at the top left, the themes to pick beneath it, an
// "Add a theme" button below the list, and on the right the theme in use — its
// colours — or the editor, while you add or edit one. The window wears the
// editor's draft as you make it (the app's applyTheme reads it).
//
// What the themes are, where they are kept, and what choosing, adding and
// deleting one does are the app's: this only shows them and asks, so the app
// saves what it is told. current is the theme in use; appName names the app to
// the editor when a new name clashes with one of its built-in themes; save
// keeps a theme (given its colours as JSON) and resolves once it is kept, so
// the editor closes on success and keeps what you typed when it fails.
const props = defineProps<{
  themes: Theme[];
  current: Theme;
  mode: Mode;
  appName: string;
  save: (original: string, name: string, tokens: string) => PromiseLike<unknown>;
}>();
const emit = defineEmits<{
  mode: [mode: Mode];
  use: [name: string];
  remove: [name: string];
}>();

const editing = ref<{ original: string; initial: Theme } | null>(null);

function startAdding() {
  editing.value = { original: "", initial: { name: "", light: props.current.light, dark: props.current.dark, builtIn: false } };
}

function startEditing() {
  editing.value = { original: props.current.name, initial: props.current };
}

// The colours a theme sets, light and dark, by name, for the right side.
const colourRows = (variant: "light" | "dark") => {
  void props.mode;
  const tokens = props.current[variant];
  return Object.keys(tokens)
    .sort()
    .map((k) => ({ name: k, colour: tokens[k] }));
};
const variants = computed(() => [
  { key: "light", label: "Light", rows: colourRows("light") },
  { key: "dark", label: "Dark", rows: colourRows("dark") },
]);
</script>

<template>
  <div class="grid grid-cols-[minmax(13rem,16rem)_minmax(0,1fr)] items-start gap-6">
    <div class="flex min-w-0 flex-col gap-4">
      <ThemePicker
        :themes="themes"
        :current="current.name"
        :mode="mode"
        :disabled="editing !== null"
        @mode="(m) => emit('mode', m)"
        @use="(n) => emit('use', n)"
      >
        <template #actions="{ theme: t }">
          <DeleteButton size="icon-xs" :what="`the theme ${t.name}`" @confirm="emit('remove', t.name)" />
        </template>
      </ThemePicker>

      <Button type="button" variant="outline" class="w-full" aria-label="Add a theme" :disabled="editing !== null" @click="startAdding">
        <Plus />Add a theme
      </Button>
    </div>

    <ThemeEditor
      v-if="editing"
      :key="editing.original"
      :themes="themes"
      :original="editing.original"
      :initial="editing.initial"
      :app-name="appName"
      :save="save"
      @close="editing = null"
    />

    <div v-else class="grid min-w-0 content-start gap-6" aria-label="Theme colours">
      <div class="flex min-w-0 items-center gap-1.5">
        <h3 class="min-w-0 truncate font-medium">{{ current.name }}</h3>
        <IconButton v-if="!current.builtIn" label="Edit" size="icon-xs" @click="startEditing"><Pencil /></IconButton>
      </div>
      <section v-for="v in variants" :key="v.key" class="grid gap-2">
        <h4 class="text-small font-medium uppercase tracking-wide text-muted-foreground">{{ v.label }}</h4>
        <ul class="grid gap-y-1">
          <li v-for="row in v.rows" :key="row.name" class="flex min-w-0 items-center gap-2 text-small">
            <span class="size-3.5 shrink-0 rounded-sm border" :style="{ background: row.colour }"></span>
            <span class="text-muted-foreground whitespace-nowrap">{{ row.name }}</span>
            <span class="ml-auto shrink-0 font-mono">{{ row.colour }}</span>
          </li>
        </ul>
      </section>
    </div>
  </div>
</template>
