<script setup lang="ts">
import { Button, type ButtonVariants } from "./button";
import Tip from "./Tip.vue";

// A button that is only an icon. Its label is its tooltip, and what a screen
// reader says, unless `name` says more (Delete, and "Delete Family").
defineOptions({ inheritAttrs: false });
withDefaults(
  defineProps<{
    label: string;
    name?: string;
    variant?: ButtonVariants["variant"];
    size?: "icon-xs" | "icon-sm" | "icon";
  }>(),
  { variant: "ghost", size: "icon-sm" },
);
</script>

<template>
  <Tip :label="label">
    <Button v-bind="$attrs" :variant="variant" :size="size" :aria-label="name ?? label"><slot /></Button>
  </Tip>
</template>
