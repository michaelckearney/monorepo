<!--
description: Facts about one thing, a label and a value on each line: an account's details, a router's status, a bill, a policy, a profile, a vehicle.
keywords: details, facts, info, information, summary, properties, fields, account details, profile, status, router, settings, overview, key value, about
example:
  <DetailsWidget title="Internet" :facts="[{ label: 'Status', value: 'Up' }, { label: 'Up for', value: 302, format: 'number' }, { label: 'Address', value: '203.0.113.7' }]" />
-->
<script setup lang="ts">
import { format as show } from "../data";
import type { Fact } from "../shapes";
import Widget from "./Widget.vue";

// One thing's facts, as Fact says, a line each, in currency if any is money.
defineProps<{ facts: Fact[]; currency?: string }>();
</script>

<template>
  <Widget>
    <dl class="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-4">
      <template v-for="(f, i) in facts" :key="i">
        <dt class="truncate border-b py-1.5 text-muted-foreground" :class="i === facts.length - 1 && 'border-0'">{{ f.label }}</dt>
        <dd class="truncate border-b py-1.5 text-right tabular-nums" :class="i === facts.length - 1 && 'border-0'">{{ show(f.value, f.format, currency) }}</dd>
      </template>
    </dl>
  </Widget>
</template>
