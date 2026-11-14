<script setup lang="ts">
import { computed, onUnmounted, ref } from "vue";
import { sql, useQuery } from "@desktop";
import { Benchmarks } from "@backend";
import { Button } from "@ui/components/button";
import {
  opens,
  shown,
  summarise,
  summaryState,
  trialSeconds,
  trialState,
  type Result,
  type Run,
  type TrialRow,
} from "../benchmarks";
import BenchmarkChat from "./BenchmarkChat.vue";
import BenchmarkGrade from "./BenchmarkGrade.vue";

// A run: one row for each scenario on each model, and under it a row for
// each trial when it ran more than once, as they run. A trial chosen shows
// its grade under the table, and its chat in a panel on the right, as the
// agent's is beside Harbor. A trial going shows its chat, tokens, and time
// so far, which its run copies here as they change.
const props = defineProps<{ run: Run }>();

const trials = useQuery<TrialRow>(
  () => sql`SELECT id, scenario, model, n, status, score, seconds, input_tokens, output_tokens, error, started_at
    FROM benchmark_trials WHERE run_id = ${props.run.id} ORDER BY id`,
);
const summaries = computed(() => summarise(trials.value));
const repeats = computed(() => summaries.value.some((s) => s.trials > 1));
const finished = computed(() => trials.value.filter((t) => !["waiting", "running"].includes(t.status)).length);

const chosen = ref<number | null>(null);
const wide = ref(false);
const picked = useQuery<{ result: string | null; transcript: string | null }>(() =>
  chosen.value === null ? null : sql`SELECT result, transcript FROM benchmark_trials WHERE id = ${chosen.value}`,
);
const trial = computed(() => trials.value.find((t) => t.id === chosen.value));
const result = computed(() => (picked.value[0]?.result ? (JSON.parse(picked.value[0].result) as Result) : null));
// A done trial's chat is in its result; one going, or cut short, has only
// its chat so far.
const transcript = computed<Result["transcript"]>(() => {
  const partial = picked.value[0]?.transcript;
  return result.value?.transcript ?? (partial ? JSON.parse(partial) : []);
});

// The clock a trial going counts its seconds by.
const now = ref(Date.now());
const tick = setInterval(() => (now.value = Date.now()), 1000);
onUnmounted(() => clearInterval(tick));
const title = computed(() =>
  trial.value ? `${trial.value.scenario} on ${trial.value.model}, trial ${trial.value.n}` : "",
);
function choose(t: TrialRow) {
  if (opens(t)) {
    chosen.value = t.id;
  }
}
</script>

<template>
  <div
    class="grid min-h-0 flex-1 overflow-hidden"
    :style="{
      gridTemplateColumns: `minmax(0, 1fr) ${trial ? (wide ? '38rem' : '24rem') : '0'}`,
    }"
  >
    <div
      class="grid min-h-0 min-w-0"
      :class="trial ? 'grid-rows-[minmax(0,1fr)_minmax(0,1fr)]' : 'grid-rows-[minmax(0,1fr)]'"
    >
      <div class="grid min-h-0 content-start gap-4 overflow-y-auto p-page">
        <div class="flex flex-wrap items-center gap-3">
          <h2 class="text-title font-semibold">Run {{ run.id }}</h2>
          <span class="text-muted-foreground">
            Graded by {{ run.grader }} · {{ finished }} of {{ trials.length }} trials finished
          </span>
          <Button
            v-if="run.status === 'running'"
            size="sm"
            variant="outline"
            class="ml-auto"
            @click="Benchmarks.Cancel(run.id)"
          >
            Cancel the run
          </Button>
        </div>
        <p class="m-0 text-small text-muted-foreground">
          Scores are from 0 to 100. Tokens and seconds are the agent's.
          <template v-if="repeats">
            A scenario run more than once shows the means over its trials that ran to the end, and each trial under it.
          </template>
          Choose a trial to see its grade and transcript.
        </p>

        <div class="overflow-x-auto">
          <table class="w-full text-left text-body" aria-label="Trials">
            <thead class="border-b text-muted-foreground">
              <tr>
                <th class="py-1.5 pr-3 pl-1 font-medium">Scenario</th>
                <th class="py-1.5 pr-3 font-medium">Model</th>
                <th class="py-1.5 pr-3 font-medium">Status</th>
                <th class="py-1.5 pr-3 text-right font-medium">Score</th>
                <th v-if="repeats" class="py-1.5 pr-3 text-right font-medium">Lowest</th>
                <th class="py-1.5 pr-3 text-right font-medium">Tokens in</th>
                <th class="py-1.5 pr-3 text-right font-medium">Tokens out</th>
                <th class="py-1.5 pr-1 text-right font-medium">Seconds</th>
              </tr>
            </thead>
            <tbody>
              <template v-for="s in summaries" :key="`${s.scenario}-${s.model}`">
                <!-- One trial: the row is the trial. -->
                <tr
                  v-if="s.trials === 1"
                  class="border-b"
                  :class="[
                    opens(s.rows[0]) && 'cursor-pointer hover:bg-accent',
                    chosen === s.rows[0].id && 'bg-accent',
                  ]"
                  :aria-selected="chosen === s.rows[0].id"
                  @click="choose(s.rows[0])"
                >
                  <td class="py-1.5 pr-3 pl-1">
                    <button
                      type="button"
                      class="font-mono disabled:cursor-default"
                      :disabled="!opens(s.rows[0])"
                      :aria-label="`${s.scenario} on ${s.model}, trial 1`"
                    >
                      {{ s.scenario }}
                    </button>
                    <span v-if="s.rows[0].error" class="block max-w-md truncate text-small text-destructive">{{
                      s.rows[0].error
                    }}</span>
                  </td>
                  <td class="py-1.5 pr-3">{{ s.model }}</td>
                  <td
                    class="py-1.5 pr-3"
                    :class="trialState(s.rows[0]).bad ? 'text-destructive' : 'text-muted-foreground'"
                  >
                    {{ trialState(s.rows[0]).label }}
                  </td>
                  <td class="py-1.5 pr-3 text-right font-semibold tabular-nums">
                    {{ shown(s.rows[0].score) }}
                  </td>
                  <td v-if="repeats" class="py-1.5 pr-3"></td>
                  <td class="py-1.5 pr-3 text-right tabular-nums">
                    {{ shown(s.rows[0].input_tokens) }}
                  </td>
                  <td class="py-1.5 pr-3 text-right tabular-nums">
                    {{ shown(s.rows[0].output_tokens) }}
                  </td>
                  <td class="py-1.5 pr-1 text-right tabular-nums">
                    {{ shown(trialSeconds(s.rows[0], now)) }}
                  </td>
                </tr>
                <!-- Several: the means, then each trial. -->
                <template v-else>
                  <tr class="border-b">
                    <td class="py-1.5 pr-3 pl-1 font-mono">{{ s.scenario }}</td>
                    <td class="py-1.5 pr-3">{{ s.model }}</td>
                    <td class="py-1.5 pr-3" :class="summaryState(s).bad ? 'text-destructive' : 'text-muted-foreground'">
                      {{ summaryState(s).label }}
                    </td>
                    <td class="py-1.5 pr-3 text-right font-semibold tabular-nums">
                      {{ shown(s.mean) }}
                    </td>
                    <td class="py-1.5 pr-3 text-right tabular-nums">
                      {{ shown(s.lowest) }}
                    </td>
                    <td class="py-1.5 pr-3 text-right tabular-nums">
                      {{ shown(s.inputTokens) }}
                    </td>
                    <td class="py-1.5 pr-3 text-right tabular-nums">
                      {{ shown(s.outputTokens) }}
                    </td>
                    <td class="py-1.5 pr-1 text-right tabular-nums">
                      {{ shown(s.seconds) }}
                    </td>
                  </tr>
                  <tr
                    v-for="t in s.rows"
                    :key="t.id"
                    class="border-b text-muted-foreground"
                    :class="[opens(t) && 'cursor-pointer hover:bg-accent', chosen === t.id && 'bg-accent']"
                    :aria-selected="chosen === t.id"
                    @click="choose(t)"
                  >
                    <td class="py-1 pr-3 pl-6">
                      <button
                        type="button"
                        class="disabled:cursor-default"
                        :disabled="!opens(t)"
                        :aria-label="`${t.scenario} on ${t.model}, trial ${t.n}`"
                      >
                        Trial {{ t.n }}
                      </button>
                      <span v-if="t.error" class="block max-w-md truncate text-small text-destructive">{{ t.error }}</span>
                    </td>
                    <td class="py-1 pr-3"></td>
                    <td class="py-1 pr-3" :class="trialState(t).bad && 'text-destructive'">
                      {{ trialState(t).label }}
                    </td>
                    <td class="py-1 pr-3 text-right tabular-nums text-foreground">
                      {{ shown(t.score) }}
                    </td>
                    <td class="py-1 pr-3"></td>
                    <td class="py-1 pr-3 text-right tabular-nums">
                      {{ shown(t.input_tokens) }}
                    </td>
                    <td class="py-1 pr-3 text-right tabular-nums">
                      {{ shown(t.output_tokens) }}
                    </td>
                    <td class="py-1 pr-1 text-right tabular-nums">
                      {{ shown(trialSeconds(t, now)) }}
                    </td>
                  </tr>
                </template>
              </template>
            </tbody>
          </table>
        </div>
      </div>
      <BenchmarkGrade
        v-if="trial"
        :key="trial.id"
        :title="title"
        :status="trial.status"
        :score="trial.score"
        :result="result"
      />
    </div>
    <BenchmarkChat
      v-if="trial"
      :key="trial.id"
      :title="title"
      :transcript="transcript"
      :working="trial.status === 'running'"
      :failed="trial.status === 'failed'"
      :error="trial.error"
      :wide="wide"
      @widen="wide = !wide"
      @close="chosen = null"
    />
  </div>
</template>
