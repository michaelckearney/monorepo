<!--
description: Any list, one a row with a title, a detail under it, and a value on the right, under group headings if you like, with a bar under each if it has a percent. For lists nothing else is made for.
keywords: list, items, rows, entries, records, accounts, balances, grouped list, simple list, generic, anything, names and values, search results
example:
  <ListWidget title="Accounts" :items="[{ title: 'Checking', detail: '••4410', value: 1840.12 }, { title: 'Savings', detail: '••9921', value: 12500 }]" format="money" total />
-->
<script setup lang="ts">
import { computed } from "vue";
import type { Format } from "../data";
import type { ListItem } from "../shapes";
import ItemList from "../primitives/ItemList.vue";
import Widget from "./Widget.vue";

// Items, as ListItem says. format is how their values are shown, in
// currency if they are money. total adds up the values shown, in the
// footer. limit shows only the first so many. @open, given the item, runs
// when you click one, in place of following its link.
const props = withDefaults(
  defineProps<{
    items: ListItem[];
    format?: Format;
    currency?: string;
    total?: boolean;
    limit?: number;
    onOpen?: (item: ListItem) => void;
    empty?: string;
  }>(),
  { empty: "Nothing to show." },
);

const shown = computed(() => (props.limit ? props.items.slice(0, props.limit) : props.items));
const sum = computed(() =>
  props.total ? { value: shown.value.reduce((s, i) => s + (Number(i.value) || 0), 0), format: props.format, currency: props.currency } : undefined,
);
const has = (key: keyof ListItem) => props.items.some((i) => i[key] !== undefined);
</script>

<template>
  <Widget :total="sum" :more="limit ? Math.max(0, items.length - limit) : 0">
    <p v-if="shown.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <ItemList
      v-else
      :items="shown"
      label="title"
      detail="detail"
      :value="has('value') ? 'value' : undefined"
      :link="onOpen ? undefined : 'link'"
      :group="has('group') ? 'group' : undefined"
      :meter="has('meter') ? 'meter' : undefined"
      :format="format"
      :currency="currency"
      :on-open="onOpen"
    />
  </Widget>
</template>
