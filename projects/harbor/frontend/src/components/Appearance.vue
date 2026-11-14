<script setup lang="ts">
import { computed, ref } from "vue";
import { Pencil, Plus } from "lucide-vue-next";
import { Preferences } from "@backend";
import { Button } from "@ui/components/button";
import ThemePicker from "@ui/components/ThemePicker.vue";
import { useThemes, type Theme } from "../themes";
import DeleteButton from "./DeleteButton.vue";
import IconButton from "@ui/components/IconButton.vue";
import SettingsPane from "@ui/components/SettingsPane.vue";
import ThemeEditor from "./ThemeEditor.vue";

// The theme, and whether the window is light or dark. The themes are a list
// on the left; the right shows the one in use — its colours — or the editor,
// while you add or edit a theme. The window wears the editor's draft as you
// make it.
const { themes, current, mode } = useThemes();

const editing = ref<{ original: string; initial: Theme } | null>(null);

function startAdding() {
  editing.value = { original: "", initial: { name: "", light: current.value.light, dark: current.value.dark, builtIn: false } };
}

function startEditing() {
  editing.value = { original: current.value.name, initial: current.value };
}

// The colours a theme sets, light and dark, by name, for the right side.
const colourRows = (variant: "light" | "dark") => {
  void mode.value;
  const tokens = current.value[variant];
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
  <SettingsPane title="Appearance">
    <div class="grid grid-cols-[minmax(13rem,16rem)_minmax(0,1fr)] items-start gap-6">
      <div class="flex min-w-0 flex-col gap-4">
        <ThemePicker
          :themes="themes"
          :current="current.name"
          :mode="mode"
          :disabled="editing !== null"
          @mode="Preferences.SetMode"
          @use="Preferences.UseTheme"
        >
          <template #actions="{ theme: t }">
            <DeleteButton size="icon-xs" :what="`the theme ${t.name}`" @confirm="Preferences.DeleteTheme(t.name)" />
          </template>
        </ThemePicker>

        <Button type="button" variant="outline" class="w-full" aria-label="Add a theme" :disabled="editing !== null" @click="startAdding">
          <Plus />Add a theme
        </Button>
      </div>

      <ThemeEditor v-if="editing" :key="editing.original" :original="editing.original" :initial="editing.initial" @close="editing = null" />

      <div v-else class="grid min-w-0 content-start gap-6" aria-label="Theme colours">
        <div class="flex min-w-0 items-center gap-1.5">
          <h3 class="min-w-0 truncate font-medium">{{ current.name }}</h3>
          <IconButton v-if="!current.builtIn" label="Edit" size="icon-xs" @click="startEditing"><Pencil /></IconButton>
        </div>
        <section v-for="v in variants" :key="v.key" class="grid gap-2">
          <h4 class="text-small font-medium uppercase tracking-wide text-muted-foreground">{{ v.label }}</h4>
          <ul class="grid gap-x-6 gap-y-1 sm:grid-cols-2">
            <li v-for="row in v.rows" :key="row.name" class="flex min-w-0 items-center gap-2 text-small">
              <span class="size-3.5 shrink-0 rounded-sm border" :style="{ background: row.colour }"></span>
              <span class="min-w-0 truncate text-muted-foreground">{{ row.name }}</span>
              <span class="ml-auto shrink-0 font-mono">{{ row.colour }}</span>
            </li>
          </ul>
        </section>
      </div>
    </div>
  </SettingsPane>
</template>
