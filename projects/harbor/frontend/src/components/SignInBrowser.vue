<script setup lang="ts">
import { Button } from "@ui/components/button";
import { computed, ref, watch } from "vue";
import { LiveView, useTab } from "@desktop";
import { Tools } from "@backend";
import BrowserTab from "./BrowserTab.vue";
import SignInPrompt from "./SignInPrompt.vue";
import type { Prompt } from "../prompt";

// A service's tab in Harbor's browser, as a small browser window: the tab,
// and the tabs it opened, such as a sign-in's pop-up; an address bar, for a
// sign-in link a site emailed you; back, forward, and reload. You sign in,
// confirm it's you before a tool runs, or look around (browse). Harbor
// carries on once you are signed in; looking around ends when you are done.
const props = defineProps<{ service: string; tab: string; heading: string; browse: boolean; prompt: Prompt | null }>();

const root = useTab(() => props.tab);
// The tab shown: a tab the page opens comes to the front, as in a browser,
// and when it closes the newest one left does.
const shown = ref(props.tab);
watch(
  () => root.opened,
  (opened, before) => {
    const added = opened.filter((id) => !before?.includes(id));
    if (added.length) {
      shown.value = added[added.length - 1];
    } else if (shown.value !== props.tab && !opened.includes(shown.value)) {
      shown.value = opened[opened.length - 1] ?? props.tab;
    }
  },
);
const tabs = computed(() => [props.tab, ...root.opened]);
const current = useTab(() => shown.value);

// The address bar shows where the tab is, except while you type in it.
const address = ref("");
const typing = ref(false);
watch(
  () => current.url,
  (url) => {
    if (!typing.value) {
      address.value = url;
    }
  },
  { immediate: true },
);
const problem = ref("");
const report = (e: unknown) => (problem.value = e instanceof Error ? e.message : String(e));

async function go() {
  problem.value = "";
  let to = address.value.trim();
  if (!to) {
    return;
  }
  if (!/^[a-z][a-z0-9+.-]*:/i.test(to)) {
    to = `https://${to}`;
  }
  typing.value = false;
  await current.navigate(to).catch(report);
}

async function close() {
  await Tools.CloseView(props.service);
}
</script>

<template>
  <div class="backdrop">
    <section class="panel" role="dialog" :aria-label="heading">
      <header>
        <h2>{{ heading }}</h2>
        <Button size="sm" variant="outline" type="button" @click="close">{{ browse ? "Done" : "Cancel" }}</Button>
      </header>
      <p v-if="!browse" class="meta">Harbor carries on once you have signed in.</p>
      <ul v-if="tabs.length > 1" class="tabs" aria-label="Tabs">
        <BrowserTab
          v-for="id in tabs"
          :id="id"
          :key="id"
          :current="id === shown"
          :closable="id !== tab"
          @select="shown = id"
        />
      </ul>
      <form class="bar" aria-label="Address bar" @submit.prevent="go">
        <Button size="sm" variant="outline" type="button" aria-label="Back" :disabled="!current.canGoBack" @click="current.back().catch(report)">
          ←
        </Button>
        <Button size="sm" variant="outline"
          type="button"
          aria-label="Forward"
          :disabled="!current.canGoForward"
          @click="current.forward().catch(report)"
        >
          →
        </Button>
        <Button size="sm" variant="outline" type="button" aria-label="Reload" @click="current.reload().catch(report)">↻</Button>
        <input
          v-model="address"
          aria-label="Address"
          autocomplete="off"
          spellcheck="false"
          @focus="typing = true"
          @blur="typing = false"
        />
      </form>
      <LiveView :tab="shown" class="view" @problem="report">
        <SignInPrompt v-if="prompt" :key="prompt.id" :service="service" :prompt="prompt" @problem="report" />
      </LiveView>
      <p v-if="problem" class="problem" role="alert">{{ problem }}</p>
    </section>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed;
  inset: 0;
  display: grid;
  place-items: center;
  background: var(--overlay);
  z-index: 10;
}
.panel {
  display: flex;
  flex-direction: column;
  width: min(72rem, 100vw - 2rem);
  height: calc(100vh - 2rem);
  padding: var(--spacing-card);
  background: var(--background);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: calc(var(--spacing) * 4);
}
h2 {
  margin: 0;
  font-size: var(--text-title);
  line-height: var(--text-title--line-height);
}
.meta {
  margin: calc(var(--spacing) * 1) 0 0;
  color: var(--muted-foreground);
}
.tabs {
  display: flex;
  gap: calc(var(--spacing) * 1);
  margin: calc(var(--spacing) * 3) 0 0;
  padding: 0;
  list-style: none;
  overflow-x: auto;
}
.bar {
  display: flex;
  gap: calc(var(--spacing) * 1);
  margin: calc(var(--spacing) * 2) 0;
}
.bar input {
  flex: 1;
  min-width: 0;
  font: inherit;
  padding: calc(var(--spacing) * 1.5) calc(var(--spacing) * 2);
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--background);
  color: var(--foreground);
}
.view {
  flex: 1;
  min-height: 0;
  border: 1px solid var(--border);
  background: white;
}
.problem {
  margin: calc(var(--spacing) * 2) 0 0;
  color: var(--destructive);
}
</style>
