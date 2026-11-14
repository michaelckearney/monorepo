<!--
description: One row of a list: something on the left (an avatar, an image, a time), a label and a detail under it, and something on the right (a value, a badge). Every list in the kit is made of these.
keywords: item, row, list item, line, entry, record, cell
example:
  <Item label="Dr. Anika Patel" detail="Your results are ready to view" link="https://example.com">
    <template #leading><Avatar name="Dr. Anika Patel" /></template>
    <template #trailing><Time value="2026-11-01" as="relative" /></template>
  </Item>
-->
<script setup lang="ts">
// A row. label is the main line, detail the quieter line under it; link
// makes the label open a page; unread makes it bold, with a dot. The
// leading slot goes on the left, trailing on the right, and the default
// slot under the detail, such as a Meter.
defineProps<{ label?: string; detail?: string; link?: string; unread?: boolean }>();
</script>

<template>
  <div class="flex min-w-0 items-center gap-3 py-2">
    <slot name="leading" />
    <div class="grid min-w-0 flex-1 gap-0.5">
      <div v-if="label || $slots.label" class="flex min-w-0 items-center gap-1.5">
        <span v-if="unread" class="size-1.5 shrink-0 rounded-full bg-primary" aria-label="Unread"></span>
        <slot name="label">
          <a
            v-if="link"
            :href="link"
            class="truncate hover:underline"
            :class="unread && 'font-semibold'"
            >{{ label }}</a
          >
          <span v-else class="truncate" :class="unread && 'font-semibold'">{{ label }}</span>
        </slot>
      </div>
      <div v-if="detail || $slots.detail" class="truncate text-small text-muted-foreground">
        <slot name="detail">{{ detail }}</slot>
      </div>
      <slot />
    </div>
    <div v-if="$slots.trailing" class="flex shrink-0 items-center gap-2"><slot name="trailing" /></div>
  </div>
</template>
