<!--
description: A few side-by-side buttons to choose one of: a period (week, month, year), a view, a person. For a widget of your own: a widget in the catalog, or the Widget frame, shows one from its views prop.
keywords: segmented, tabs, switcher, period, range, week month year, view, choose one, radio, toggle group, button group
example:
  <Segmented :model-value="'month'" :options="[{ value: 'week', label: 'Week' }, { value: 'month', label: 'Month' }, { value: 'year', label: 'Year' }]" label="Period" />
-->
<script setup lang="ts">
// A choice of a few options, as buttons: strings, or { value, label }.
// label names it for screen readers. v-model is the chosen value.
defineProps<{ options: (string | { value: string; label: string })[]; label: string }>();
const model = defineModel<string>();
const valueOf = (o: string | { value: string }) => (typeof o === "string" ? o : o.value);
const labelOf = (o: string | { label: string }) => (typeof o === "string" ? o : o.label);
</script>

<template>
  <div class="inline-flex h-control items-center gap-0.5 rounded-md bg-muted p-0.5" role="radiogroup" :aria-label="label">
    <button
      v-for="o in options"
      :key="valueOf(o)"
      type="button"
      role="radio"
      :aria-checked="model === valueOf(o)"
      class="h-full rounded-sm px-2 text-muted-foreground transition-colors hover:text-foreground"
      :class="model === valueOf(o) && 'bg-card font-medium text-foreground'"
      @click="model = valueOf(o)"
    >
      {{ labelOf(o) }}
    </button>
  </div>
</template>
