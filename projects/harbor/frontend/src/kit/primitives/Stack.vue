<!--
description: Puts its children one under another, with an even gap.
keywords: stack, column, vertical, layout, spacing, gap
example:
  <Stack gap="tight"><Text>Dr. Patel</Text><Text muted size="small">Clearwater Pediatrics</Text></Stack>
-->
<script setup lang="ts">
// Children one under another. gap is tight (4px), normal (8px), or loose (16px).
withDefaults(defineProps<{ gap?: "tight" | "normal" | "loose" }>(), { gap: "normal" });
</script>

<template>
  <div class="flex min-w-0 flex-col" :class="{ tight: 'gap-1', normal: 'gap-2', loose: 'gap-4' }[gap]">
    <slot />
  </div>
</template>
