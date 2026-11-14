<script setup lang="ts">
import { computed } from "vue";
import { ChevronRight, CircleDot, GitBranch, GitPullRequest, Plus, Settings, Ship, Terminal } from "lucide-vue-next";
import type { Project, Run } from "@backend";
import SidePanel from "@ui/components/SidePanel.vue";
import Tip from "@ui/components/Tip.vue";
import { status, type Item } from "../items";
import { defaultPanels, setPanel, toggleFolded, usePreferences } from "../preferences";
import { projectOf, runOf, runsHere } from "../runs";

// The sidebar, as Harbor's: a section for each project of each monorepo,
// with its default branch and the items for it; then the framework's, and
// any items with no scope, which no bot works. Each section folds. A dot
// says an item needs you (yellow) or the bot is on it (green); a terminal
// says it runs on this computer. settingsAlert, when there is one, puts a
// dot on Settings.
const props = defineProps<{
  projects: Project[];
  items: Record<string, Item[]>;
  runs: Record<string, Run[]>;
  selected: { repo: string; key: string };
  settingsOpen: boolean;
  settingsAlert: string;
}>();
const emit = defineEmits<{ select: [repo: string, key: string]; newIssue: [repo: string, scope: string]; settings: [] }>();

const preferences = usePreferences();
const short = (repo: string) => repo.slice(repo.indexOf("/") + 1);

/** A section: a project, the framework, or the items with no scope. scope is what a new issue in it is for. */
type Section = { key: string; repo: string; base: string; name: string; project: string; scope: string | null; items: Item[] };
const sections = computed<Section[]>(() =>
  props.projects.flatMap((p) => {
    const named = (name: string) => (props.projects.length > 1 ? `${short(p.repo)}/${name}` : name);
    const scopes = [...(p.projects ?? []).map((name) => `project:${name}`), "framework"];
    const items = props.items[p.repo] ?? [];
    const of = (scope: string) => items.filter((i) => i.scope === scope);
    const out: Section[] = [
      ...(p.projects ?? []).map((name) => ({ key: `${p.repo}/project:${name}`, repo: p.repo, base: p.base, name: named(name), project: name, scope: `project:${name}`, items: of(`project:${name}`) })),
      { key: `${p.repo}/framework`, repo: p.repo, base: p.base, name: named("framework"), project: "", scope: "framework", items: of("framework") },
    ];
    const unscoped = items.filter((i) => !scopes.includes(i.scope));
    if (unscoped.length) {
      out.push({ key: `${p.repo}/`, repo: p.repo, base: p.base, name: named("no scope"), project: "", scope: null, items: unscoped });
    }
    return out;
  }),
);
const waiting = computed(() => props.projects.filter((p) => !p.ready));

const isFolded = (s: Section) => preferences.value.folded.includes(s.key);
const needsYou = (s: Section) => s.items.some((i) => status(i) === "needs-you");
const anyNeedsYou = computed(() => sections.value.some(needsYou));
const mainRuns = (s: Section) => runsHere(runOf(props.runs[s.repo], s.project), 0);
const here = (repo: string, i: Item) => (props.runs[repo] ?? []).some((r) => runsHere(r, i.pr?.number ?? -1, projectOf(i.scope)));
const isSelected = (repo: string, key: string) => props.selected.repo === repo && props.selected.key === key;

const row = "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sidebar-foreground hover:bg-sidebar-accent";
const current = "bg-sidebar-accent font-medium text-sidebar-accent-foreground";
const heading = "flex items-center gap-1 px-2 py-1 text-small font-medium text-muted-foreground";
const add = "grid size-5 shrink-0 place-items-center rounded hover:bg-sidebar-accent";
</script>

<template>
  <SidePanel
    side="left"
    :panel="preferences.panels.left"
    :other="preferences.panels.right"
    :initial="defaultPanels.left"
    element="nav"
    landmark="Slipway"
    label="the sidebar"
    :icon="Ship"
    :minimum="200"
    shortcut="b"
    :alert="anyNeedsYou ? 'An item is waiting for you' : settingsAlert || undefined"
    @save="(p) => setPanel('left', p)"
  >
    <template #title>Slipway</template>
    <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-2 py-3">
      <section v-for="s in sections" :key="s.key" :aria-label="s.name">
        <header :class="heading">
          <h2 class="flex min-w-0 flex-1">
            <button
              type="button"
              class="flex min-w-0 flex-1 items-center gap-1 text-left hover:text-foreground"
              :aria-expanded="!isFolded(s)"
              @click="toggleFolded(s.key)"
            >
              <ChevronRight class="size-3.5 shrink-0 transition-transform" :class="!isFolded(s) && 'rotate-90'" aria-hidden="true" />
              <span class="min-w-0 truncate">{{ s.name }}</span>
              <template v-if="isFolded(s) && needsYou(s)">
                <span class="size-1.5 shrink-0 rounded-full bg-warning" aria-hidden="true"></span>
                <span class="sr-only">(an item is waiting for you)</span>
              </template>
            </button>
          </h2>
          <Tip v-if="s.scope !== null" :label="`New issue for ${s.name}`">
            <button type="button" :class="add" :aria-label="`New issue for ${s.name}`" @click="emit('newIssue', s.repo, s.scope)">
              <Plus class="size-3.5" />
            </button>
          </Tip>
        </header>
        <ul v-if="!isFolded(s)">
          <li v-if="s.project">
            <button
              type="button"
              :class="[row, isSelected(s.repo, `main:${s.project}`) && current]"
              :aria-current="isSelected(s.repo, `main:${s.project}`) ? 'page' : undefined"
              @click="emit('select', s.repo, `main:${s.project}`)"
            >
              <GitBranch class="size-4 shrink-0 text-muted-foreground" />
              <span class="min-w-0 flex-1 truncate font-mono text-small">{{ s.base }}</span>
              <Terminal v-if="mainRuns(s)" class="size-3.5 shrink-0 text-muted-foreground" aria-label="Runs on this computer" />
            </button>
          </li>
          <li v-for="i in s.items" :key="i.key">
            <button
              type="button"
              :class="[row, isSelected(s.repo, i.key) && current]"
              :aria-current="isSelected(s.repo, i.key) ? 'page' : undefined"
              :title="`#${i.number} ${i.title}`"
              @click="emit('select', s.repo, i.key)"
            >
              <CircleDot v-if="i.issue" class="size-4 shrink-0 text-muted-foreground" />
              <GitPullRequest v-else class="size-4 shrink-0 text-muted-foreground" />
              <span class="min-w-0 flex-1 truncate">{{ i.title || `#${i.number}` }}</span>
              <Terminal v-if="here(s.repo, i)" class="size-3.5 shrink-0 text-muted-foreground" aria-label="Runs on this computer" />
              <template v-if="status(i) === 'needs-you'">
                <span class="size-1.5 shrink-0 rounded-full bg-warning" title="Waiting for you"></span>
                <span class="sr-only">(waiting for you)</span>
              </template>
              <template v-else-if="status(i) === 'working'">
                <span class="size-1.5 shrink-0 animate-pulse rounded-full bg-success" title="The bot is working"></span>
                <span class="sr-only">(the bot is working)</span>
              </template>
            </button>
          </li>
        </ul>
      </section>
      <p v-for="p in waiting" :key="p.repo" class="px-2 text-small text-muted-foreground">
        The bots of {{ short(p.repo) }} wait for a valid harness.yaml on {{ p.base }}.
      </p>
      <p v-if="!projects.length" class="px-2 py-1 text-muted-foreground">No projects yet.</p>
    </div>

    <footer class="grid gap-2 border-t p-2">
      <slot />
      <button
        type="button"
        :class="[row, settingsOpen && current]"
        :aria-description="settingsAlert || undefined"
        @click="emit('settings')"
      >
        <Settings class="size-4 text-muted-foreground" /><span class="flex-1">Settings</span>
        <span v-if="settingsAlert" class="size-1.5 shrink-0 rounded-full bg-warning" aria-hidden="true"></span>
      </button>
    </footer>
  </SidePanel>
</template>
