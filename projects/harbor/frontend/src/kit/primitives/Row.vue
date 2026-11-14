<!--
description: Puts its children side by side, with an even gap, lined up and spread as you say.
keywords: row, horizontal, inline, side by side, layout, spacing, justify, align
example:
  <Row justify="between"><Text>Checking</Text><Value :value="1840.12" format="money" /></Row>
-->
<script setup lang="ts">
// Children side by side. justify spreads them: start, center, end, or
// between (first at the start, last at the end). align lines them up across:
// center, start, end, or baseline. wrap lets them run onto more lines.
withDefaults(
  defineProps<{
    gap?: "tight" | "normal" | "loose";
    justify?: "start" | "center" | "end" | "between";
    align?: "center" | "start" | "end" | "baseline";
    wrap?: boolean;
  }>(),
  { gap: "normal", justify: "start", align: "center" },
);
</script>

<template>
  <div
    class="flex min-w-0"
    :class="[
      { tight: 'gap-1', normal: 'gap-2', loose: 'gap-4' }[gap],
      { start: 'justify-start', center: 'justify-center', end: 'justify-end', between: 'justify-between' }[justify],
      { center: 'items-center', start: 'items-start', end: 'items-end', baseline: 'items-baseline' }[align],
      wrap && 'flex-wrap',
    ]"
  >
    <slot />
  </div>
</template>
