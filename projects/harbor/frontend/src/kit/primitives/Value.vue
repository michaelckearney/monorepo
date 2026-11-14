<!--
description: A number, amount of money, percent, or date, formatted; money can be signed and coloured, in green and out plain.
keywords: value, number, amount, money, currency, price, balance, percent, format, signed, total
example:
  <Value :value="-84.2" format="money" signed />
example:
  <Value :value="0.42" format="percent" />
-->
<script setup lang="ts">
import { computed } from "vue";
import { format as show, type Format } from "../data";
import { toneText, type Tone } from "../tone";

// A value as format says (number, money, percent, date, month, datetime,
// time, or text). signed puts + before money in, in green, and − before
// money out. tone colours it by meaning instead.
const props = defineProps<{
  value: unknown;
  format?: Format;
  currency?: string;
  signed?: boolean;
  tone?: Tone;
}>();

const n = computed(() => Number(props.value));
const text = computed(() => {
  if (!props.signed || Number.isNaN(n.value)) {
    return show(props.value, props.format, props.currency);
  }
  const abs = show(Math.abs(n.value), props.format, props.currency);
  return n.value > 0 ? `+${abs}` : n.value < 0 ? `−${abs}` : abs;
});
</script>

<template>
  <span class="whitespace-nowrap tabular-nums" :class="tone ? toneText[tone] : signed && n > 0 && 'text-success'">{{ text }}</span>
</template>
