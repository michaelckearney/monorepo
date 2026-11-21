<script setup lang="ts">
import { onMounted, ref } from "vue";
import { AgentTools } from "@backend";
import { Button } from "@ui/components/button";
import AgentToolRunner, { type AgentTool } from "./AgentToolRunner.vue";
import EffectBadge from "./EffectBadge.vue";
import PageHeader from "./PageHeader.vue";
import { takesInput } from "../manifest";

// The tools Harbor's agent has, from Harbor's MCP server, to try one at a time.
const tools = ref<AgentTool[]>([]);
const failure = ref("");

onMounted(async () => {
  try {
    tools.value = ((await AgentTools.List()) ?? []).map((t) => ({ ...t, inputSchema: JSON.parse(t.inputSchema) }));
  } catch (e) {
    failure.value = e instanceof Error ? e.message : String(e);
  }
});

// A tool with an input runs from the Run beside its box; one with no input
// runs from the row's Run, which starts the runner through its ref.
const runners = new Map<string, { run: () => void }>();
const running = ref(new Set<string>());
function setRunner(name: string, el: unknown) {
  if (el) {
    runners.set(name, el as { run: () => void });
  } else {
    runners.delete(name);
  }
}
function setRunning(name: string, is: boolean) {
  const next = new Set(running.value);
  if (is) {
    next.add(name);
  } else {
    next.delete(name);
  }
  running.value = next;
}
</script>

<template>
  <PageHeader title="Tools" />
  <div class="min-h-0 flex-1 overflow-y-auto">
    <div class="max-w-3xl p-page">
      <p class="mb-4 text-muted-foreground">
        The tools Harbor's agent has, from Harbor's MCP server. A run returns what the agent would be given. It does not
        ask first, even where the agent would.
      </p>
      <p v-if="failure" class="whitespace-pre-wrap text-destructive" role="alert">{{ failure }}</p>
      <ul class="divide-y border-y">
        <li v-for="tool in tools" :key="tool.name" class="py-3">
          <div class="flex items-center gap-2">
            <code class="font-mono text-body">{{ tool.name }}</code>
            <EffectBadge :tool="tool" />
            <Button
              v-if="!takesInput(tool.inputSchema)"
              size="xs"
              variant="outline"
              class="ml-auto"
              :disabled="running.has(tool.name)"
              :aria-label="`Run ${tool.name}`"
              @click="runners.get(tool.name)?.run()"
            >
              {{ running.has(tool.name) ? "Running…" : "Run" }}
            </Button>
          </div>
          <p class="mt-1 text-muted-foreground">{{ tool.description }}</p>
          <AgentToolRunner
            :ref="(el) => setRunner(tool.name, el)"
            :tool="tool"
            @running="(is) => setRunning(tool.name, is)"
          />
        </li>
      </ul>
    </div>
  </div>
</template>
