<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { Eye, EyeOff, RefreshCw } from "lucide-vue-next";
import { MCP } from "@backend";
import { Button } from "@ui/components/button";
import CopyButton from "./CopyButton.vue";
import IconButton from "@ui/components/IconButton.vue";
import SettingsPane from "@ui/components/SettingsPane.vue";

// How to use Harbor from an agent outside it, such as Claude Code, through
// Harbor's MCP server. Harbor keeps the address and the token when it
// restarts; a new token shuts out every agent given the old one.
const url = ref("");
const token = ref("");
const problem = ref("");
// The token lets an agent use your services, so it shows only when you ask.
const shown = ref(false);
const renewing = ref(false);

onMounted(async () => {
  try {
    ({ url: url.value, token: token.value } = await MCP.Connection());
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e);
  }
});

async function renew() {
  renewing.value = false;
  try {
    ({ url: url.value, token: token.value } = await MCP.NewToken());
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e);
  }
}

const mask = "•".repeat(12);
const command = (t: string) => `claude mcp add --transport http harbor ${url.value} --header "Authorization: Bearer ${t}"`;
const field = "min-w-0 flex-1 rounded-md border bg-muted px-3 py-1.5 font-mono text-small";
const visible = computed(() => (shown.value ? token.value : mask));
</script>

<template>
  <SettingsPane title="Connections">
    <p v-if="problem" class="text-destructive" role="alert">Harbor's MCP server did not start: {{ problem }}</p>
    <template v-else-if="url">
      <div class="grid gap-1.5">
        <span class="font-medium">Address</span>
        <div class="flex items-center gap-1">
          <code class="address truncate" :class="field">{{ url }}</code>
          <CopyButton :text="url" label="Copy the address" />
        </div>
      </div>

      <div class="grid gap-1.5">
        <span class="font-medium">Token</span>
        <div class="flex items-center gap-1">
          <code class="token truncate" :class="field">{{ visible }}</code>
          <IconButton :label="shown ? 'Hide' : 'Show'" @click="shown = !shown">
            <EyeOff v-if="shown" /><Eye v-else />
          </IconButton>
          <CopyButton :text="token" label="Copy the token" />
          <span v-if="renewing" class="inline-flex items-center gap-1.5" role="group" aria-label="Replace the token?">
            <Button size="xs" variant="destructive" @click="renew">Replace</Button>
            <Button size="xs" variant="outline" @click="renewing = false">Keep</Button>
          </span>
          <IconButton v-else label="New token" @click="renewing = true"><RefreshCw /></IconButton>
        </div>
      </div>

      <div class="grid gap-1.5">
        <span class="font-medium">Claude Code</span>
        <div class="flex items-start gap-1">
          <code class="command break-words" :class="field">{{ command(visible) }}</code>
          <CopyButton :text="command(token)" label="Copy the command" />
        </div>
      </div>
    </template>
  </SettingsPane>
</template>
