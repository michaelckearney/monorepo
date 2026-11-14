<script setup lang="ts">
import { onMounted, ref } from "vue";
import { AgentTools } from "@backend";
import { Button } from "@ui/components/button";
import AgentToolRunner, { type AgentTool } from "./AgentToolRunner.vue";
import PageHeader from "./PageHeader.vue";

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

// The tools whose Run panel is open.
const opened = ref(new Set<string>());
function toggle(name: string) {
  const next = new Set(opened.value);
  if (!next.delete(name)) {
    next.add(name);
  }
  opened.value = next;
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
            <Button
              size="xs"
              variant="outline"
              class="ml-auto"
              :aria-expanded="opened.has(tool.name)"
              :aria-label="`Run ${tool.name}`"
              @click="toggle(tool.name)"
            >
              Run
            </Button>
          </div>
          <p class="mt-1 text-muted-foreground">{{ tool.description }}</p>
          <AgentToolRunner v-if="opened.has(tool.name)" :tool="tool" />
        </li>
      </ul>
    </div>
  </div>
</template>
