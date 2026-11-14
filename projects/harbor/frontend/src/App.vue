<script setup lang="ts">
import { computed, defineAsyncComponent, watch } from "vue";
import { sql, useQuery } from "@desktop";
import { devView, open, place } from "./navigation";
import { applyTheme } from "./themes";
import Sidebar from "./components/Sidebar.vue";
import AgentPanel from "./components/AgentPanel.vue";
import ServicePage from "./components/ServicePage.vue";
import ServicesPage from "./components/ServicesPage.vue";
import DashboardPage from "./components/DashboardPage.vue";
import DocumentEditor from "./components/DocumentEditor.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import SignInBrowser from "./components/SignInBrowser.vue";
import Welcome from "./components/Welcome.vue";
import type { Prompt } from "./prompt";

applyTheme();

// What only a dev build has. DESKTOP_MODE is fixed when the app is built, so
// in any other mode this is null and the bundle leaves the components out.
const dev =
  DESKTOP_MODE === "dev"
    ? {
        DevBar: defineAsyncComponent(() => import("./components/DevBar.vue")),
        AgentToolsPage: defineAsyncComponent(() => import("./components/AgentToolsPage.vue")),
        BenchmarksPage: defineAsyncComponent(() => import("./components/BenchmarksPage.vue")),
        ComponentsPage: defineAsyncComponent(() => import("./components/ComponentsPage.vue")),
      }
    : null;

// A live view of a service's tab, from any page, a tool call, or an agent,
// shows over the page until it ends.
const views = useQuery<{ service: string; tab: string; heading: string; browse: number; prompt: string | null }>(
  () => sql`SELECT service, tab, heading, browse, prompt FROM live_views ORDER BY started LIMIT 1`,
);
const prompt = computed(() => (views.value[0]?.prompt ? (JSON.parse(views.value[0].prompt) as Prompt) : null));

// With nowhere chosen, as when the window opens or a dashboard is deleted,
// the window shows the dashboard you last opened, or your first.
const dashboards = useQuery<{ id: number }>(() => sql`SELECT id FROM dashboards ORDER BY name COLLATE NOCASE`);
const last = useQuery<{ value: string }>(() => sql`SELECT value FROM settings WHERE key = 'last_dashboard'`);
watch(
  [place, dashboards, last],
  () => {
    if (place.value || dashboards.value.length === 0) {
      return;
    }
    const id = Number(last.value[0]?.value);
    open("dashboard", dashboards.value.some((d) => d.id === id) ? id : dashboards.value[0].id);
  },
  { immediate: true },
);
</script>

<template>
  <div class="flex h-full flex-col overflow-hidden">
    <component :is="dev.DevBar" v-if="dev" />
    <main v-if="dev && devView === 'tools'" class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <component :is="dev.AgentToolsPage" />
    </main>
    <main v-if="dev && devView === 'benchmarks'" class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <component :is="dev.BenchmarksPage" />
    </main>
    <main v-if="dev && devView === 'components'" class="flex min-h-0 flex-1 flex-col overflow-hidden">
      <component :is="dev.ComponentsPage" />
    </main>
    <!-- Hidden rather than removed under a developer tool, so an unsaved edit is still there on the way back. -->
    <div v-show="devView === 'harbor'" class="flex min-h-0 flex-1 overflow-hidden">
      <Sidebar />
      <main class="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <!-- The key starts each place fresh, so an editor never keeps another document's text. -->
        <template v-if="place">
          <ServicesPage v-if="place.kind === 'services'" />
          <DocumentEditor
            v-else-if="place.editing"
            :key="`edit-${place.kind}-${place.id}`"
            :kind="place.kind"
            :id="place.id"
          />
          <ServicePage v-else-if="place.kind === 'service'" :key="`service-${place.id}`" :id="place.id" />
          <DashboardPage v-else :key="`dashboard-${place.id}`" :id="place.id" />
        </template>
        <Welcome v-else />
      </main>
      <AgentPanel />
    </div>
    <!-- Settings belongs to Harbor, not a developer tool, and a sign-in's live view needs you first, so it steps aside for both. -->
    <SettingsDialog :hidden="!!views[0] || devView !== 'harbor'" />
    <SignInBrowser
      v-if="views[0]"
      :key="views[0].service"
      :service="views[0].service"
      :tab="views[0].tab"
      :heading="views[0].heading"
      :browse="views[0].browse === 1"
      :prompt="prompt"
    />
  </div>
</template>
