<!--
description: A switch to turn something on or off: only mine, show done, include archived, autopay. For a widget of your own: a widget in the catalog, or the Widget frame, shows one from its toggle prop.
keywords: toggle, switch, checkbox, on off, only mine, show, hide, include, filter, flag, boolean
example:
  <Toggle :model-value="true" label="Only mine" />
-->
<script setup lang="ts">
// A switch, labelled. v-model is true when it is on.
defineProps<{ label: string }>();
const model = defineModel<boolean>();
</script>

<template>
  <label class="inline-flex cursor-pointer items-center gap-2 select-none">
    <input v-model="model" type="checkbox" role="switch" class="peer sr-only" />
    <span
      class="relative h-5 w-9 shrink-0 rounded-full bg-input transition-colors peer-checked:bg-primary peer-focus-visible:ring-3 peer-focus-visible:ring-ring/50 after:absolute after:left-0.5 after:top-0.5 after:size-4 after:rounded-full after:bg-card after:transition-transform peer-checked:after:translate-x-4"
    ></span>
    {{ label }}
  </label>
</template>
