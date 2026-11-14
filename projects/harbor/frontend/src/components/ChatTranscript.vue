<script setup lang="ts">
import { computed } from "vue";
import Transcript from "@ui/components/Transcript.vue";
import type { TranscriptItem, TranscriptStep } from "@ui/transcript";
import { blocks, code, describe, grouped, input, state, summary, tense, type Block, type Item, type ToolStep } from "../chat";
import Code from "./Code.vue";

// A chat's messages and tool steps, as the agent's panel shows them: live in
// a chat, or saved in a benchmark trial. working is whether the agent is
// still going, so a step with no result is running rather than stopped. Each
// step opens to what it was given that its words do not say, and what it
// gave back. A step waiting for you is open already. The asking slot is
// what it shows, in a chat that can answer it.
const props = defineProps<{ items: readonly Item[]; working: boolean }>();
defineSlots<{ asking?(props: { step: ToolStep }): unknown }>();

// What a step shows beside its words; a step that went well shows nothing.
const states = {
  asking: "needs your permission",
  refused: "you said no",
  failed: "failed",
  done: "",
  running: "running…",
  stopped: "stopped",
};
const tones = {
  asking: "primary",
  refused: "destructive",
  failed: "destructive",
  done: "muted",
  running: "muted",
  stopped: "muted",
} as const;
const asking = (s: ToolStep) => state(s, props.working) === "asking";
// What a step opens to: its input, then its output; a failed step's error is apart.
const parts = (s: ToolStep): Block[][] =>
  [blocks(input(s), code(s).input), s.error ? [] : blocks(s.result, code(s).output)].filter((p) => p.length > 0);
const opens = (s: ToolStep) => asking(s) || !!s.error || parts(s).length > 0;

// Each step's tool call, by its key, for the step slot.
const tools = computed(() => new Map(props.items.flatMap((i) => (i.kind === "tool" ? [[i.key, i] as const] : []))));
const shown = computed<TranscriptItem[]>(() =>
  grouped(props.items).map((item) => {
    if (item.kind !== "steps") {
      return item;
    }
    const steps = item.steps.map((s): TranscriptStep => {
      const st = state(s, props.working);
      return {
        key: s.key,
        text: describe(s, tense(s, props.working)),
        state: states[st],
        tone: tones[st],
        opens: opens(s),
        open: asking(s),
        attention: asking(s),
      };
    });
    const attention = item.steps.some(asking);
    return { kind: "steps", key: item.key, summary: summary(item, props.working), attention, open: attention, steps };
  }),
);
</script>

<template>
  <Transcript :items="shown">
    <template #step="{ step }">
      <template v-for="tool in [tools.get(step.key)!]" :key="step.key">
        <slot v-if="asking(tool)" name="asking" :step="tool" />
        <div v-for="(part, i) in parts(tool)" :key="i" class="part grid gap-2 rounded-md bg-muted px-2 py-1.5 text-small">
          <template v-for="(block, j) in part" :key="j">
            <dl v-if="block.kind === 'fields'" class="m-0 grid grid-cols-[auto_minmax(0,1fr)] gap-x-3">
              <template v-for="[label, value] in block.fields" :key="label">
                <dt class="text-muted-foreground">{{ label }}</dt>
                <dd class="m-0 break-words">{{ value }}</dd>
              </template>
            </dl>
            <div v-else class="grid gap-0.5">
              <span v-if="block.label" class="text-muted-foreground">{{ block.label }}</span>
              <Code :text="block.text" :language="block.language" class="max-h-64" />
            </div>
          </template>
        </div>
        <p v-if="tool.error" class="m-0 whitespace-pre-wrap text-destructive">{{ tool.error }}</p>
      </template>
    </template>
  </Transcript>
</template>
