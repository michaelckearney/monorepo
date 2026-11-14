<script setup lang="ts">
import { computed } from "vue";
import { Plug } from "lucide-vue-next";
import { markBroken, useServiceIcons } from "../icons";

// A service's favicon, or a plug when it has none yet. Both take the same
// space, so a list does not shift when an icon arrives.
const props = defineProps<{ url: string }>();
const iconFor = useServiceIcons();
const src = computed(() => iconFor.value(props.url));
</script>

<template>
  <img v-if="src" :src="src" alt="" class="size-4 shrink-0 object-contain" @error="markBroken(src)" />
  <Plug v-else class="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
</template>
