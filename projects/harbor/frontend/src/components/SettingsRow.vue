<script setup lang="ts">
// A row in a SettingsList, laid out the same in every part of Settings: what
// it is, then a detail, which gives way first, then its status, then what you
// can do with it, as icon buttons.
defineProps<{ title: string; detail?: string }>();
</script>

<template>
  <li class="flex min-h-row items-center gap-3 px-3 py-1.5">
    <slot name="icon" />
    <span class="flex min-w-0 flex-1 items-baseline gap-2">
      <span class="title shrink-0 font-medium">{{ title }}</span>
      <span v-if="detail" class="detail min-w-0 truncate text-muted-foreground" :title="detail">{{ detail }}</span>
    </span>
    <slot name="status" />
    <span v-if="$slots.default" class="flex shrink-0 items-center gap-1"><slot /></span>
  </li>
</template>
