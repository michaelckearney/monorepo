<!--
description: A few numbers that belong together, side by side with their labels: time off balances, account balances, counts by kind, totals for the month.
keywords: stats, figures, numbers, counts, totals, balances, time off left, leave balance, pto, hours, summary, at a glance, scoreboard, kpis, tiles
example:
  <StatsWidget title="Time off left" :stats="[{ label: 'Vacation', value: 64, note: 'hours' }, { label: 'Sick', value: 24, note: 'hours' }, { label: 'Floating', value: 8, note: 'hours' }]" />
-->
<script setup lang="ts">
import { format as show } from "../data";
import type { Stat } from "../shapes";
import Widget from "./Widget.vue";

// Figures, as Stat says, side by side, as many to a row as fit, in currency
// if any is money.
withDefaults(defineProps<{ stats: Stat[]; currency?: string; empty?: string }>(), { empty: "Nothing to show." });
</script>

<template>
  <Widget>
    <p v-if="stats.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <div v-else class="grid grid-cols-[repeat(auto-fill,minmax(6.5rem,1fr))] gap-x-4 gap-y-3">
      <div v-for="(s, i) in stats" :key="i" class="grid min-w-0 content-start gap-0.5">
        <p class="truncate text-small text-muted-foreground">{{ s.label }}</p>
        <p class="truncate text-heading font-semibold tabular-nums">{{ show(s.value, s.format, currency) }}</p>
        <p v-if="s.note" class="truncate text-caption text-muted-foreground">{{ s.note }}</p>
      </div>
    </div>
  </Widget>
</template>
