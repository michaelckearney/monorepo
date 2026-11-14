<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { Maximize2, Minimize2, PanelRightClose } from "lucide-vue-next";
import type { Result } from "../benchmarks";
import { chatItems } from "../chat";
import ChatTranscript from "./ChatTranscript.vue";
import IconButton from "@ui/components/IconButton.vue";

// A trial's chat, beside its run as the agent's panel is beside Harbor, and
// drawn as it draws a chat, with nothing to send. While the trial goes, its
// chat grows, and the panel follows it, unless you have scrolled up.
const props = defineProps<{
  title: string;
  transcript: Result["transcript"];
  working: boolean;
  /** The trial itself failed, rather than the agent's turn. */
  failed: boolean;
  error: string;
  wide: boolean;
}>();
const emit = defineEmits<{ widen: []; close: [] }>();

const items = computed(() =>
  chatItems(
    props.transcript.map((m, i) => ({
      seq: i,
      role: m.role,
      parts: JSON.stringify(m.parts),
    })),
  ),
);

const log = ref<HTMLElement | null>(null);
watch(
  () => props.transcript.length,
  async () => {
    const el = log.value;
    if (!el || el.scrollHeight - el.scrollTop - el.clientHeight > 80) {
      return;
    }
    await nextTick();
    el.scrollTop = el.scrollHeight;
  },
);
</script>

<template>
  <aside class="flex min-h-0 min-w-0 flex-col border-l bg-background" aria-label="Trial chat">
    <header class="flex min-h-14 shrink-0 items-center gap-1 border-b py-2 pl-4 pr-2">
      <h2 class="min-w-0 flex-1 truncate font-medium">{{ title }}</h2>
      <IconButton :label="wide ? 'Narrow the chat' : 'Widen the chat'" @click="emit('widen')">
        <Minimize2 v-if="wide" /><Maximize2 v-else />
      </IconButton>
      <IconButton label="Close the trial" @click="emit('close')"><PanelRightClose /></IconButton>
    </header>
    <div ref="log" class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4" aria-label="Chat" role="log">
      <ChatTranscript :items="items" :working="working" />
      <p v-if="error" class="m-0 whitespace-pre-wrap text-destructive" role="alert">
        {{ failed ? `The trial failed: ${error}` : `The turn ended with an error: ${error}` }}
      </p>
    </div>
  </aside>
</template>
