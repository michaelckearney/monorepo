<script setup lang="ts">
import { ref } from "vue";
import { Check, Copy } from "lucide-vue-next";
import IconButton from "@ui/components/IconButton.vue";

// Copies text, and shows a check for a moment after.
const props = defineProps<{ text: string; label?: string }>();
const copied = ref(false);
let timer: ReturnType<typeof setTimeout> | undefined;

async function copy() {
  await navigator.clipboard.writeText(props.text);
  copied.value = true;
  clearTimeout(timer);
  timer = setTimeout(() => (copied.value = false), 1500);
}
</script>

<template>
  <IconButton :label="copied ? 'Copied' : (label ?? 'Copy')" @click="copy">
    <Check v-if="copied" /><Copy v-else />
  </IconButton>
</template>
