<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { Check, ChevronDown, Loader2, RefreshCw, RotateCcw, Square, Terminal } from "lucide-vue-next";
import { Local } from "@backend";
import type { Mode, Project, Run } from "@backend";
import IconButton from "@ui/components/IconButton.vue";
import SidePanel from "@ui/components/SidePanel.vue";
import { Popover, PopoverContent, PopoverTrigger } from "@ui/components/popover";
import type { Item } from "../items";
import { message } from "../poll";
import { defaultPanels, setPanel, usePreferences } from "../preferences";
import { active, busy, projectOf, runHere, runOf, runsHere, running } from "../runs";

// What runs on this computer, for the whole window: the panel on the right,
// collapsed to a strip until you open it (⌘J), whatever item you are
// looking at. Each project of a monorepo runs on its own, all
// at once, one branch each; the picker in the header says which, and runs
// another branch of a project in place of that project's. It shows the
// output of one project's run: the one you picked, or else one of the
// monorepo you are looking at that runs, or else any that runs.
const props = defineProps<{
  projects: Project[];
  items: Record<string, Item[]>;
  runs: Record<string, Run[]>;
  /** The monorepo you are looking at. */
  looking: string;
}>();
const emit = defineEmits<{ ran: [] }>();

type Shown = { repo: string; project: string };
const picked = ref<Shown | null>(null);
const firstActive = (repo: string) => (props.runs[repo] ?? []).find(active);
const shown = computed<Shown>(() => {
  const p = picked.value;
  if (p && active(runOf(props.runs[p.repo], p.project))) return p;
  const here = firstActive(props.looking);
  if (here) return { repo: props.looking, project: here.project };
  for (const m of props.projects) {
    const r = firstActive(m.repo);
    if (r) return { repo: m.repo, project: r.project };
  }
  return p ?? { repo: props.looking, project: props.projects.find((m) => m.repo === props.looking)?.projects?.[0] ?? "" };
});
const project = computed(() => props.projects.find((p) => p.repo === shown.value.repo) ?? null);
const run = computed(() => runOf(props.runs[shown.value.repo], shown.value.project));
const short = (repo: string) => repo.slice(repo.indexOf("/") + 1);
const label = computed(() => {
  const what = running(run.value, project.value?.base ?? "");
  const name = shown.value.project || (project.value ? short(project.value.repo) : "");
  return name && run.value?.running ? `${name}${what ? ` ${what}` : ""}` : name ? `${name}: nothing runs` : "Nothing";
});

// The menu: each project of each monorepo, with its default branch and
// the pull requests for it, or for no one project.
const sections = computed(() =>
  props.projects.flatMap((p) =>
    (p.projects ?? []).map((name) => ({
      repo: p.repo,
      base: p.base,
      name,
      title: props.projects.length > 1 ? `${short(p.repo)} · ${name}` : name,
      prs: (props.items[p.repo] ?? []).filter((i) => i.pr && [name, ""].includes(projectOf(i.scope))).map((i) => i.pr!),
    })),
  ),
);
const runsBranch = (repo: string, name: string, pr: number) => runsHere(runOf(props.runs[repo], name), pr);

// The modes of the project shown, as its clone's framework says: the
// default first. Picking another runs it again, in that mode.
const modes = ref<Mode[]>([]);
watch(
  () => [shown.value.repo, shown.value.project, run.value?.phase ?? ""] as const,
  async ([repo, name], before) => {
    if (before && before[0] === repo && before[1] === name && modes.value.length) return;
    if (!name) {
      modes.value = [];
      return;
    }
    try {
      modes.value = (await Local.Modes(repo, name)) ?? [];
    } catch {
      modes.value = [];
    }
  },
  { immediate: true },
);
const mode = computed(() => run.value?.mode || modes.value[0]?.mode || "");
async function switchMode(m: string) {
  if (!run.value?.running || m === mode.value) return;
  await act(() => runHere(shown.value.repo, run.value?.pr ?? 0, false, shown.value.project, m));
}

const preferences = usePreferences();
const failed = computed(() => !!run.value?.running && run.value.state === "failed");

const menu = ref(false);
const error = ref("");
async function act(f: () => Promise<unknown>) {
  error.value = "";
  try {
    await f();
  } catch (e) {
    error.value = message(e);
  }
  emit("ran");
}
async function pick(repo: string, name: string, pr: number, fork: boolean) {
  menu.value = false;
  picked.value = { repo, project: name };
  if (!runsBranch(repo, name, pr)) await act(() => runHere(repo, pr, fork, name));
}

// The output, kept at its end as it grows while you are at the end.
const out = ref<HTMLElement | null>(null);
watch(
  () => run.value?.output,
  async () => {
    const el = out.value;
    if (el && el.scrollHeight - el.scrollTop - el.clientHeight > 60) return;
    await nextTick();
    out.value?.scrollTo({ top: out.value.scrollHeight });
  },
);
</script>

<template>
  <SidePanel
    side="right"
    :panel="preferences.panels.right"
    :other="preferences.panels.left"
    :initial="defaultPanels.right"
    element="aside"
    landmark="App output"
    label="the app's output"
    :icon="Terminal"
    :minimum="320"
    shortcut="j"
    :alert="failed ? 'It failed to run' : undefined"
    @save="(p) => setPanel('right', p)"
  >
    <template #title>App output</template>
    <template #actions>
      <template v-if="run?.running">
        <IconButton label="Fetch, to catch up with GitHub" @click="act(() => Local.Refresh(shown.repo, shown.project))">
          <RefreshCw :class="run.fetching && 'animate-spin'" />
        </IconButton>
        <IconButton v-if="run.pr" label="Reset data, so the branch starts with none" @click="act(() => Local.Reset(shown.repo, shown.project))">
          <RotateCcw />
        </IconButton>
        <IconButton v-if="run.started" label="Stop" class="text-destructive" @click="act(() => Local.Stop(shown.repo, shown.project))">
          <Square />
        </IconButton>
      </template>
    </template>

    <div class="flex shrink-0 items-center gap-2 border-b px-3 py-2">
      <Popover v-model:open="menu">
        <PopoverTrigger
          class="flex h-control-sm min-w-0 flex-1 items-center gap-1 rounded-md border bg-background px-2 text-small hover:bg-accent"
          aria-label="Run on this computer"
        >
          <span class="min-w-0 flex-1 truncate text-left">{{ label }}</span><ChevronDown class="size-3 shrink-0" />
        </PopoverTrigger>
        <PopoverContent align="start" class="w-80 p-1" role="menu">
          <template v-for="(sec, n) in sections" :key="`${sec.repo}#${sec.name}`">
            <div v-if="n" class="my-1 border-t" />
            <div class="px-2 pb-1 pt-1.5 text-caption font-medium text-muted-foreground">{{ sec.title }}</div>
            <button
              type="button"
              role="menuitemradio"
              :aria-checked="runsBranch(sec.repo, sec.name, 0)"
              class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-accent"
              @click="pick(sec.repo, sec.name, 0, false)"
            >
              <Check class="size-3.5 shrink-0" :class="!runsBranch(sec.repo, sec.name, 0) && 'invisible'" />
              <span class="flex-1 font-mono text-small">{{ sec.base }}</span>
            </button>
            <button
              v-for="pr in sec.prs"
              :key="pr.number"
              type="button"
              role="menuitemradio"
              :aria-checked="runsBranch(sec.repo, sec.name, pr.number)"
              class="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-accent"
              :title="pr.title"
              @click="pick(sec.repo, sec.name, pr.number, pr.fork)"
            >
              <Check class="size-3.5 shrink-0" :class="!runsBranch(sec.repo, sec.name, pr.number) && 'invisible'" />
              <span class="min-w-0 flex-1 truncate"><span class="text-muted-foreground">#{{ pr.number }}</span> {{ pr.title }}</span>
            </button>
          </template>
        </PopoverContent>
      </Popover>
      <select
        v-if="modes.length > 1"
        aria-label="Mode"
        title="The mode it runs in"
        class="h-control-sm rounded-md border bg-background px-1.5 text-small"
        :value="mode"
        @change="switchMode(($event.target as HTMLSelectElement).value)"
      >
        <option v-for="m in modes" :key="m.mode" :value="m.mode">{{ m.mode }}</option>
      </select>
    </div>

    <p v-if="busy(run)" class="flex shrink-0 items-center gap-2 border-b px-3 py-2 text-small text-muted-foreground">
      <Loader2 class="size-3.5 animate-spin" />{{ run?.phase === "cloning" ? `Cloning ${shown.repo}…` : "Starting. The first build takes a minute or two." }}
    </p>
    <p v-else-if="run?.running && run.state === 'building'" class="flex shrink-0 items-center gap-2 border-b px-3 py-2 text-small text-muted-foreground">
      <Loader2 class="size-3.5 animate-spin" />Building…
    </p>
    <p v-else-if="failed" class="shrink-0 border-b px-3 py-2 text-small text-destructive">{{ run?.message || "It failed to run." }}</p>
    <p v-if="error || run?.error" class="shrink-0 border-b px-3 py-2 text-small text-destructive">{{ error || run?.error }}</p>

    <pre v-if="run?.output" ref="out" class="min-h-0 flex-1 overflow-auto whitespace-pre-wrap px-3 py-2 font-mono text-small text-sidebar-foreground">{{
      run.output
    }}</pre>
    <p v-else class="px-3 py-2 text-muted-foreground">
      {{ run?.running ? "It was started outside the app, so its output is where it was started." : "Nothing runs here yet. Pick a branch above to run it." }}
    </p>
  </SidePanel>
</template>
