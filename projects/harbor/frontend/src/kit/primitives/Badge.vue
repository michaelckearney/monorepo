<!--
description: A short label in a soft colour by what it means: a status, a category, a priority.
keywords: badge, tag, label, pill, chip, status, category, priority, tone
example:
  <Row><Badge tone="bad">Missing</Badge><Badge tone="good">Paid</Badge><Badge>Groceries</Badge></Row>
-->
<script setup lang="ts">
import { toneSoft, type Tone } from "../tone";

// A short label. tone is neutral, good, warning, bad, or info.
withDefaults(defineProps<{ tone?: Tone }>(), { tone: "neutral" });
</script>

<template>
  <span class="inline-flex shrink-0 items-center whitespace-nowrap rounded-full px-2 py-px text-caption font-medium" :class="toneSoft[tone]">
    <slot />
  </span>
</template>
