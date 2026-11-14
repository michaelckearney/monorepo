<script setup lang="ts">
import { ChevronRight } from "lucide-vue-next";
import type { TranscriptItem, TranscriptStep } from "../transcript";
import Markdown from "./Markdown.vue";

// A conversation with an agent: your messages, its replies, and each run of
// its steps as one line, which opens to the steps, each of which opens to
// what the step slot shows of it. A step with nothing to show does not open.
// image is Markdown's, for the images in the agent's replies.
defineProps<{ items: readonly TranscriptItem[]; image?: (src: string) => string | null | undefined }>();
defineSlots<{ step?(props: { step: TranscriptStep }): unknown }>();

const tones = { primary: "text-primary", destructive: "text-destructive", muted: "text-muted-foreground" };
</script>

<template>
  <template v-for="item in items" :key="item.key">
    <p
      v-if="item.kind === 'you'"
      class="m-0 max-w-[92%] self-end whitespace-pre-wrap rounded-xl rounded-br-sm bg-primary px-3 py-2 leading-relaxed text-primary-foreground"
      aria-label="You"
    >
      {{ item.text }}
    </p>
    <pre
      v-else-if="item.kind === 'agent' && item.error"
      class="m-0 max-h-40 overflow-auto whitespace-pre-wrap font-mono text-small text-destructive"
      aria-label="Agent"
    >{{ item.text }}</pre>
    <Markdown v-else-if="item.kind === 'agent'" :text="item.text" :image="image" class="max-w-full leading-relaxed" aria-label="Agent" />
    <details v-else class="steps group/steps text-body" :open="item.open">
      <summary
        class="flex cursor-pointer list-none items-center gap-1 text-muted-foreground hover:text-foreground [&::-webkit-details-marker]:hidden"
      >
        <span class="steps-summary min-w-0 truncate" :class="{ 'text-primary': item.attention }">
          {{ item.summary }}
        </span>
        <ChevronRight class="size-3.5 shrink-0 transition-transform group-open/steps:rotate-90" aria-hidden="true" />
      </summary>
      <div class="mt-1.5 overflow-hidden rounded-lg border">
        <component
          :is="step.opens ? 'details' : 'div'"
          v-for="step in item.steps"
          :key="step.key"
          class="step group/step border-t first:border-t-0"
          :class="{ 'bg-primary/5': step.attention }"
          :open="step.opens ? step.open : undefined"
        >
          <component
            :is="step.opens ? 'summary' : 'div'"
            class="flex list-none items-center gap-2 px-3 py-1.5 [&::-webkit-details-marker]:hidden"
            :class="{ 'cursor-pointer hover:bg-muted/50': step.opens }"
          >
            <span class="what min-w-0 flex-1 truncate">{{ step.text }}</span>
            <span v-if="step.state" class="state shrink-0" :class="tones[step.tone]">
              {{ step.state }}
            </span>
            <ChevronRight
              v-if="step.opens"
              class="size-3.5 shrink-0 text-muted-foreground transition-transform group-open/step:rotate-90"
              aria-hidden="true"
            />
          </component>
          <div v-if="step.opens" class="grid gap-1.5 px-3 pb-2">
            <slot name="step" :step="step" />
          </div>
        </component>
      </div>
    </details>
  </template>
</template>
