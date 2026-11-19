<script setup lang="ts">
import { effect, type Tool } from "../manifest";

defineProps<{ tool: Tool }>();
</script>

<template>
  <span
    class="effect rounded-full px-2 py-px text-caption font-medium"
    :class="{
      'bg-destructive/15 text-destructive': effect(tool) === 'destructive',
      'bg-muted text-muted-foreground': effect(tool) !== 'destructive',
    }"
  >
    {{ effect(tool) }}
  </span>
</template>
