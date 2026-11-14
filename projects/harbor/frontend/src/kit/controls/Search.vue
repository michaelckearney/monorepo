<!--
description: A box to type words into, to search a service or filter a list. For a widget of your own: a widget in the catalog, or the Widget frame, shows one from its search prop.
keywords: search, filter, find, query, text box, input, type, look up, keyword
example:
  <Search :model-value="''" placeholder="Search messages" />
-->
<script setup lang="ts">
import { Search as SearchIcon } from "lucide-vue-next";

// A search box. v-model is what is typed; placeholder the hint in it, and
// its name for screen readers.
defineProps<{ placeholder?: string }>();
const model = defineModel<string>();
</script>

<template>
  <span class="relative inline-flex min-w-48">
    <SearchIcon class="pointer-events-none absolute left-2 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
    <input
      v-model="model"
      type="search"
      :placeholder="placeholder"
      :aria-label="placeholder ?? 'Search'"
      class="h-control w-full rounded-md border border-input bg-card pl-8 pr-3 text-body outline-none placeholder:text-muted-foreground focus-visible:ring-3 focus-visible:ring-ring/50"
    />
  </span>
</template>
