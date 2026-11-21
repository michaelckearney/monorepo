<script setup lang="ts">
import { computed, ref } from "vue";
import { Tools } from "@backend";
import { exampleInput, takesInput, type Tool } from "../manifest";
import { addFile, download, filesIn, type FileRef } from "../files";
import { Button } from "@ui/components/button";
import { signInNeeded } from "../kit/runtime";

// Runs one tool and shows what it returned. A tool that takes an input runs
// from the Run beside the JSON box; one that takes none has no box and is run
// by the row's own Run, through the exposed run. So each tool has one Run.
// An input that is a file has a picker, which puts the chosen file's id in
// the JSON; a file in the output can be saved.
const props = defineProps<{ service: string; tool: Tool }>();
const emit = defineEmits<{ running: [boolean] }>();

const hasInput = takesInput(props.tool.inputSchema);
const input = ref(exampleInput(props.tool.inputSchema));
const output = ref("");
const files = ref<FileRef[]>([]);
const fileInputs = computed(() => {
  const properties = (props.tool.inputSchema.properties ?? {}) as Record<string, { format?: string }>;
  return Object.keys(properties).filter((name) => properties[name].format === "file");
});

async function attach(name: string, e: Event) {
  const chosen = (e.target as HTMLInputElement).files?.[0];
  if (!chosen) {
    return;
  }
  problem.value = "";
  needsYou.value = false;
  try {
    const value = JSON.parse(input.value);
    value[name] = await addFile(chosen);
    input.value = JSON.stringify(value, null, 2);
  } catch (err) {
    problem.value = err instanceof Error ? err.message : String(err);
  }
}
const problem = ref("");
// Whether what stopped the run is a sign-in that waits on you: the app says
// that in its needs-you amber, not the failure red.
const needsYou = ref(false);
const running = ref(false);

async function run() {
  if (running.value) {
    return;
  }
  output.value = "";
  files.value = [];
  problem.value = "";
  needsYou.value = false;
  try {
    JSON.parse(input.value);
  } catch {
    problem.value = "The input is not JSON.";
    return;
  }
  running.value = true;
  emit("running", true);
  try {
    const result = await Tools.Call(props.service, props.tool.name, input.value);
    const value = JSON.parse(result);
    output.value = JSON.stringify(value, null, 2);
    files.value = filesIn(value);
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e);
    needsYou.value = signInNeeded(e) !== null;
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
    <label v-for="name in fileInputs" :key="name" class="grid gap-1 text-small text-muted-foreground">
      File for {{ name }}
      <input type="file" :aria-label="`File for ${name}`" @change="attach(name, $event)" />
    </label>
    <div v-if="hasInput">
      <Button type="submit" size="sm" :disabled="running">{{ running ? "Running…" : "Run" }}</Button>
    </div>
    <p v-if="problem" class="whitespace-pre-wrap" :class="needsYou ? 'need' : 'text-destructive'" role="alert">{{ problem }}</p>
    <pre
      v-if="output"
      :aria-label="`Output of ${tool.name}`"
      class="max-h-96 overflow-auto rounded-md border bg-muted p-2 font-mono text-small"
      >{{ output }}</pre
    >
    <div v-if="files.length" class="flex flex-wrap gap-2">
      <Button v-for="f in files" :key="f.file" type="button" size="sm" variant="outline" @click="download(f)">
        Save {{ f.name || "file" }}
      </Button>
    </div>
  </form>
</template>

<style scoped>
/* A run that waits on your sign-in is the state the app already shows in its
   needs-you amber, as the Signed out dot does; only a real failure is red. */
.need {
  color: var(--warning);
}
</style>
