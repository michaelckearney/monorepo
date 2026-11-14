<script setup lang="ts">
import { computed, ref } from "vue";
import { Trash2 } from "lucide-vue-next";
import { Button } from "@ui/components/button";
import IconButton from "@ui/components/IconButton.vue";

// Delete asks once more, in place: deleting cannot be undone. It is quiet
// until then, and red only where you confirm. As an icon, it is a small
// trash can, for a crowded header; icon-xs is that in a row of a list, and xs
// is a word there. A row already says what it deletes, so it asks without
// repeating it, and names it only for a screen reader.
const props = withDefaults(defineProps<{ what: string; size?: "sm" | "xs" | "icon" | "icon-xs" }>(), { size: "sm" });
const emit = defineEmits<{ confirm: [] }>();
const asking = ref(false);
const inRow = computed(() => props.size === "xs" || props.size === "icon-xs");
</script>

<template>
  <span v-if="asking" class="inline-flex items-center gap-1.5" role="group" :aria-label="`Delete ${what}?`">
    <span v-if="!inRow" class="whitespace-nowrap text-body">Delete {{ what }}?</span>
    <Button type="button" :size="inRow ? 'xs' : 'sm'" variant="destructive" @click="emit('confirm')">Yes, delete</Button>
    <Button type="button" :size="inRow ? 'xs' : 'sm'" variant="outline" @click="asking = false">Keep</Button>
  </span>
  <IconButton
    v-else-if="size === 'icon' || size === 'icon-xs'"
    type="button"
    label="Delete"
    :name="`Delete ${what}`"
    :size="size === 'icon-xs' ? 'icon-xs' : 'icon-sm'"
    @click="asking = true"
  >
    <Trash2 />
  </IconButton>
  <Button v-else type="button" :size="size" variant="ghost" :aria-label="size === 'xs' ? `Delete ${what}` : undefined" @click="asking = true">
    Delete
  </Button>
</template>
