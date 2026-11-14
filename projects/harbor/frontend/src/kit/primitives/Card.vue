<!--
description: A bordered surface with an optional title, for content of your own. It has no data and no loading state: to show a service's data, use Widget or a widget made for it.
keywords: card, panel, box, surface, container, note, frame, custom
example:
  <Card title="Before the appointment">Bring the insurance card and the list of medicines.</Card>
-->
<script setup lang="ts">
import { useRows, useSpan, type Span } from "../grid";

// A surface: a header over a line (the title, or the header slot, with
// actions on the right), a row of controls under it (the controls slot),
// the content, and a footer under a line. In a Grid, span is how
// many columns it takes and rows how many rows; content taller than that scrolls.
const props = withDefaults(defineProps<{
  /** The heading, unless the header slot replaces it. */
  title?: string;
  /** Columns it takes of a Grid's 12: a number, or "full" for all of them. */
  span?: Span;
  /** Rows it takes of a Grid's 8. */
  rows?: number;
}>(), { span: 4, rows: 4 });

const column = useSpan(() => props.span);
const row = useRows(() => props.rows);
</script>

<template>
  <section
    class="flex min-w-0 flex-col rounded-lg border bg-card text-card-foreground"
    :style="{ gridColumn: column, gridRow: row }"
    :aria-label="title"
  >
    <header v-if="title || $slots.header || $slots.actions" class="flex items-center justify-between gap-3 border-b px-card py-2">
      <slot name="header">
        <h3 class="flex h-control-xs min-w-0 items-center truncate font-medium">{{ title }}</h3>
      </slot>
      <div v-if="$slots.actions" class="flex h-control-xs shrink-0 items-center gap-2"><slot name="actions" /></div>
    </header>
    <div v-if="$slots.controls" class="flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-card py-2"><slot name="controls" /></div>
    <div class="min-h-0 min-w-0 flex-1 overflow-auto p-card"><slot /></div>
    <footer v-if="$slots.footer" class="flex items-center gap-2 border-t px-card py-2 text-small text-muted-foreground">
      <slot name="footer" />
    </footer>
  </section>
</template>
