<!--
description: A bar for how full something is, which turns amber and then red as it nears full.
keywords: meter, bar, progress, gauge, usage, capacity, quota, full, percent, level, budget
example:
  <Stack><Meter :value="42" /><Meter :value="85" /><Meter :value="380" :max="400" /></Stack>
-->
<script setup lang="ts">
import { computed } from "vue";
import { toneFill, type Tone } from "../tone";

// value out of max (100 unless set). It is amber from warn percent, and red
// from bad percent; tone sets its colour instead.
const props = withDefaults(defineProps<{ value: number; max?: number; warn?: number; bad?: number; tone?: Tone }>(), {
  max: 100,
  warn: 80,
  bad: 90,
});

const percent = computed(() => Math.max(0, Math.min(100, (Number(props.value) / props.max) * 100 || 0)));
const t = computed<Tone>(() => props.tone ?? (percent.value >= props.bad ? "bad" : percent.value >= props.warn ? "warning" : "info"));
</script>

<template>
  <div
    class="h-1.5 overflow-hidden rounded-full bg-muted"
    role="meter"
    :aria-valuenow="value"
    aria-valuemin="0"
    :aria-valuemax="max"
  >
    <div class="h-full rounded-full" :class="toneFill[t]" :style="{ width: `${percent}%` }"></div>
  </div>
</template>
