<!--
description: Many things, each with its state as a coloured dot and word: orders and deliveries, devices on a network, sensors and their readings, test results, services that are up or down. A detail under each, and a value on the right.
keywords: status, state, health, monitor, monitoring, devices, network, clients, connected, sensors, readings, temperature, orders, deliveries, packages, shipments, tracking, on the way, results, lab results, test results, online, offline, up, down, alerts
example:
  <StatusWidget title="On the way" :items="[{ label: 'Running shoes', status: 'Shipped', detail: 'Arrives Thu' }, { label: 'Desk lamp', status: 'Delayed', detail: 'Arrives Nov 12' }]" />
example:
  <StatusWidget title="Sensors" :items="[{ label: 'Living room', value: '21.5 °C', tone: 'good' }, { label: 'Freezer', value: '-2 °C', tone: 'bad' }]" />
-->
<script setup lang="ts">
import type { StatusItem } from "../shapes";
import { guessTone, toneFill, toneText } from "../tone";
import Item from "../primitives/Item.vue";
import Widget from "./Widget.vue";

// Things and their states, as StatusItem says. limit shows only the first
// so many.
const props = withDefaults(defineProps<{ items: StatusItem[]; limit?: number; empty?: string }>(), { empty: "Nothing to show." });

const toneOf = (s: StatusItem) => s.tone ?? guessTone(s.status);
const shown = () => (props.limit ? props.items.slice(0, props.limit) : props.items);
</script>

<template>
  <Widget :more="limit ? Math.max(0, items.length - limit) : 0">
    <p v-if="items.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <ul v-else class="grid">
      <li v-for="(s, i) in shown()" :key="i" class="border-b last:border-0">
        <Item :label="s.label" :detail="s.detail" :link="s.link">
          <template #leading>
            <span class="size-2 shrink-0 rounded-full" :class="toneFill[toneOf(s)]" :title="s.status"></span>
          </template>
          <template v-if="s.value || s.status" #trailing>
            <span v-if="s.value" class="font-medium tabular-nums">{{ s.value }}</span>
            <span v-if="s.status" class="text-small" :class="toneOf(s) === 'neutral' ? 'text-muted-foreground' : toneText[toneOf(s)]">{{ s.status }}</span>
          </template>
        </Item>
      </li>
    </ul>
  </Widget>
</template>
