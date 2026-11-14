<script setup lang="ts">
import { Check } from "lucide-vue-next";
import { complete, dark, type Mode, type Theme } from "../themes";

// Whether the window is light or dark, and the themes, to pick one: each
// with its colours in the mode the window is in. disabled stops picking, as
// while a theme is being edited. The actions slot is what the row of a
// theme you added has at its end, such as a button to delete it.
defineProps<{ themes: Theme[]; current: string; mode: Mode; disabled?: boolean }>();
const emit = defineEmits<{ mode: [mode: Mode]; use: [name: string] }>();
defineSlots<{ actions?(props: { theme: Theme }): unknown }>();

const modes: { value: Mode; label: string }[] = [
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
  { value: "system", label: "Match the system" },
];

const swatches = (t: Theme) => {
  const c = complete(dark.value ? t.dark : t.light);
  return [c.background, c.primary, c["chart-1"] ?? c.primary, c["chart-2"] ?? c.accent, c.foreground];
};
</script>

<template>
  <div class="inline-flex h-control w-full items-center gap-0.5 rounded-md bg-muted p-0.5" role="radiogroup" aria-label="Mode">
    <button
      v-for="m in modes"
      :key="m.value"
      type="button"
      role="radio"
      :aria-checked="mode === m.value"
      class="h-full flex-1 whitespace-nowrap rounded-sm px-2 text-small text-muted-foreground transition-colors hover:text-foreground"
      :class="mode === m.value && 'bg-card font-medium text-foreground'"
      @click="emit('mode', m.value)"
    >
      {{ m.label }}
    </button>
  </div>

  <fieldset class="contents" :disabled="disabled">
    <ul class="grid gap-2" aria-label="Themes">
      <li v-for="t in themes" :key="t.name" class="relative">
        <button
          type="button"
          class="flex w-full items-center gap-3 rounded-lg border bg-card px-3 py-2 text-left hover:border-ring"
          :class="[{ 'ring-2 ring-ring': current === t.name }, !t.builtIn && 'pr-8']"
          :aria-pressed="current === t.name"
          @click="emit('use', t.name)"
        >
          <span class="flex h-5 w-20 shrink-0 overflow-hidden rounded-md border">
            <span v-for="(c, i) in swatches(t)" :key="i" class="h-full flex-1" :style="{ background: c }"></span>
          </span>
          <span class="flex min-w-0 items-center gap-1.5 font-medium">
            <Check v-if="current === t.name" class="size-3.5 shrink-0 text-primary" /><span class="truncate">{{ t.name }}</span>
          </span>
        </button>
        <span v-if="$slots.actions && !t.builtIn" class="absolute right-1.5 top-1/2 -translate-y-1/2"><slot name="actions" :theme="t" /></span>
      </li>
    </ul>
  </fieldset>
</template>
