<!--
description: Text in one of Harbor's sizes, muted or in a tone, cut to a number of lines.
keywords: text, label, paragraph, words, caption, heading, title, muted, truncate, clamp, font
example:
  <Text size="title" weight="semibold">Northwind payroll</Text>
example:
  <Text muted size="small" :lines="2">Your next pay is on Friday. Time off you booked is taken from it.</Text>
-->
<script setup lang="ts">
import { toneText, type Tone } from "../tone";

// Text. size is caption, small, body, or title. muted is the quieter colour
// for secondary text; tone colours it by meaning instead. lines cuts it to
// that many lines, with an ellipsis. mono is for codes and ids.
withDefaults(
  defineProps<{
    size?: "caption" | "small" | "body" | "title";
    weight?: "normal" | "medium" | "semibold";
    muted?: boolean;
    tone?: Tone;
    lines?: number;
    mono?: boolean;
    as?: string;
  }>(),
  { size: "body", weight: "normal", as: "p" },
);
</script>

<template>
  <component
    :is="as"
    class="min-w-0"
    :class="[
      { caption: 'text-caption', small: 'text-small', body: 'text-body', title: 'text-title' }[size],
      { normal: 'font-normal', medium: 'font-medium', semibold: 'font-semibold' }[weight],
      tone ? toneText[tone] : muted && 'text-muted-foreground',
      lines === 1 && 'truncate',
      mono && 'font-mono',
    ]"
    :style="lines && lines > 1 ? { display: '-webkit-box', WebkitLineClamp: lines, WebkitBoxOrient: 'vertical', overflow: 'hidden' } : undefined"
  >
    <slot />
  </component>
</template>
