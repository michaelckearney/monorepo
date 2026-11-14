<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, shallowRef, type Component } from "vue";
import { Documents } from "@backend";
import type { KitComponent } from "@backend";
import { Badge } from "@ui/components/badge";
import { Input } from "@ui/components/input";
import { harbor } from "../dashboards";
import PageHeader from "./PageHeader.vue";

// The Components page, in dev mode: every component a dashboard can import from
// "harbor", with what it says of itself and its examples, shown as a
// dashboard shows them, in the theme you chose. The place to review the kit.

const components = ref<KitComponent[]>([]);
const views = shallowRef<Record<string, Component[]>>({});
const problem = ref("");
const query = ref("");
const chosen = ref("Widget");

// The examples run as a dashboard's code does, with a "harbor" of their own.
const examples = harbor({ dashboard: 0, needsSignIn() {}, shown() {} });
const style = document.createElement("style");
document.head.append(style);
onUnmounted(() => style.remove());

onMounted(async () => {
  try {
    // Go's empty lists come as null.
    components.value = ((await Documents.Kit()) ?? []).map((c) => ({ ...c, keywords: c.keywords ?? [], examples: c.examples ?? [] }));
    views.value = Object.fromEntries(components.value.map((c) => [c.name, c.examples!.map((e) => examples.load(e.code.js, []))]));
    style.textContent = components.value.flatMap((c) => c.examples!.map((e) => e.code.css)).join("\n");
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e);
  }
});

const kinds = [
  { kind: "widget", title: "Widgets" },
  { kind: "primitive", title: "Primitives" },
  { kind: "layout", title: "Layout" },
  { kind: "control", title: "Controls" },
];
const shown = computed(() => {
  const q = query.value.trim().toLowerCase();
  const found = components.value.filter(
    (c) => !q || [c.name, c.description, ...(c.keywords ?? [])].some((t) => t.toLowerCase().includes(q)),
  );
  return kinds.map((k) => ({ ...k, components: found.filter((c) => c.kind === k.kind) })).filter((k) => k.components.length);
});
const current = computed(() => components.value.find((c) => c.name === chosen.value));
</script>

<template>
  <PageHeader title="Components" :note="components.length ? `${components.length} components` : ''" />
  <p v-if="problem" class="p-page text-destructive" role="alert">{{ problem }}</p>
  <div v-else class="grid min-h-0 flex-1 grid-cols-[15rem_minmax(0,1fr)] overflow-hidden">
    <nav class="flex min-h-0 flex-col gap-3 overflow-y-auto border-r bg-sidebar p-3" aria-label="Components">
      <Input v-model="query" placeholder="Filter" aria-label="Filter components" />
      <section v-for="k in shown" :key="k.kind" class="grid gap-0.5">
        <h2 class="px-2 pb-1 text-small font-medium text-muted-foreground">{{ k.title }}</h2>
        <button
          v-for="c in k.components"
          :key="c.name"
          type="button"
          class="truncate rounded-md px-2 py-1 text-left text-sidebar-foreground hover:bg-sidebar-accent"
          :class="c.name === chosen && 'bg-sidebar-accent font-medium text-sidebar-accent-foreground'"
          :aria-current="c.name === chosen ? 'page' : undefined"
          @click="chosen = c.name"
        >
          {{ c.name }}
        </button>
      </section>
    </nav>
    <main v-if="current" class="min-h-0 overflow-y-auto p-page" :aria-label="current.name">
      <div class="mx-auto grid max-w-4xl gap-6">
        <header class="grid gap-2">
          <div class="flex items-center gap-2">
            <h1 class="text-heading font-semibold">{{ current.name }}</h1>
            <Badge variant="secondary">{{ current.kind }}</Badge>
          </div>
          <p class="text-muted-foreground">{{ current.description }}</p>
          <p class="flex flex-wrap gap-1">
            <span v-for="k in current.keywords" :key="k" class="rounded-full bg-muted px-2 text-small text-muted-foreground">{{ k }}</span>
          </p>
        </header>
        <section v-for="(e, i) in current.examples" :key="i" class="grid gap-2" :aria-label="`Example ${i + 1}`">
          <div class="rounded-lg border bg-background p-4">
            <component :is="views[current.name]?.[i]" />
          </div>
          <pre class="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-small"><code>{{ e.source }}</code></pre>
        </section>
        <details v-if="current.props" class="grid gap-2">
          <summary class="cursor-pointer font-medium">Props</summary>
          <pre class="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-small"><code>{{ current.props }}</code></pre>
        </details>
      </div>
    </main>
  </div>
</template>
