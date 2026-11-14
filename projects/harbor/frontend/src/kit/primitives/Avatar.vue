<!--
description: A person's or a thing's initials in a circle, the same colour every time for the same name, or their picture.
keywords: avatar, person, people, user, initials, profile, picture, sender, who, author, member
example:
  <Row><Avatar name="Sam Rivera" /><Avatar name="Dr. Anika Patel" /><Avatar name="Maple Grove" size="sm" /></Row>
-->
<script setup lang="ts">
import { computed } from "vue";

// Initials from name, in a colour that name always gets, or the picture at
// src. size is sm (20px) or md (28px).
const props = withDefaults(defineProps<{ name: string; src?: string; size?: "sm" | "md" }>(), { size: "md" });

const initials = computed(() => {
  const words = props.name
    .replace(/^(dr|mr|mrs|ms|mx)\.?\s+/i, "")
    .split(/[\s@._-]+/)
    .filter(Boolean);
  return ((words[0]?.[0] ?? "") + (words.length > 1 ? words[words.length - 1][0] : "")).toUpperCase() || "?";
});
const color = computed(() => {
  let h = 0;
  for (const c of props.name) {
    h = (h * 31 + c.charCodeAt(0)) >>> 0;
  }
  return `var(--chart-${(h % 5) + 1})`;
});
</script>

<template>
  <span
    class="inline-grid shrink-0 place-items-center overflow-hidden rounded-full font-medium"
    :class="size === 'sm' ? 'size-5 text-caption' : 'size-7 text-small'"
    :style="src ? undefined : { background: `color-mix(in oklab, ${color} 18%, transparent)`, color }"
    :title="name"
    role="img"
    :aria-label="name"
  >
    <img v-if="src" :src="src" alt="" class="size-full object-cover" />
    <template v-else>{{ initials }}</template>
  </span>
</template>
