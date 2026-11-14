<!--
description: A bar, line, or area chart, for a widget of your own: one series or several, with round numbers on the axis. ChartWidget is one in a widget.
keywords: chart, graph, plot, bar chart, line chart, area chart, series, visualization
example:
  <Chart :series="[{ name: 'kWh', points: [{ x: '2026-06', y: 410 }, { x: '2026-07', y: 520 }, { x: '2026-08', y: 498 }] }]" x-format="month" :height="120" />
-->
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { format as show, type Format } from "../data";
import type { Series } from "../shapes";

// A bar, line, or area chart of one series or several, as Series says,
// with a legend when there are several. The first series' points are the
// labels along the bottom, shown as xFormat says ("month", "date"). format
// is how values are shown, in currency if they are money. height is in
// pixels; title says what it shows, for screen readers.
const props = withDefaults(
  defineProps<{
    series: Series[];
    kind?: "bar" | "line" | "area";
    xFormat?: Format;
    format?: Format;
    currency?: string;
    height?: number;
    title?: string;
    empty?: string;
  }>(),
  { kind: "bar", height: 200, empty: "Nothing to show." },
);

const box = ref<HTMLElement | null>(null);
const width = ref(480);
const observer = new ResizeObserver(([entry]) => (width.value = Math.max(160, entry.contentRect.width)));
// The chart is not there while there is nothing to show.
watch(
  box,
  (el) => {
    observer.disconnect();
    if (el) {
      observer.observe(el);
    }
  },
  { flush: "post" },
);
onUnmounted(() => observer.disconnect());

/** A round step for about five ticks between 0 and max. */
function step(span: number) {
  const raw = span / 4 || 1;
  const p = 10 ** Math.floor(Math.log10(raw));
  return ([1, 2, 2.5, 5, 10].map((m) => m * p).find((s) => s >= raw) ?? 10 * p);
}

const chart = computed(() => {
  const first = props.series[0]?.points ?? [];
  const series = props.series.map((s) => first.map((p) => s.points.find((q) => q.x === p.x)?.y ?? 0));
  const all = series.flat();
  const lo = Math.min(0, ...all);
  const hi = Math.max(0, ...all);
  const s = step(hi - lo);
  const min = Math.floor(lo / s) * s;
  const max = Math.ceil(hi / s) * s || s;
  const ticks: number[] = [];
  for (let t = min; t <= max + s / 2; t += s) {
    ticks.push(t);
  }
  return {
    xs: first.map((p) => show(p.x, props.xFormat)),
    series: series.map((values, i) => ({ key: i, name: props.series[i].name, values })),
    min,
    max,
    ticks,
  };
});

const pad = { left: 52, right: 8, top: 10, bottom: 24 };
const plotW = computed(() => width.value - pad.left - pad.right);
const plotH = computed(() => props.height - pad.top - pad.bottom);
const yAt = (v: number) => pad.top + plotH.value - ((v - chart.value.min) / (chart.value.max - chart.value.min)) * plotH.value;
const band = computed(() => plotW.value / Math.max(1, chart.value.xs.length));
const xAt = (i: number) => pad.left + band.value * (i + 0.5);
// Every label, unless they would run into each other.
const every = computed(() => Math.max(1, Math.ceil((chart.value.xs.length * 56) / plotW.value)));
const color = (i: number) => `var(--chart-${(i % 5) + 1})`;
const value = (v: number) => show(v, props.format, props.currency);
// An axis's round numbers need no cents.
const tick = (v: number) =>
  props.format === "money"
    ? new Intl.NumberFormat(undefined, { style: "currency", currency: props.currency ?? "USD", maximumFractionDigits: 0 }).format(v)
    : value(v);

function bar(si: number, i: number, v: number) {
  const n = chart.value.series.length;
  const w = (band.value * 0.7) / n;
  const x = pad.left + band.value * i + band.value * 0.15 + w * si;
  const y0 = yAt(0);
  const y = yAt(v);
  return { x, y: Math.min(y, y0), w: Math.max(1, w - 2), h: Math.max(1, Math.abs(y0 - y)) };
}
function path(values: number[], closed: boolean) {
  const line = values.map((v, i) => `${i ? "L" : "M"}${xAt(i)},${yAt(v)}`).join("");
  return closed && values.length ? `${line}L${xAt(values.length - 1)},${yAt(0)}L${xAt(0)},${yAt(0)}Z` : line;
}
</script>

<template>
  <p v-if="chart.xs.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
  <div v-else ref="box" class="grid min-w-0 gap-2">
      <div v-if="chart.series.length > 1" class="flex flex-wrap gap-3">
        <span v-for="(s, i) in chart.series" :key="s.key" class="flex items-center gap-1 text-caption text-muted-foreground">
          <span class="size-2 rounded-sm" :style="{ background: color(i) }"></span>{{ s.name }}
        </span>
      </div>
      <svg :width="width" :height="height" class="block overflow-visible" role="img" :aria-label="title">
        <g v-for="t in chart.ticks" :key="t">
          <line :x1="pad.left" :x2="width - pad.right" :y1="yAt(t)" :y2="yAt(t)" stroke="var(--border)" />
          <text :x="pad.left - 6" :y="yAt(t) + 3.5" text-anchor="end" class="fill-muted-foreground text-caption">
            {{ tick(t) }}
          </text>
        </g>
        <template v-for="(label, i) in chart.xs" :key="i">
          <text
            v-if="i % every === 0"
            :x="xAt(i)"
            :y="height - 6"
            text-anchor="middle"
            class="fill-muted-foreground text-caption"
          >
            {{ label }}
          </text>
        </template>
        <template v-for="(s, si) in chart.series" :key="s.key">
          <template v-if="kind === 'bar'">
            <rect
              v-for="(v, i) in s.values"
              :key="i"
              v-bind="{ x: bar(si, i, v).x, y: bar(si, i, v).y, width: bar(si, i, v).w, height: bar(si, i, v).h }"
              rx="2"
              :fill="color(si)"
            >
              <title>{{ chart.xs[i] }}: {{ value(v) }}</title>
            </rect>
          </template>
          <template v-else>
            <path v-if="kind === 'area'" :d="path(s.values, true)" :fill="color(si)" fill-opacity="0.15" />
            <path :d="path(s.values, false)" fill="none" :stroke="color(si)" stroke-width="2" stroke-linejoin="round" />
            <circle v-for="(v, i) in s.values" :key="i" :cx="xAt(i)" :cy="yAt(v)" r="2.5" :fill="color(si)">
              <title>{{ chart.xs[i] }}: {{ value(v) }}</title>
            </circle>
          </template>
        </template>
      </svg>
  </div>
</template>
