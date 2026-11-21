<script lang="ts">
import type { Annotations } from "../manifest";

export type AgentTool = { name: string; description: string; annotations?: Annotations; inputSchema: Record<string, unknown> };
</script>

<script setup lang="ts">
import { ref, watch } from "vue";
import { AgentTools } from "@backend";
import { Button } from "@ui/components/button";
import { exampleInput, takesInput } from "../manifest";

// Runs one of the agent's tools and shows the result as the agent would get
// it, and how long the call took. A tool that takes an input runs from the
// Run beside the JSON box; one that takes none has no box and is run by the
// row's own Run, through the exposed run. So each tool has one Run.
const props = defineProps<{ tool: AgentTool }>();
const emit = defineEmits<{ running: [boolean] }>();

const hasInput = takesInput(props.tool.inputSchema);
const input = ref(exampleInput(props.tool.inputSchema));
const result = ref("");
const took = ref("");
const problem = ref("");
const running = ref(false);

// Whether the agent would ask before this call. For some tools it depends on
// the input, so it follows what you type; "" while that is not JSON.
const asks = ref<boolean | "">("");
watch(
  input,
  async (text) => {
    try {
      JSON.parse(text);
    } catch {
      asks.value = "";
      return;
    }
    try {
      const answer = await AgentTools.Asks(props.tool.name, text);
      // An answer for older input is out of date.
      if (input.value === text) {
        asks.value = answer;
      }
    } catch {
      asks.value = "";
    }
  },
  { immediate: true },
);

async function run() {
  if (running.value) {
    return;
  }
  result.value = "";
  took.value = "";
  problem.value = "";
  try {
    JSON.parse(input.value);
  } catch {
    problem.value = "The input is not JSON.";
    return;
  }
  running.value = true;
  emit("running", true);
  const start = performance.now();
  try {
    const text = await AgentTools.Call(props.tool.name, input.value);
    result.value = JSON.stringify(JSON.parse(text), null, 2);
    took.value = `Took ${Math.round(performance.now() - start)} ms.`;
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e);
  } finally {
    running.value = false;
    emit("running", false);
  }
}

// A tool with no input has no box to run from: the row's Run calls this.
defineExpose({ run });
</script>

<template>
  <form class="mt-2 grid gap-2" :aria-label="`Run ${tool.name}`" @submit.prevent="run">
    <label v-if="hasInput" class="grid gap-1 text-small text-muted-foreground">
      Input
      <textarea
        v-model="input"
        :aria-label="`Input for ${tool.name}`"
        rows="4"
        spellcheck="false"
        class="rounded-md border bg-muted p-2 font-mono text-small text-foreground outline-none focus:ring-2 focus:ring-ring/40"
      ></textarea>
    </label>
    <div v-if="hasInput || asks !== ''" class="flex items-center gap-3">
      <Button v-if="hasInput" type="submit" size="sm" :disabled="running">{{ running ? "Running…" : "Run" }}</Button>
      <span v-if="asks !== ''" class="asks text-small text-muted-foreground">
        {{ asks ? "The agent asks you before this call." : "The agent makes this call without asking." }}
      </span>
    </div>
    <p v-if="problem" class="whitespace-pre-wrap text-destructive" role="alert">{{ problem }}</p>
    <template v-if="result">
      <pre :aria-label="`Result of ${tool.name}`" class="max-h-96 overflow-auto rounded-md border bg-muted p-2 font-mono text-small">{{
        result
      }}</pre>
      <p class="took text-small text-muted-foreground">{{ took }}</p>
    </template>
  </form>
</template>
