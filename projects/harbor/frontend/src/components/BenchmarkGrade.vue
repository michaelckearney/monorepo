<script setup lang="ts">
import { ref } from "vue";
import { shown, type Call, type Result, type TrialRow } from "../benchmarks";

// A trial's grade, under its run's table: the grader's summary and each
// criterion; and the evidence the grader judged.
defineProps<{ title: string; status: TrialRow["status"]; score: number | null; result: Result | null }>();

// Why a trial has no result to show.
const missing: Record<TrialRow["status"], string> = {
  waiting: "The trial has not started.",
  running: "The trial is going; it is graded once the agent's turn ends.",
  done: "",
  failed: "The trial failed before it could be graded.",
  cancelled: "The run was cancelled before this trial was graded.",
  stopped: "Harbor quit before this trial was graded.",
};

const tabs = ["Grade", "Evidence"] as const;
const tab = ref<(typeof tabs)[number]>("Grade");

const json = (v: unknown) => JSON.stringify(v, null, 2);
const said = (c: Call) => (c.error ? `error: ${c.error}` : json(c.output));
</script>

<template>
  <section class="flex min-h-0 flex-col border-t" aria-label="Grade">
    <header class="flex shrink-0 flex-wrap items-center gap-3 px-page py-2">
      <h3 class="min-w-0 truncate font-medium">{{ title }}</h3>
      <div v-if="result" role="tablist" class="flex gap-1">
        <button
          v-for="t in tabs"
          :key="t"
          type="button"
          role="tab"
          class="rounded-md px-2 py-1 text-body hover:bg-accent"
          :class="tab === t ? 'bg-accent font-medium' : 'text-muted-foreground'"
          :aria-selected="tab === t"
          @click="tab = t"
        >
          {{ t }}
        </button>
      </div>
      <span class="ml-auto text-heading font-semibold tabular-nums" aria-label="Score">{{ shown(score) }}</span>
    </header>

    <div class="min-h-0 flex-1 overflow-y-auto px-page pb-page">
      <p v-if="!result" class="m-0 text-muted-foreground">{{ missing[status] }}</p>

      <div v-else-if="tab === 'Grade'" class="grid gap-3" role="tabpanel">
        <p v-if="result.notGraded" class="m-0 whitespace-pre-wrap text-destructive">
          Not graded: {{ result.notGraded }}
        </p>
        <template v-if="result.grade">
          <p class="m-0">{{ result.grade.summary }}</p>
          <div class="grid gap-3 [grid-template-columns:repeat(auto-fill,minmax(20rem,1fr))]">
            <div v-for="c in result.grade.criteria" :key="c.id" class="grid content-start gap-1 rounded-lg border p-3">
              <div class="flex items-baseline gap-2">
                <span class="font-medium">{{ c.title }}</span>
                <span class="text-small text-muted-foreground">weight {{ c.weight }}</span>
                <span class="ml-auto font-semibold tabular-nums">{{ shown(c.score) }}</span>
              </div>
              <p class="m-0 whitespace-pre-wrap">{{ c.rationale }}</p>
              <ul v-if="c.issues?.length" class="m-0 list-disc pl-4 text-destructive">
                <li v-for="(issue, i) in c.issues" :key="i">{{ issue }}</li>
              </ul>
              <details v-if="c.evidence?.length" class="text-body">
                <summary class="cursor-pointer text-muted-foreground">Evidence</summary>
                <ul class="m-0 mt-1 list-disc pl-4">
                  <li v-for="(e, i) in c.evidence" :key="i" class="whitespace-pre-wrap">
                    {{ e }}
                  </li>
                </ul>
              </details>
            </div>
          </div>
        </template>
      </div>

      <div v-else class="grid gap-3" role="tabpanel">
        <p
          v-if="!result.evidence.services?.length && !result.evidence.dashboards?.length"
          class="m-0 text-muted-foreground"
        >
          The agent saved nothing.
        </p>
        <details
          v-for="s in result.evidence.services ?? []"
          :key="`s-${s.name}`"
          class="rounded-lg border px-3 py-1.5"
        >
          <summary class="cursor-pointer font-medium">Service {{ s.name }}</summary>
          <p v-if="s.problem" class="m-0 text-destructive">Harbor cannot read it: {{ s.problem }}</p>
          <div
            v-for="phase in [
              { title: 'signed in', calls: s.signedIn },
              { title: 'signed out', calls: s.signedOut },
            ]"
            :key="phase.title"
          >
            <div v-for="c in phase.calls ?? []" :key="c.tool" class="mt-2 text-body">
              <span class="font-mono">{{ c.tool }}</span
              >, {{ phase.title }}
              <pre
                class="mt-1 max-h-48 overflow-auto whitespace-pre-wrap font-mono text-small"
                :class="c.error && 'text-destructive'"
                >{{ said(c) }}</pre>
            </div>
          </div>
          <pre class="mt-2 max-h-96 overflow-auto whitespace-pre-wrap border-t pt-2 font-mono text-small">{{
            s.source
          }}</pre>
        </details>
        <details
          v-for="d in result.evidence.dashboards ?? []"
          :key="`d-${d.name}`"
          class="rounded-lg border px-3 py-1.5"
        >
          <summary class="cursor-pointer font-medium">Dashboard {{ d.name }}</summary>
          <pre class="mt-2 max-h-96 overflow-auto whitespace-pre-wrap font-mono text-small">{{ d.source }}</pre>
        </details>
        <div v-if="result.evidence.prompts?.length">
          <h4 class="mb-1 text-body font-medium">Tool calls Harbor would have asked about (the run allowed each)</h4>
          <pre class="max-h-48 overflow-auto whitespace-pre-wrap font-mono text-small">{{
            json(result.evidence.prompts)
          }}</pre>
        </div>
        <div v-if="result.evidence.signIns?.length" class="grid gap-1">
          <h4 class="text-body font-medium">Sign-ins the run did for the user</h4>
          <details v-for="(si, i) in result.evidence.signIns" :key="i" class="text-body">
            <summary class="cursor-pointer">
              {{ si.service }}, in the {{ si.browser }}'s browser:
              {{ si.outcome }}
            </summary>
            <ol class="m-0 list-decimal pl-4 text-muted-foreground">
              <li v-for="(step, j) in si.steps ?? []" :key="j">{{ step }}</li>
            </ol>
          </details>
        </div>
      </div>
    </div>
  </section>
</template>
