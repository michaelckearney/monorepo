<!--
description: A coloured dot and a word, for the state of something: online, delivered, failed, pending.
keywords: status, state, dot, indicator, online, offline, health, up, down, condition
example:
  <Stack gap="tight"><Status tone="good">Online</Status><Status tone="bad">Offline</Status><Status label="pending" /></Stack>
-->
<script setup lang="ts">
import { computed } from "vue";
import { guessTone, toneFill, toneText, type Tone } from "../tone";

// A dot and a word. Given label without tone, the tone comes from the word:
// "delivered" is good, "pending" a warning, "failed" bad.
const props = defineProps<{ label?: string; tone?: Tone }>();
const t = computed(() => props.tone ?? guessTone(props.label));
</script>

<template>
  <span class="inline-flex min-w-0 items-center gap-1.5">
    <span class="size-2 shrink-0 rounded-full" :class="toneFill[t]"></span>
    <span class="truncate" :class="t !== 'neutral' && toneText[t]"><slot>{{ label }}</slot></span>
  </span>
</template>
