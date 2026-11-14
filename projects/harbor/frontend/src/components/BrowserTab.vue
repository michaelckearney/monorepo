<script setup lang="ts">
import { computed } from "vue";
import { useTab } from "@desktop";

// One tab in the sign-in browser's tab bar: its title, or its address while
// it has none.
const props = defineProps<{ id: string; current: boolean; closable: boolean }>();
defineEmits<{ select: [] }>();

const tab = useTab(() => props.id);
const label = computed(() => tab.title || tab.url || "New tab");
</script>

<template>
  <li class="tab" :class="{ current }">
    <button type="button" class="title" :aria-pressed="current" :title="tab.url" @click="$emit('select')">
      {{ label }}
    </button>
    <button v-if="closable" type="button" class="close" :aria-label="`Close ${label}`" @click="tab.close()">×</button>
  </li>
</template>

<style scoped>
.tab {
  display: flex;
  align-items: center;
  max-width: 14rem;
  border: 1px solid var(--border);
  border-bottom: none;
  border-radius: var(--radius-sm) var(--radius-sm) 0 0;
  background: var(--background);
}
.tab.current {
  border-color: var(--primary);
}
.title,
.close {
  border: none;
  background: none;
  color: var(--foreground);
  font: inherit;
  padding: calc(var(--spacing) * 1.5) calc(var(--spacing) * 2);
  cursor: pointer;
}
.title {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.close {
  padding-left: 0;
  color: var(--muted-foreground);
}
</style>
