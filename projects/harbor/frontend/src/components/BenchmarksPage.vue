<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from "vue";
import { Plus } from "lucide-vue-next";
import { sql, useQuery } from "@desktop";
import { Benchmarks } from "@backend";
import { Button } from "@ui/components/button";
import type { Run } from "../benchmarks";
import BenchmarkNewRun from "./BenchmarkNewRun.vue";
import BenchmarkRun from "./BenchmarkRun.vue";
import PageHeader from "./PageHeader.vue";

// The developer bar's Benchmarks page: each run on the left, newest first,
// and the one chosen, or a new run's form.
const runs = useQuery<Run>(() => sql`SELECT id, grader, status, started_at FROM benchmark_runs ORDER BY id DESC`);
const chosen = ref<number | null>(null);
const creating = ref(false);
// A run just started may not be in the query yet; the newest shows until it
// is.
const run = computed(() => runs.value.find((r) => r.id === chosen.value) ?? runs.value[0]);

// A run going in another process, such as Harbor's command-line program's,
// is ended by that process; this ends one whose process quit first, so it
// does not look as if it were going.
const going = useQuery<{ n: number }>(() => sql`SELECT COUNT(*) AS n FROM benchmark_runs WHERE ended_at IS NULL`);
let checking: ReturnType<typeof setInterval> | undefined;
watch(
  () => (going.value[0]?.n ?? 0) > 0,
  (any) => {
    clearInterval(checking);
    checking = any ? setInterval(() => Benchmarks.StopDeadRuns().catch(console.error), 5000) : undefined;
  },
  { immediate: true },
);
onUnmounted(() => clearInterval(checking));

function started(id: number) {
  creating.value = false;
  chosen.value = id;
}

// Times are stored in UTC.
const when = (at: string) =>
  new Date(at.replace(" ", "T") + "Z").toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
const states: Record<Run["status"], string> = {
  running: "Running",
  done: "Done",
  cancelled: "Cancelled",
  stopped: "Stopped when its process quit",
};
</script>

<template>
  <PageHeader title="Benchmarks">
    <Button size="sm" variant="outline" :disabled="creating" @click="creating = true"><Plus />New run</Button>
  </PageHeader>
  <div class="grid min-h-0 flex-1 grid-cols-[15rem_minmax(0,1fr)] overflow-hidden">
    <nav aria-label="Runs" class="min-h-0 overflow-y-auto border-r p-2">
      <p v-if="!runs.length" class="p-2 text-muted-foreground">No runs yet.</p>
      <button
        v-for="r in runs"
        :key="r.id"
        type="button"
        class="grid w-full gap-0.5 rounded-md px-2 py-1.5 text-left hover:bg-accent"
        :class="!creating && run?.id === r.id && 'bg-accent font-medium'"
        :aria-current="!creating && run?.id === r.id ? 'page' : undefined"
        @click="(chosen = r.id), (creating = false)"
      >
        <span>Run {{ r.id }}</span>
        <span class="text-small text-muted-foreground">{{ when(r.started_at) }} · {{ states[r.status] }}</span>
      </button>
    </nav>
    <div v-if="creating || !run" class="min-h-0 overflow-y-auto">
      <BenchmarkNewRun @started="started" @cancel="creating = false" />
    </div>
    <BenchmarkRun v-else :key="run.id" :run="run" />
  </div>
</template>
