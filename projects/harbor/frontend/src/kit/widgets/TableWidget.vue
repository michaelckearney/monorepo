<!--
description: Rows in columns with headings, numbers lined up on the right: for anything with several fields worth comparing row by row.
keywords: table, rows, columns, grid, spreadsheet, data, records, report, comparison, list with columns, tabular
example:
  <TableWidget title="Largest charges" span="full" :records="[{ date: '2026-10-30', description: 'Harbor Grocers', account: 'Checking', amount: -84.2 }, { date: '2026-10-28', description: 'City Water', account: 'Checking', amount: -42.17 }]" :columns="['date:date', 'description', 'account', 'amount:money']" />
-->
<script setup lang="ts">
import { computed } from "vue";
import { column, format, numeric, type Column } from "../data";
import Widget from "./Widget.vue";

// Records, a row each, in columns: :columns="['date:date', 'payee', 'amount:money']", each a
// field of the records with a format after a colon, or { key, label, format,
// value }, where value is a function of the row. link, a field of the
// records, is a URL the first column opens. limit shows only the first so
// many.
const props = withDefaults(
  defineProps<{
    records: Record<string, unknown>[];
    columns: (string | Column)[];
    link?: string;
    currency?: string;
    limit?: number;
    empty?: string;
  }>(),
  { empty: "Nothing to show." },
);

const cols = computed(() => props.columns.map(column));
const shown = computed(() => (props.limit ? props.records.slice(0, props.limit) : props.records));
type Col = ReturnType<typeof column>;
const right = (row: unknown, c: Col) => numeric(c.format) || (c.format === "text" && typeof c.value(row) === "number");
const href = (row: Record<string, unknown>) => (props.link ? String(row[props.link] ?? "") || undefined : undefined);
</script>

<template>
  <Widget :more="limit ? Math.max(0, records.length - limit) : 0">
    <p v-if="shown.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <div v-else class="-mx-1 overflow-x-auto">
      <table class="w-full border-collapse tabular-nums">
        <thead>
          <tr>
            <th
              v-for="c in cols"
              :key="c.key"
              class="border-b px-2 py-1.5 text-left text-small font-medium text-muted-foreground"
              :class="{ 'text-right': numeric(c.format) }"
            >
              {{ c.label }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(row, i) in shown" :key="i" class="border-b last:border-0">
            <td v-for="(c, ci) in cols" :key="c.key" class="px-2 py-1.5 align-top" :class="{ 'whitespace-nowrap text-right': right(row, c) }">
              <a v-if="ci === 0 && href(row)" :href="href(row)" class="hover:underline">{{ format(c.value(row), c.format, currency) }}</a>
              <template v-else>{{ format(c.value(row), c.format, currency) }}</template>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </Widget>
</template>
