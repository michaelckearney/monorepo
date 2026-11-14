<script setup lang="ts">
import { inject } from "vue";
import { ArrowLeft, X } from "lucide-vue-next";
import { closeSettingsKey } from "../settings";
import IconButton from "./IconButton.vue";

// A part of Settings, or a form within it: a header with its name and what
// you can do there, then its content, which scrolls. A form has a back
// button, to the part it is in.
defineProps<{ title: string; back?: boolean }>();
const emit = defineEmits<{ back: [] }>();
const closeSettings = inject(closeSettingsKey, () => {});
</script>

<template>
  <section :aria-label="title" class="flex min-h-0 flex-1 flex-col">
    <header class="flex h-14 shrink-0 items-center gap-1.5 border-b px-page">
      <IconButton v-if="back" label="Back" class="-ml-2" @click="emit('back')"><ArrowLeft /></IconButton>
      <div class="flex min-w-0 flex-1 items-center gap-2">
        <h2 class="min-w-0 truncate text-title font-semibold">{{ title }}</h2>
        <slot name="title" />
      </div>
      <slot name="actions" />
      <IconButton label="Close" class="-mr-2" @click="closeSettings()"><X /></IconButton>
    </header>
    <div class="min-h-0 flex-1 overflow-y-auto">
      <div class="grid gap-6 p-page">
        <slot />
      </div>
    </div>
  </section>
</template>
