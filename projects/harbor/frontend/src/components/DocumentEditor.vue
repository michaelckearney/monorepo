<script setup lang="ts">
import { Button } from "@ui/components/button";
import { computed, onMounted, ref, watch } from "vue";
import { sql, useQuery } from "@desktop";
import { Documents } from "@backend";
import { close, open, type Kind } from "../navigation";
import CodeEditor from "./CodeEditor.vue";
import { Input } from "@ui/components/input";
import PageHeader from "./PageHeader.vue";

// The whole file in one editor. id 0 is a new document, which starts from the
// starter file.
const props = defineProps<{ kind: Kind; id: number }>();

const source = ref("");
const name = ref("");
const loaded = ref(false);
const problems = ref<string[]>([]);
const saving = ref(false);

// Read the saved file once. Live updates would replace what is being typed.
const saved = useQuery<{ name: string; source: string }>(() => {
  if (props.id === 0 || loaded.value) {
    return null;
  }
  return props.kind === "service"
    ? sql`SELECT name, source FROM services WHERE id = ${props.id}`
    : sql`SELECT name, source FROM dashboards WHERE id = ${props.id}`;
});
watch(
  saved,
  (rows) => {
    if (rows[0] && !loaded.value) {
      source.value = rows[0].source;
      name.value = rows[0].name;
      loaded.value = true;
    }
  },
  { immediate: true },
);

onMounted(async () => {
  if (props.id === 0) {
    source.value =
      props.kind === "service" ? await Documents.ServiceStarter() : await Documents.DashboardStarter();
    loaded.value = true;
  }
});

const heading = computed(() => {
  const what = props.kind === "service" ? "service" : "dashboard";
  return props.id === 0 ? `New ${what}` : `Edit ${what}`;
});

async function save() {
  if (saving.value) {
    return;
  }
  problems.value = [];
  saving.value = true;
  try {
    const id =
      props.kind === "service"
        ? await Documents.SaveService(props.id, source.value)
        : await Documents.SaveDashboard(props.id, name.value, source.value);
    open(props.kind, id);
  } catch (e) {
    problems.value = (e instanceof Error ? e.message : String(e)).split("\n");
  } finally {
    saving.value = false;
  }
}

function cancel() {
  if (props.id === 0) {
    close();
  } else {
    open(props.kind, props.id);
  }
}

function onKey(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === "s") {
    e.preventDefault();
    void save();
  }
}
</script>

<template>
  <form class="flex min-h-0 flex-1 flex-col" @submit.prevent="save" @keydown="onKey">
    <PageHeader :title="heading">
      <template #title>
        <Input v-if="kind === 'dashboard'" v-model="name" placeholder="Name" aria-label="Dashboard name" class="w-64" />
      </template>
      <Button type="button" size="sm" variant="outline" @click="cancel">Cancel</Button>
      <Button type="submit" size="sm" :disabled="!loaded || saving">Save</Button>
    </PageHeader>
    <div class="flex min-h-0 flex-1 flex-col gap-3 p-4">
      <ul
        v-if="problems.length > 0"
        class="list-disc rounded-md border border-destructive py-2 pl-6 pr-3 font-mono text-small whitespace-pre-wrap text-destructive"
        role="alert"
        aria-label="Problems"
      >
        <li v-for="p in problems" :key="p">{{ p }}</li>
      </ul>
      <div class="min-h-0 flex-1">
        <CodeEditor
          v-if="loaded"
          v-model="source"
          :language="kind === 'service' ? 'typescript' : 'vue'"
          :label="kind === 'service' ? 'Service code' : 'Dashboard code'"
        />
      </div>
    </div>
  </form>
</template>
