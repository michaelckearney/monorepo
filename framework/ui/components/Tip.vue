<script setup lang="ts">
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./tooltip";

// A tooltip on the one element in its slot, such as a button that is only an
// icon. It has its own provider, so it works wherever it is put.
defineProps<{ label: string; side?: "top" | "right" | "bottom" | "left" }>();
</script>

<template>
  <TooltipProvider :delay-duration="300">
    <Tooltip>
      <TooltipTrigger as-child><slot /></TooltipTrigger>
      <TooltipContent :side="side">{{ label }}</TooltipContent>
    </Tooltip>
  </TooltipProvider>
</template>
