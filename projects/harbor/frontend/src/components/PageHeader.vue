<script setup lang="ts">
// The top of every page, always one line: its name, which gives way first,
// and what you can do there.
defineProps<{ title: string; note?: string }>();
</script>

<template>
  <header class="flex h-14 shrink-0 items-center gap-3 border-b px-page">
    <div class="flex min-w-0 flex-1 items-baseline gap-2">
      <span v-if="$slots.icon" class="flex shrink-0 self-center"><slot name="icon" /></span>
      <h1 class="min-w-0 truncate text-title font-semibold tracking-tight">{{ title }}</h1>
      <span v-if="note" class="min-w-0 truncate text-small text-muted-foreground">{{ note }}</span>
      <slot name="title" />
    </div>
    <div class="flex shrink-0 items-center gap-1.5"><slot /></div>
  </header>
</template>
