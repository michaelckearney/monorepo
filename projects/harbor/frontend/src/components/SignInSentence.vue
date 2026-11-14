<script setup lang="ts">
import { computed } from "vue";
import { signingIn } from "../services";

// A sentence that names the services you must sign in to, each a link that
// signs you in: "Sign in to Clearwater Health, Maple Grove Schools, and
// Payday to see this." lead and end are the words around the names.
const props = defineProps<{ lead: string; end: string; items: { title: string; signIn: () => unknown }[] }>();

// The names joined as the language joins a list, with each name's place kept.
const parts = computed(() =>
  new Intl.ListFormat(undefined, { type: "conjunction" })
    .formatToParts(props.items.map((_, i) => String(i)))
    .map((p) => (p.type === "element" ? { item: props.items[Number(p.value)] } : { text: p.value })),
);
</script>

<template>
  <p>
    {{ lead }}
    <template v-for="(part, i) in parts" :key="i"
      ><button
        v-if="part.item"
        type="button"
        class="font-medium text-primary underline underline-offset-2 hover:text-primary/80 disabled:cursor-default disabled:opacity-50"
        :disabled="signingIn !== ''"
        @click="part.item.signIn()"
      >{{ part.item.title }}</button
      ><template v-else>{{ part.text }}</template></template
    >
    {{ end }}
  </p>
</template>
