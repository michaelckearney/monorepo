<script setup lang="ts">
import { provide, type Component } from "vue";
import { DialogContent, DialogOverlay, DialogRoot, DialogTitle } from "reka-ui";
import { closeSettingsKey } from "../settings";

// Settings, over the window: a nav of its parts, and the part you chose, from
// the slot named after it. open is that part, or null when Settings is
// closed. hidden keeps it closed while something else must be on top, and
// it comes back as it was after. A SettingsPane in it closes it.
const props = defineProps<{
  sections: { value: string; label: string; icon: Component; alert?: string }[];
  open: string | null;
  hidden?: boolean;
}>();
const emit = defineEmits<{ "update:open": [section: string | null] }>();
provide(closeSettingsKey, () => emit("update:open", null));
</script>

<template>
  <DialogRoot :open="props.open !== null && !hidden" @update:open="(o) => !o && emit('update:open', null)">
    <DialogOverlay class="fixed inset-0 z-[5] bg-overlay" />
    <DialogContent
      :aria-describedby="undefined"
      class="fixed left-1/2 top-1/2 z-[5] flex h-[min(40rem,calc(100vh-2rem))] w-[min(56rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 overflow-hidden rounded-xl border bg-background shadow-lg outline-none max-sm:inset-0 max-sm:h-full max-sm:w-full max-sm:translate-x-0 max-sm:translate-y-0 max-sm:rounded-none"
    >
      <nav aria-label="Settings" class="flex w-48 shrink-0 flex-col gap-0.5 border-r bg-sidebar p-2">
        <DialogTitle class="px-2 pb-2 pt-1.5 text-title font-semibold">Settings</DialogTitle>
        <button
          v-for="s in sections"
          :key="s.value"
          type="button"
          class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sidebar-foreground hover:bg-sidebar-accent"
          :class="props.open === s.value && 'bg-sidebar-accent font-medium text-sidebar-accent-foreground'"
          :aria-current="props.open === s.value ? 'page' : undefined"
          @click="emit('update:open', s.value)"
        >
          <component :is="s.icon" class="size-4 text-muted-foreground" />{{ s.label }}
          <template v-if="s.alert">
            <span class="ml-auto size-1.5 shrink-0 rounded-full bg-warning" :title="s.alert"></span>
            <span class="sr-only">({{ s.alert }})</span>
          </template>
        </button>
      </nav>
      <div class="flex min-w-0 flex-1 flex-col">
        <slot v-if="props.open" :name="props.open" />
      </div>
    </DialogContent>
  </DialogRoot>
</template>
