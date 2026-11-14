<!--
description: A bar, line, or area chart: usage or spending over time, by month or by week, values by category, one series or several, with round numbers on the axis.
keywords: chart, graph, plot, bar chart, line chart, area chart, over time, trend, history, by month, monthly, weekly, by day, daily, usage, electricity, energy, spending, compare, series, visualization
example:
  <ChartWidget title="Electricity, kWh a month" :span="6" :series="[{ name: 'kWh', points: [{ x: '2026-06', y: 410 }, { x: '2026-07', y: 520 }, { x: '2026-08', y: 498 }, { x: '2026-09', y: 380 }] }]" x-format="month" />
example:
  <ChartWidget title="Spending by week" :span="6" :series="[{ name: 'Food', points: [{ x: '2026-10-05', y: 120 }, { x: '2026-10-12', y: 140 }, { x: '2026-10-19', y: 98 }] }, { name: 'Fuel', points: [{ x: '2026-10-05', y: 60 }, { x: '2026-10-12', y: 45 }, { x: '2026-10-19', y: 70 }] }]" x-format="date" kind="line" format="money" :views="['By week', 'By month']" view="By week" />
-->
<script setup lang="ts">
import type { Format } from "../data";
import type { Series } from "../shapes";
import Chart from "../primitives/Chart.vue";
import Widget from "./Widget.vue";

// A chart of series, as Series says. xFormat is how the labels along the
// bottom are shown ("month", "date"); format how values are, in currency
// if they are money.
defineProps<{
  series: Series[];
  kind?: "bar" | "line" | "area";
  xFormat?: Format;
  format?: Format;
  currency?: string;
  empty?: string;
}>();
</script>

<template>
  <Widget>
    <Chart :series="series" :kind="kind" :x-format="xFormat" :format="format" :currency="currency" :empty="empty" :title="$attrs.title as string | undefined" />
  </Widget>
</template>
