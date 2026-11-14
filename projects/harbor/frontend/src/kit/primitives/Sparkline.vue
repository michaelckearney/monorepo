<!--
description: A small line of a series of numbers, to show the shape of a trend without axes.
keywords: sparkline, trend, line, history, series, mini chart, over time
example:
  <Sparkline :values="[3, 5, 4, 6, 8, 7, 9]" />
-->
<script setup lang="ts">
import { computed } from "vue";

// values as a line, filling the width, height pixels tall. color is the
// nth chart colour, 1 to 5.
const props = withDefaults(defineProps<{ values: number[]; height?: number; color?: number }>(), { height: 28, color: 1 });

const points = computed(() => {
  const v = props.values.map(Number).filter((n) => !Number.isNaN(n));
  if (v.length < 2) {
    return "";
  }
  const lo = Math.min(...v);
  const hi = Math.max(...v);
  return v.map((n, i) => `${(i / (v.length - 1)) * 100},${hi === lo ? 50 : 96 - ((n - lo) / (hi - lo)) * 92}`).join(" ");
});
</script>

<template>
  <svg v-if="points" viewBox="0 0 100 100" preserveAspectRatio="none" class="block w-full" :style="{ height: `${height}px` }" aria-hidden="true">
    <polyline
      :points="points"
      fill="none"
      :stroke="`var(--chart-${color})`"
      stroke-width="2"
      vector-effect="non-scaling-stroke"
      stroke-linejoin="round"
    />
  </svg>
</template>
