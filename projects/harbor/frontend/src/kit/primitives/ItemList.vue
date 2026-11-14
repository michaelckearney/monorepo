<!--
description: A list of items, one a row with a label, a detail, and a value, under group headings if you like. ListWidget's and TasksWidget's rows; with the item slot, any rows you make.
keywords: list, rows, items, entries, grouped, groups, headings, lines, records
example:
  <ItemList :items="[{ name: 'Checking', number: '••4410', balance: 1840.12 }, { name: 'Savings', number: '••9921', balance: 12500 }]" label="name" detail="number" value="balance" format="money" />
example:
  <ItemList :items="[{ kid: 'Sam', what: 'Fractions worksheet' }, { kid: 'Sam', what: 'Read ch. 4' }, { kid: 'Jo', what: 'Spelling test' }]" group="kid">
    <template #item="{ item }"><Item :label="item.what" /></template>
  </ItemList>
-->
<script setup lang="ts" generic="T">
import { computed } from "vue";
import { get, show, type Format, type Getter } from "../data";
import Item from "./Item.vue";
import Meter from "./Meter.vue";

// Items, one a row. Each of label, detail, value, link, group, and meter is
// a field, with a format after a colon ("due:date"), or a function of the
// item. group puts the items under headings, in the order they come; meter,
// from 0 to 100, draws a bar under the item. The item slot, given
// { item, index }, makes each row instead. @open, given the item, runs
// when you click a row.
const props = defineProps<{
  items: T[];
  label?: Getter;
  detail?: Getter;
  value?: Getter;
  link?: Getter;
  group?: Getter;
  meter?: Getter;
  format?: Format;
  currency?: string;
  onOpen?: (item: T) => void;
}>();

const groups = computed(() => {
  const out: { name: string; items: { item: T; index: number }[] }[] = [];
  props.items.forEach((item, index) => {
    const name = props.group === undefined ? "" : show(item, props.group);
    const last = out[out.length - 1];
    if (last && last.name === name) {
      last.items.push({ item, index });
    } else {
      out.push({ name, items: [{ item, index }] });
    }
  });
  return out;
});
const text = (item: unknown, g: Getter | undefined) => (g === undefined ? undefined : show(item, g));
const href = (item: unknown) => (props.link === undefined ? undefined : String(get(item, props.link) ?? "") || undefined);
const valueText = (item: unknown) => show(item, props.value, props.format, props.currency);
</script>

<template>
  <div class="grid">
    <section v-for="(g, gi) in groups" :key="gi" class="grid">
      <h4 v-if="g.name" class="pb-0.5 pt-3 text-small font-medium text-muted-foreground first:pt-0">{{ g.name }}</h4>
      <ul class="grid">
        <li
          v-for="{ item, index } in g.items"
          :key="index"
          class="border-b last:border-0"
          :class="onOpen && 'cursor-pointer hover:bg-accent/50'"
          @click="onOpen?.(item)"
        >
          <slot name="item" :item="item" :index="index">
            <Item :label="text(item, label)" :detail="text(item, detail)" :link="href(item)">
              <template v-if="value !== undefined" #trailing>
                <span class="font-medium tabular-nums">{{ valueText(item) }}</span>
              </template>
              <Meter v-if="meter !== undefined" class="mt-1" :value="Number(get(item, meter)) || 0" />
            </Item>
          </slot>
        </li>
      </ul>
    </section>
  </div>
</template>
