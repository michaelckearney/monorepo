<!--
description: One number, large: a balance, a bill, the latest pay, a count, a reading. With how it changed from the one before, a small line of its history, and a note such as a due date.
keywords: metric, number, figure, kpi, stat, big number, balance, bill, amount due, latest pay, paycheck, net pay, total, count, score, reading, headline, single value
example:
  <MetricWidget title="Latest pay" :span="3" :rows="2" :value="2140.5" :previous="2050" format="money" note="Paid Oct 31" />
example:
  <MetricWidget title="Electricity bill" :span="3" :rows="2" :value="128.4" format="money" note="Due Nov 15" toggle="Autopay" :toggled="true" />
-->
<script setup lang="ts">
import { format as show, type Format } from "../data";
import Sparkline from "../primitives/Sparkline.vue";
import Trend from "../primitives/Trend.vue";
import Widget from "./Widget.vue";

// One number or word, large, as format says, in currency if it is money.
// previous adds how it changed (up is good, unless lowerIsBetter); series a
// line of its history. note is a line under it. Give it :rows="2": it is
// short.
defineProps<{
  value: number | string | null;
  format?: Format;
  currency?: string;
  previous?: number;
  series?: number[];
  lowerIsBetter?: boolean;
  note?: string;
}>();
</script>

<template>
  <Widget>
    <div class="grid gap-1">
      <div class="flex min-w-0 items-baseline gap-2">
        <p class="truncate text-figure font-semibold tracking-tight tabular-nums">{{ show(value, format, currency) }}</p>
        <Trend v-if="previous !== undefined && typeof value === 'number'" :value="value" :previous="previous" :lower-is-better="lowerIsBetter" />
      </div>
      <Sparkline v-if="series && series.length > 1" :values="series" class="my-1" />
      <p v-if="note" class="text-small text-muted-foreground">{{ note }}</p>
    </div>
  </Widget>
</template>
