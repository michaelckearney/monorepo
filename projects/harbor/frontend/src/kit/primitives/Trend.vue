<!--
description: How much a number changed from the one before, as "▲ 4.2%" in green or red.
keywords: trend, change, delta, difference, up, down, increase, decrease, compared, previous, growth
example:
  <Row><Trend :value="2140" :previous="2050" /><Trend :value="96" :previous="120" lower-is-better /></Row>
-->
<script setup lang="ts">
import { computed } from "vue";
import { format as show, type Format } from "../data";
import { toneText } from "../tone";

// The change from previous to value, as a percent, or as an amount when
// format is set ("▲ $90.00"). Up is good, unless lowerIsBetter.
const props = defineProps<{ value: number; previous: number; format?: Format; currency?: string; lowerIsBetter?: boolean }>();

const change = computed(() => Number(props.value) - Number(props.previous));
const text = computed(() => {
  if (!Number(props.previous) && !props.format) {
    return "";
  }
  const amount = props.format
    ? show(Math.abs(change.value), props.format, props.currency)
    : `${Math.abs((change.value / Math.abs(Number(props.previous))) * 100).toFixed(1).replace(/\.0$/, "")}%`;
  return `${change.value > 0 ? "▲" : change.value < 0 ? "▼" : "■"} ${amount}`;
});
const good = computed(() => (props.lowerIsBetter ? change.value < 0 : change.value > 0));
</script>

<template>
  <span
    v-if="text"
    class="whitespace-nowrap text-small font-medium tabular-nums"
    :class="change === 0 ? 'text-muted-foreground' : good ? toneText.good : toneText.bad"
    >{{ text }}</span
  >
</template>
