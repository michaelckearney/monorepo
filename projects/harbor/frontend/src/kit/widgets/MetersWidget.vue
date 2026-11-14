<!--
description: How full each of several things is, as a bar with its percent: disks and storage, quotas, data plans, budgets by category, goals. Amber near full, red when nearly out.
keywords: meters, usage, storage, disk space, disks, drives, capacity, quota, data plan, budget, budgets, spending limits, limits, goals, progress, utilization, remaining, how full, free space
example:
  <MetersWidget title="Disks" :gauges="[{ label: 'Photos', value: 1610, max: 2000 }, { label: 'Backups', value: 380, max: 1000 }]" unit="GB" />
-->
<script setup lang="ts">
import type { Gauge } from "../shapes";
import Item from "../primitives/Item.vue";
import Meter from "../primitives/Meter.vue";
import Widget from "./Widget.vue";

// How full things are, as Gauge says. unit follows the numbers in "1,610 of
// 2,000 GB used".
const props = withDefaults(defineProps<{ gauges: Gauge[]; unit?: string; empty?: string }>(), { empty: "Nothing to show." });

const pct = (g: Gauge) => (g.percent ?? ((g.value ?? NaN) / (g.max ?? NaN)) * 100) || 0;
const n = (v: number) => new Intl.NumberFormat().format(v);
const detailOf = (g: Gauge) =>
  g.detail ?? (g.value !== undefined && g.max !== undefined ? `${n(g.value)} of ${n(g.max)}${props.unit ? ` ${props.unit}` : ""} used` : "");
</script>

<template>
  <Widget>
    <p v-if="gauges.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <ul v-else class="grid">
      <li v-for="(g, i) in gauges" :key="i" class="border-b last:border-0">
        <Item :label="g.label" :detail="detailOf(g)">
          <template #trailing>
            <span class="font-medium tabular-nums">{{ Math.round(pct(g)) }}%</span>
          </template>
          <Meter class="mt-1" :value="pct(g)" />
        </Item>
      </li>
    </ul>
  </Widget>
</template>
