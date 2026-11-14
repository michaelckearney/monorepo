<!--
description: A button: primary for the one main thing, secondary, or ghost for quiet ones.
keywords: button, action, click, press, submit, link button
example:
  <Row><Button>Refresh</Button><Button variant="secondary">Open site</Button><Button variant="ghost" size="sm">More</Button></Row>
-->
<script setup lang="ts">
// A button. variant is primary, secondary, or ghost; size is sm or md.
// @click and the button's own attributes, such as disabled, pass through.
withDefaults(defineProps<{ variant?: "primary" | "secondary" | "ghost"; size?: "sm" | "md" }>(), {
  variant: "primary",
  size: "md",
});
</script>

<template>
  <button
    type="button"
    class="inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium outline-none transition-colors focus-visible:ring-3 focus-visible:ring-ring/50 disabled:pointer-events-none disabled:opacity-50 [&_svg]:size-4"
    :class="[
      size === 'sm' ? 'h-control-sm px-2' : 'h-control px-3',
      {
        primary: 'bg-primary text-primary-foreground hover:bg-primary/90',
        secondary: 'border bg-card hover:bg-accent hover:text-accent-foreground',
        ghost: 'hover:bg-accent hover:text-accent-foreground',
      }[variant],
    ]"
  >
    <slot />
  </button>
</template>
