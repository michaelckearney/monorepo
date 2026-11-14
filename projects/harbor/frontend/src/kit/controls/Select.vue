<!--
description: A dropdown to choose one of several things: a project, a person, an account, a period. For a widget of your own: a widget in the catalog, or the Widget frame, shows one from its filters prop.
keywords: select, dropdown, choose, pick, picker, filter, menu, options, project, person, account, choice
example:
  <Select :model-value="'p-sam'" :options="[{ value: 'p-sam', label: 'Sam' }, { value: 'p-jo', label: 'Jo' }]" label="Person" />
-->
<script setup lang="ts">
import { ChevronDown } from "lucide-vue-next";

// A choice of options: strings, or { value, label }. label names it, beside
// it, unless hideLabel. v-model is the chosen value.
withDefaults(
  defineProps<{
    options: (string | { value: string; label: string })[];
    label: string;
    hideLabel?: boolean;
  }>(),
  { hideLabel: true },
);
const model = defineModel<string>();
const valueOf = (o: string | { value: string }) => (typeof o === "string" ? o : o.value);
const labelOf = (o: string | { label: string }) => (typeof o === "string" ? o : o.label);
</script>

<template>
  <label class="inline-flex items-center gap-2">
    <span :class="hideLabel ? 'sr-only' : 'text-muted-foreground'">{{ label }}</span>
    <span class="relative inline-flex">
      <select
        v-model="model"
        class="h-control appearance-none rounded-md border border-input bg-card py-0 pl-3 pr-8 text-body text-foreground outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <option v-for="o in options" :key="valueOf(o)" :value="valueOf(o)">{{ labelOf(o) }}</option>
      </select>
      <ChevronDown class="pointer-events-none absolute right-2 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
    </span>
  </label>
</template>
