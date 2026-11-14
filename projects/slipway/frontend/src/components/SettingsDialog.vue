<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Github, Palette, Server } from "lucide-vue-next";
import { Settings } from "@backend";
import type { Status } from "@backend";
import { Button } from "@ui/components/button";
import { Input } from "@ui/components/input";
import SettingsDialog from "@ui/components/SettingsDialog.vue";
import SettingsPane from "@ui/components/SettingsPane.vue";
import ThemePicker from "@ui/components/ThemePicker.vue";
import { builtInThemes } from "@ui/themes";
import { message } from "../poll";
import { setMode, setTheme, usePreferences } from "../preferences";

// Slipway's Settings: the server whose bots it shows, the GitHub token it
// reads and replies with, as you, and how it looks. open is the part shown,
// or null when Settings is closed. saved says a setting changed, so what
// it reads is read again.
type Section = "server" | "github" | "appearance";
const props = defineProps<{ open: Section | null; status: Status | null; login: string; loginError: string }>();
const emit = defineEmits<{ "update:open": [section: Section | null]; saved: [] }>();

const preferences = usePreferences();
const serverAlert = computed(() => (props.status && !props.status.ok ? "The server can't be reached" : undefined));
const githubAlert = computed(() => (props.loginError ? "GitHub has no token it takes" : undefined));
const sections = computed(() => [
  { value: "server", label: "Server", icon: Server, alert: serverAlert.value },
  { value: "github", label: "GitHub", icon: Github, alert: githubAlert.value },
  { value: "appearance", label: "Appearance", icon: Palette },
]);

// The server's address, and a new token, which the app sets but never
// reads back: the field starts empty each time.
const server = ref("");
const token = ref("");
const error = ref("");
watch(
  () => props.open,
  async (open, was) => {
    if (open && !was) {
      server.value = await Settings.Server();
      token.value = "";
      error.value = "";
    }
  },
  { immediate: true },
);

async function save(what: () => Promise<void>) {
  error.value = "";
  try {
    await what();
    emit("saved");
  } catch (e) {
    error.value = message(e);
  }
}
const saveServer = () => save(() => Settings.SetServer(server.value));
const saveToken = () =>
  save(async () => {
    await Settings.SetToken(token.value);
    token.value = "";
  });
</script>

<template>
  <SettingsDialog :sections="sections" :open="open" @update:open="(s) => emit('update:open', s as Section | null)">
    <template #server>
      <SettingsPane title="Server">
        <form class="grid max-w-lg gap-2" @submit.prevent="saveServer">
          <label for="server" class="font-medium">Address</label>
          <div class="flex gap-2">
            <Input id="server" v-model="server" placeholder="http://server:7420" autofocus />
            <Button type="submit" variant="outline">Save</Button>
          </div>
          <p class="text-small text-muted-foreground">
            The harness API of the server the bots run on. It answers on the tailnet, to the Tailscale logins its harness.yaml allows.
          </p>
        </form>
        <p v-if="status" class="flex items-center gap-2">
          <span class="size-1.5 shrink-0 rounded-full" :class="status.ok ? 'bg-success' : 'bg-destructive'" aria-hidden="true"></span>
          <span v-if="status.ok">Connected · harness {{ status.harness }}</span>
          <span v-else class="text-muted-foreground">{{ status.error }}</span>
        </p>
        <p v-if="error" class="text-small text-destructive">{{ error }}</p>
      </SettingsPane>
    </template>

    <template #github>
      <SettingsPane title="GitHub">
        <form class="grid max-w-lg gap-2" @submit.prevent="saveToken">
          <label for="token" class="font-medium">Token</label>
          <div class="flex gap-2">
            <Input
              id="token"
              v-model="token"
              type="password"
              autocomplete="off"
              :placeholder="login ? `Signed in as ${login}` : 'github_pat_…'"
              aria-label="GitHub token"
            />
            <Button type="submit" variant="outline" :disabled="!token.trim()">Save</Button>
          </div>
          <p class="text-small text-muted-foreground">
            A fine-grained token for the monorepos, with read and write on issues, and read on contents, pull requests, checks, and commit
            statuses. Slipway reads and replies with it, as you.
          </p>
        </form>
        <p class="flex items-center gap-2">
          <span class="size-1.5 shrink-0 rounded-full" :class="login ? 'bg-success' : 'bg-destructive'" aria-hidden="true"></span>
          <span v-if="login">Signed in as {{ login }}</span>
          <span v-else class="text-muted-foreground">{{ loginError || "Checking…" }}</span>
        </p>
        <p v-if="error" class="text-small text-destructive">{{ error }}</p>
      </SettingsPane>
    </template>

    <template #appearance>
      <SettingsPane title="Appearance">
        <div class="grid max-w-xs gap-4">
          <ThemePicker
            :themes="builtInThemes"
            :current="preferences.theme.name"
            :mode="preferences.mode"
            @mode="setMode"
            @use="setTheme"
          />
        </div>
      </SettingsPane>
    </template>
  </SettingsDialog>
</template>
