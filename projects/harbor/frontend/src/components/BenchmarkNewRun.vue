<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { X } from "lucide-vue-next";
import { Benchmarks } from "@backend";
import { Button } from "@ui/components/button";
import { Input } from "@ui/components/input";
import { devView, openSettings } from "../navigation";
import { useProviders } from "../providerStatus";
import IconButton from "@ui/components/IconButton.vue";

// A new run's form: the scenarios, the models to benchmark, how many times
// each, and the model that grades. Each model is provider/model, such as
// openai/gpt-5, on that provider as it is set up in Settings, under Providers.
const emit = defineEmits<{ started: [id: number]; cancel: [] }>();

const scenarios = ref<{ name: string; prompt: string }[]>([]);
const chosen = ref(new Set<string>());
const failure = ref("");
onMounted(async () => {
  try {
    scenarios.value = (await Benchmarks.Scenarios()) ?? [];
    chosen.value = new Set(scenarios.value.map((s) => s.name));
  } catch (e) {
    failure.value = e instanceof Error ? e.message : String(e);
  }
});
function toggle(name: string) {
  const next = new Set(chosen.value);
  if (!next.delete(name)) {
    next.add(name);
  }
  chosen.value = next;
}

// The model the agent uses, as provider/model, to start from.
const { providers, inUse, values, status } = useProviders();
const current = computed(() => {
  const model =
    values.value.find((v) => v.provider === inUse.value && v.key === "model")?.value ??
    providers.value.find((p) => p.name === inUse.value)?.settings.find((s) => s.key === "model")?.default;
  return model ? `${inUse.value}/${model}` : "";
});
const models = ref<string[]>([""]);
const grader = ref("");
// Once, so it does not undo what you type.
let filled = false;
watch(
  current,
  (m) => {
    if (m && !filled) {
      filled = true;
      models.value = [m];
      grader.value = m;
    }
  },
  { immediate: true },
);
const repeat = ref(1);

const starting = ref(false);
async function start() {
  starting.value = true;
  failure.value = "";
  try {
    const names = scenarios.value.filter((s) => chosen.value.has(s.name)).map((s) => s.name);
    const ms = models.value.map((m) => m.trim()).filter((m) => m);
    emit("started", await Benchmarks.Start(names, ms, grader.value.trim(), Number(repeat.value)));
  } catch (e) {
    failure.value = e instanceof Error ? e.message : String(e);
  } finally {
    starting.value = false;
  }
}

function setUpProvider() {
  devView.value = "harbor";
  openSettings("providers");
}
</script>

<template>
  <form class="grid max-w-3xl gap-4 p-page" aria-label="New run" @submit.prevent="start">
    <p v-if="status" class="flex items-center gap-3 text-muted-foreground" role="status">
      <span class="flex-1">{{ status }}</span>
      <Button type="button" size="sm" variant="outline" @click="setUpProvider">Set up a provider</Button>
    </p>

    <fieldset class="grid gap-1">
      <legend class="mb-1 font-medium">Scenarios</legend>
      <label v-for="s in scenarios" :key="s.name" class="flex cursor-pointer gap-2 rounded-md px-1 py-1 hover:bg-accent">
        <input type="checkbox" class="mt-0.5" :checked="chosen.has(s.name)" @change="toggle(s.name)" />
        <span class="grid min-w-0">
          <span class="font-mono text-body">{{ s.name }}</span>
          <span class="truncate text-small text-muted-foreground">{{ s.prompt }}</span>
        </span>
      </label>
    </fieldset>

    <fieldset class="grid gap-2">
      <legend class="mb-1 font-medium">Models to benchmark</legend>
      <p class="text-small text-muted-foreground">
        Write each as provider/model, such as openai/gpt-5 or openai-compatible/Qwen/Qwen3-8B, on a provider set up in
        Settings. The grader can be on another provider.
      </p>
      <div v-for="(_, i) in models" :key="i" class="flex max-w-md gap-2">
        <Input v-model="models[i]" :aria-label="`Model ${i + 1}`" placeholder="openai/gpt-5" />
        <IconButton
          v-if="models.length > 1"
          type="button"
          size="icon"
          label="Remove"
          :name="`Remove model ${i + 1}`"
          @click="models.splice(i, 1)"
        >
          <X />
        </IconButton>
      </div>
      <Button type="button" size="sm" variant="outline" class="justify-self-start" @click="models.push('')">
        Add a model
      </Button>
    </fieldset>

    <div class="flex flex-wrap gap-4">
      <label class="grid gap-1">
        <span class="font-medium">Times each</span>
        <Input v-model.number="repeat" type="number" min="1" max="20" class="w-24" />
      </label>
      <label class="grid gap-1">
        <span class="font-medium">Grader</span>
        <Input v-model="grader" class="w-80" placeholder="openai/gpt-5" />
      </label>
    </div>

    <p v-if="failure" class="whitespace-pre-wrap text-destructive" role="alert">{{ failure }}</p>
    <div class="flex gap-2">
      <Button type="submit" :disabled="starting || !chosen.size">Start the run</Button>
      <Button type="button" variant="outline" @click="emit('cancel')">Cancel</Button>
    </div>
  </form>
</template>
