<!--
description: Lays out a dashboard's widgets on a grid of 12 columns by 8 rows that fills the window, the same on every dashboard; span on a widget is how many columns it takes, rows how many rows, and together they must fit. When the window is narrow, the widgets stack in one column, in the order they are written.
keywords: grid, layout, columns, rows, dashboard layout, arrange, responsive, span, stack, mobile, narrow, fit, size
example:
  <Grid>
    <Card title="One" :span="6" :rows="4">Left half, top</Card>
    <Card title="Two" :span="6" :rows="4">Right half, top</Card>
    <Card title="Three" span="full" :rows="4">The whole bottom half</Card>
  </Grid>
-->
<script setup lang="ts">
import { inject, nextTick, onMounted, onUnmounted, provide, ref, watch } from "vue";
import { columns, rowHeight, rows, stackBelow, stackedKey, viewKey } from "../grid";

// A dashboard's layout: the same grid on every dashboard, columns by rows,
// filling the view, so the dashboard never scrolls. Each widget takes span columns and rows rows,
// and stays that size whether it is loading, signed out, or full. Widgets
// that need more room than the grid has say so in place of the dashboard.
//
// Narrower than stackBelow, as on a small window or with the agent's panel
// wide, the widgets stack in one column, in the order they are written,
// each its rows of rowHeight tall, and the dashboard scrolls. Outside a
// dashboard, as in the kit's examples, the rows are rowHeight too.
const el = ref<HTMLElement | null>(null);
const view = inject(viewKey, ref(null));
const stacked = ref(false);
/** The grid's height filling the view, and its rows', in px. */
const fit = ref<{ height: number; row: number } | null>(null);
const overflows = ref(false);
provide(stackedKey, stacked);

async function measure() {
  const grid = el.value!;
  const rem = parseFloat(getComputedStyle(document.documentElement).fontSize);
  stacked.value = grid.clientWidth < stackBelow * rem;
  const v = view.value;
  if (stacked.value || !v) {
    fit.value = null;
    overflows.value = false;
    return;
  }
  const style = getComputedStyle(v);
  const above = grid.getBoundingClientRect().top - v.getBoundingClientRect().top + v.scrollTop;
  const gap = parseFloat(getComputedStyle(grid).rowGap);
  const height = v.clientHeight - parseFloat(style.paddingBottom) - above;
  fit.value = { height, row: (height - (rows - 1) * gap) / rows };
  // Widgets past the last row make rows of their own, below the grid's
  // height, once it is laid out with these rows.
  await nextTick();
  overflows.value = grid.scrollHeight > grid.clientHeight + 1;
}

const resized = new ResizeObserver(measure);
// Widgets coming and going, or something shown above the dashboard, such
// as the sign-ins it needs.
const changed = new MutationObserver(measure);
function observe(v: HTMLElement | null) {
  if (v) {
    resized.observe(v);
    changed.observe(v, { childList: true });
  }
}
onMounted(() => {
  resized.observe(el.value!);
  changed.observe(el.value!, { childList: true });
  observe(view.value);
});
// The page sets the view once it is mounted, which is after the Grid in it.
watch(view, observe, { flush: "post" });
onUnmounted(() => {
  resized.disconnect();
  changed.disconnect();
});
</script>

<template>
  <p v-if="overflows" class="mb-3 text-destructive" role="alert">
    These widgets need more room than a dashboard has: {{ columns }} columns by {{ rows }} rows. Make some smaller, with span
    and rows, or take some out.
  </p>
  <!-- dense: a small widget fills a gap a big one left before it. -->
  <div
    ref="el"
    class="grid grid-flow-row-dense gap-4 overflow-hidden"
    :style="
      stacked
        ? { gridTemplateColumns: 'minmax(0, 1fr)', gridAutoRows: `${rowHeight}rem` }
        : {
            gridTemplateColumns: `repeat(${columns}, minmax(0, 1fr))`,
            gridTemplateRows: `repeat(${rows}, ${fit ? `${fit.row}px` : `${rowHeight}rem`})`,
            height: fit ? `${fit.height}px` : undefined,
          }
    "
  >
    <slot />
  </div>
</template>
