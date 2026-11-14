<script setup lang="ts">
import { computed } from "vue";
import { highlight, type Language } from "../highlight";

// Code, coloured by its language, from the theme's chart colours. With no
// language, it is plain text.
const props = defineProps<{ text: string; language?: Language }>();

const tokens = computed(() => (props.language ? highlight(props.text, props.language) : [{ text: props.text, class: "" }]));
</script>

<template>
  <pre class="code m-0 overflow-auto whitespace-pre-wrap break-words font-mono"><span
    v-for="(token, i) in tokens"
    :key="i"
    :class="token.class || undefined"
  >{{ token.text }}</span></pre>
</template>

<style scoped>
.tok-keyword {
  color: var(--chart-3);
}
.tok-string {
  color: var(--chart-4);
}
.tok-literal {
  color: var(--chart-2);
}
.tok-property {
  color: var(--chart-1);
}
.tok-type {
  color: var(--chart-5);
}
.tok-comment {
  color: var(--muted-foreground);
  font-style: italic;
}
</style>
