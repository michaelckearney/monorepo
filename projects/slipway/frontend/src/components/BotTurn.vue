<script setup lang="ts">
import { computed, ref } from "vue";
import { Bot } from "lucide-vue-next";
import Transcript from "@ui/components/Transcript.vue";
import type { TranscriptItem } from "@ui/transcript";
import { sentence, state, summary, took, type Step, type Turn } from "../turns";
import { ago } from "../poll";

// One bot turn in the issue's timeline, the way Harbor's agent panel shows
// a chat: the bot's messages, and each run of steps as one line that opens
// to the steps, each of which opens to what it gave back. working is
// whether this turn is still going, so a step with no result is running
// rather than stopped.
const props = defineProps<{ turn: Turn; working: boolean }>();

const states = { running: "running…", failed: "failed", stopped: "stopped", done: "" };
const tones = { running: "muted", failed: "destructive", stopped: "muted", done: "muted" } as const;
const showPrompt = ref(false);

const steps = computed(() => new Map(props.turn.items.flatMap((i) => (i.kind === "steps" ? i.steps.map((s) => [s.key, s] as const) : []))));
const items = computed<TranscriptItem[]>(() =>
  props.turn.items.map((item): TranscriptItem => {
    if (item.kind === "text") {
      return { kind: "agent", key: item.key, text: item.text, error: item.error };
    }
    const failing = item.steps.some((s: Step) => state(s, props.working) === "failed");
    return {
      kind: "steps",
      key: item.key,
      summary: summary(item, props.working),
      attention: false,
      // It opens by itself while one of its steps fails, so a failure is not hidden.
      open: failing,
      steps: item.steps.map((s) => {
        const st = state(s, props.working);
        return {
          key: s.key,
          text: sentence(s, st === "running" ? "now" : "past"),
          state: states[st],
          tone: tones[st],
          opens: !!s.output.trim(),
          open: false,
          attention: false,
        };
      }),
    };
  }),
);
</script>

<template>
  <article class="flex gap-3">
    <span class="flex size-6 shrink-0 items-center justify-center rounded-full bg-muted text-sidebar-foreground">
      <Bot class="size-3.5" />
    </span>
    <div class="flex min-w-0 flex-1 flex-col gap-2">
      <p class="text-small text-muted-foreground">
        <strong class="font-medium text-foreground">Bot</strong>
        <template v-if="working"> is working · {{ took(turn.start, new Date().toISOString()) }}</template>
        <template v-else> took a turn · {{ ago(turn.start) }} · {{ took(turn.start, turn.end) }}</template>
        <template v-if="turn.prompt">
          ·
          <button type="button" class="hover:text-foreground" :aria-expanded="showPrompt" @click="showPrompt = !showPrompt">
            {{ showPrompt ? "hide the prompt" : "prompt" }}
          </button>
        </template>
      </p>
      <pre
        v-if="showPrompt"
        class="max-h-80 overflow-auto whitespace-pre-wrap rounded-md bg-muted px-3 py-2 font-mono text-small text-sidebar-foreground"
        >{{ turn.prompt }}</pre
      >
      <Transcript :items="items">
        <template #step="{ step }">
          <pre
            class="m-0 max-h-64 overflow-auto whitespace-pre-wrap rounded-md bg-muted px-2 py-1.5 font-mono text-small"
            :class="state(steps.get(step.key)!, working) === 'failed' ? 'text-destructive' : 'text-sidebar-foreground'"
            >{{ steps.get(step.key)!.output }}</pre
          >
        </template>
      </Transcript>
      <p v-if="!turn.items.length" class="text-muted-foreground">{{ working ? "Starting…" : "It did nothing the app saw." }}</p>
    </div>
  </article>
</template>
