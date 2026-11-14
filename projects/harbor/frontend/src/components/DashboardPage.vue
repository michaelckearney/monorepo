<script setup lang="ts">
import { computed, onErrorCaptured, onUnmounted, provide, ref, shallowRef, watch, type Component } from "vue";
import { MessageSquare, Pencil, RefreshCw } from "lucide-vue-next";
import { sql, useQuery } from "@desktop";
import { Documents } from "@backend";
import { Button } from "@ui/components/button";
import { harbor, type SignInNeeded } from "../dashboards";
import { viewKey } from "../kit/grid";
import { WriteFailed } from "../kit/runtime";
import { askAgent, close, open } from "../navigation";
import { signIn as signInTo, signingIn, signInProblem } from "../services";
import DeleteButton from "./DeleteButton.vue";
import IconButton from "@ui/components/IconButton.vue";
import PageHeader from "./PageHeader.vue";

const props = defineProps<{ id: number }>();

// updated_at changes on every save, including the agent's, so the
// dashboard is compiled again and shown as it is now.
const rows = useQuery<{ name: string; updated_at: string }>(
  () => sql`SELECT name, updated_at FROM dashboards WHERE id = ${props.id}`,
);

const view = shallowRef<Component | null>(null);
// What the dashboard scrolls in: its Grids fit their rows to it.
const scroller = ref<HTMLElement | null>(null);
provide(viewKey, scroller);
const problem = ref("");

// The logins that writes found you are not through. The page offers each,
// rather than each call showing you the login page and waiting. A read's
// widget shows its own. Once you sign in, or refresh, the dashboard's data
// loads again, and what it shows stays until it has.
const needed = ref<SignInNeeded[]>([]);
// The sign-ins a card shows, which the page leaves to it: a card's other
// calls to the service can fail after it has taken one back.
const inCards = new Set<string>();
const key = (n: SignInNeeded) => `${n.service}/${n.tool ?? ""}`;
function refresh() {
  needed.value = [];
  inCards.clear();
  dashboard.refresh();
}
const dashboard = harbor({
  dashboard: props.id,
  needsSignIn(n) {
    if (!inCards.has(key(n)) && !needed.value.some((m) => key(m) === key(n))) {
      needed.value = [...needed.value, n];
    }
  },
  shown(n) {
    inCards.add(key(n));
    needed.value = needed.value.filter((m) => key(m) !== key(n));
  },
});

async function signIn(n: SignInNeeded) {
  if (await signInTo(n.service, n.tool ?? "")) {
    refresh();
  }
}
const style = document.createElement("style");
document.head.append(style);
onUnmounted(() => {
  style.remove();
  dashboard.dispose();
});

watch(
  () => rows.value[0]?.updated_at,
  async (updated) => {
    if (!updated) {
      return;
    }
    try {
      const compiled = await Documents.CompiledDashboard(props.id);
      style.textContent = compiled.css;
      view.value = dashboard.load(compiled.js, compiled.uses ?? []);
      problem.value = "";
    } catch (e) {
      view.value = null;
      problem.value = e instanceof Error ? e.message : String(e);
    }
  },
  { immediate: true },
);

// An error while it runs stays in the dashboard, and says what it was. A
// write that failed says why in its widget already.
onErrorCaptured((e) => {
  if (e instanceof WriteFailed) {
    return false;
  }
  view.value = null;
  problem.value = `The dashboard stopped with an error: ${e instanceof Error ? e.message : String(e)}`;
  return false;
});

const updated = computed(() => {
  const at = rows.value[0]?.updated_at;
  if (!at) {
    return "";
  }
  const d = new Date(at.replace(" ", "T") + "Z");
  return `Updated ${d.toLocaleDateString(undefined, { month: "short", day: "numeric" })}, ${d.toLocaleTimeString(undefined, { hour: "numeric", minute: "2-digit" })}`;
});

async function remove() {
  await Documents.DeleteDashboard(props.id);
  close();
}
</script>

<template>
  <template v-if="rows[0]">
    <PageHeader :title="rows[0].name" :note="updated">
      <IconButton label="Refresh" @click="refresh()"><RefreshCw /></IconButton>
      <IconButton label="Edit" @click="open('dashboard', id, true)"><Pencil /></IconButton>
      <DeleteButton :what="rows[0].name" size="icon" @confirm="remove" />
    </PageHeader>
    <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto p-page">
      <section
        v-if="needed.length"
        class="mb-4 grid gap-2 rounded-lg border border-warning/40 bg-warning/10 px-3 py-2"
        aria-label="Sign-ins needed"
      >
        <p v-for="n in needed" :key="`${n.service}/${n.tool ?? ''}`" class="flex items-center justify-between gap-4">
          <span>{{ n.heading }}</span>
          <Button size="sm" :disabled="signingIn !== ''" @click="signIn(n)">{{ n.tool ? "Confirm" : "Sign in" }}</Button>
        </p>
        <p v-if="signInProblem" class="text-destructive" role="alert">{{ signInProblem }}</p>
      </section>
      <div v-if="problem" class="grid justify-items-start gap-2">
        <p class="whitespace-pre-wrap text-destructive" role="alert">{{ problem }}</p>
        <Button size="sm" variant="outline" @click="askAgent(`Fix this dashboard. It shows this error:\n\n${problem}`)">
          <MessageSquare />Ask the agent to fix it
        </Button>
      </div>
      <section v-else-if="view" class="dashboard" aria-label="Dashboard">
        <component :is="view" :key="rows[0].updated_at" />
      </section>
    </div>
  </template>
</template>
