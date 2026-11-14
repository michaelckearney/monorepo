<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Download, RotateCw } from "lucide-vue-next";
import { GitHub, Local, Server, Settings, Updates } from "@backend";
import type { Issue, Project, Run, Status, Thread } from "@backend";
import { Button } from "@ui/components/button";
import { Input } from "@ui/components/input";
import { Textarea } from "@ui/components/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@ui/components/dialog";
import Sidebar from "@/components/Sidebar.vue";
import ItemView from "@/components/ItemView.vue";
import OutputPanel from "@/components/OutputPanel.vue";
import SettingsDialog from "@/components/SettingsDialog.vue";
import { group, type Item } from "./items";
import { message, usePoll } from "./poll";
import { applyTheme } from "./preferences";
import { runFor } from "./runs";

applyTheme();

// The projects are the server's: the repositories its host config gives a
// bot. The app keeps its own clone of each to run, so there is nothing to
// add here.
const status = usePoll(() => Server.Status(), null as Status | null, 10_000);
const projects = usePoll(() => (status.value.value?.ok ? Server.Projects() : Promise.resolve([] as Project[])), [] as Project[], 15_000, [
  () => status.value.value?.ok,
]);
// Every project's threads, in one request, for the counts beside each
// project and for the project shown.
const threads = usePoll(() => (status.value.value?.ok ? Server.Threads("") : Promise.resolve([] as Thread[])), [] as Thread[], 4_000, [
  () => status.value.value?.ok,
]);
const login = usePoll(() => GitHub.Login(), "", 60_000);
const update = usePoll(() => Updates.Check(), null as Awaited<ReturnType<typeof Updates.Check>> | null, 6 * 3600_000);

const repos = computed(() => (projects.value.value ?? []).map((p) => p.repo));

// Every project's open issues and pull requests, from GitHub, for the
// sidebar. A project GitHub cannot list keeps what it had.
type Lists = { issues: Issue[]; prs: Issue[]; error: string };
const lists: ReturnType<typeof usePoll<Record<string, Lists>>> = usePoll(
  async (): Promise<Record<string, Lists>> =>
    Object.fromEntries(
      await Promise.all(
        repos.value.map(async (repo): Promise<[string, Lists]> => {
          try {
            const [issues, prs] = await Promise.all([GitHub.Issues(repo), GitHub.PullRequests(repo)]);
            return [repo, { issues: issues ?? [], prs: prs ?? [], error: "" }];
          } catch (e) {
            const had: Lists | undefined = lists.value.value[repo];
            return [repo, { issues: had?.issues ?? [], prs: had?.prs ?? [], error: message(e) }];
          }
        }),
      ),
    ),
  {} as Record<string, Lists>,
  30_000,
  [() => repos.value.join()],
);
// What each project's harness run on this computer is doing.
const runs = usePoll(
  async () =>
    Object.fromEntries(
      await Promise.all(repos.value.map((repo) => Local.State(repo).then((r): [string, Run[]] => [repo, r ?? []]).catch((): [string, Run[]] => [repo, []]))),
    ),
  {} as Record<string, Run[]>,
  1_500,
  [() => repos.value.join()],
);
const reloadRuns = runs.reload;

const items = computed<Record<string, Item[]>>(() =>
  Object.fromEntries(
    repos.value.map((repo) => {
      const l = lists.value.value[repo];
      return [repo, group(l?.issues ?? [], l?.prs ?? [], (threads.value.value ?? []).filter((t) => t.repo === repo))];
    }),
  ),
);

// What you are looking at: an item of a monorepo, or one of its projects'
// default branch, "main:<project>". At first, the first item of the first
// monorepo, which is the one that most needs you.
const selected = ref({ repo: "", key: "" });
// Until you pick one, it follows the first project's list as it loads.
const picked = ref(false);
watch(
  [repos, items],
  () => {
    if (picked.value && repos.value.includes(selected.value.repo)) return;
    const repo = repos.value[0] ?? "";
    const first = projects.value.value?.find((p) => p.repo === repo)?.projects?.[0] ?? "";
    selected.value = { repo, key: items.value[repo]?.[0]?.key ?? `main:${first}` };
  },
  { immediate: true },
);
const project = computed(() => projects.value.value?.find((p) => p.repo === selected.value.repo) ?? null);
const item = computed(() => items.value[selected.value.repo]?.find((i) => i.key === selected.value.key) ?? null);
// The project the view runs: the default branch's, or the item's; and an
// item that is gone, merged or closed, shows its project's default branch.
const mainOf = computed(() => {
  if (selected.value.key.startsWith("main:")) return selected.value.key.slice("main:".length);
  const scope = item.value?.scope ?? "";
  return scope.startsWith("project:") ? scope.slice("project:".length) : "";
});
const shownKey = computed(() => (item.value ? item.value.key : `main:${mainOf.value || (project.value?.projects?.[0] ?? "")}`));
const shownProject = computed(() => (item.value ? mainOf.value : shownKey.value.slice("main:".length)));
const listError = computed(() => lists.value.value[selected.value.repo]?.error ?? "");

// A new issue is for a project or the framework, by its scope label, which
// is what a bot works on; one with none is a note no bot works.
const newIssueFor = ref("");
const issueScope = ref("");
const issueTitle = ref("");
const issueBody = ref("");
const newError = ref("");
const scopes = computed(() => [
  ...(projects.value.value?.find((p) => p.repo === newIssueFor.value)?.projects ?? []).map((n) => ({ value: `project:${n}`, label: n })),
  { value: "framework", label: "the framework, and every project" },
  { value: "", label: "nothing: a note no bot works" },
]);
function openNewIssue(repo: string, scope: string) {
  newIssueFor.value = repo;
  issueScope.value = scope;
  newError.value = "";
}
async function createIssue() {
  newError.value = "";
  try {
    const repo = newIssueFor.value;
    const n = await GitHub.NewIssue(repo, issueTitle.value, issueBody.value, issueScope.value);
    newIssueFor.value = "";
    issueTitle.value = issueBody.value = "";
    await lists.reload();
    selected.value = { repo, key: `issue-${n}` };
    picked.value = true;
  } catch (e) {
    newError.value = message(e);
  }
}

// Settings: the part open, or null. With no server yet, as at first
// launch, it opens at the server.
const settings = ref<"server" | "github" | "appearance" | null>(null);
void Settings.Server().then((s) => {
  if (!s) settings.value = "server";
});
const settingsAlert = computed(() =>
  status.value.value && !status.value.value.ok ? "The server can't be reached" : login.error.value ? "GitHub has no token it takes" : "",
);
// Settings opens at what needs you, or at the server.
function openSettings() {
  settings.value = status.value.value?.ok && login.error.value ? "github" : "server";
}
async function saved() {
  await Promise.all([status.reload(), login.reload(), lists.reload()]);
}

// A release installs each new release itself, once it is out. A build
// that is not a release, such as Slipway Dev, only says so.
const installing = ref(false);
const installed = ref("");
const updateNote = ref("");
async function install() {
  const latest = update.value.value!.latest;
  installing.value = true;
  updateNote.value = "";
  try {
    await Updates.Install(latest);
    installed.value = latest;
  } catch (e) {
    updateNote.value = message(e);
  } finally {
    installing.value = false;
  }
}
watch(
  () => update.value.value,
  (u) => {
    if (u?.newer && u.released && !installing.value && installed.value !== u.latest && !updateNote.value) void install();
  },
);
async function relaunch() {
  try {
    await Updates.Relaunch();
  } catch (e) {
    updateNote.value = message(e);
  }
}
</script>

<template>
  <div class="flex h-full">
    <Sidebar
      :projects="projects.value.value ?? []"
      :items="items"
      :runs="runs.value.value"
      :selected="{ repo: selected.repo, key: shownKey }"
      :settings-open="settings !== null"
      :settings-alert="settingsAlert"
      @select="(repo, key) => ((selected = { repo, key }), (picked = true))"
      @new-issue="openNewIssue"
      @settings="openSettings"
    >
      <div v-if="installed" class="flex items-center gap-2 px-2 text-small">
        <span class="min-w-0 flex-1">Slipway {{ installed }} is installed.</span>
        <Button size="xs" variant="outline" @click="relaunch"><RotateCw />Relaunch</Button>
      </div>
      <div v-else-if="update.value.value?.newer" class="flex items-center gap-2 px-2 text-small">
        <span class="min-w-0 flex-1">Slipway {{ update.value.value.latest }} is out.</span>
        <Button size="xs" variant="outline" :disabled="installing" @click="install"><Download />{{ installing ? "Installing…" : "Install" }}</Button>
      </div>
      <p v-if="updateNote" class="px-2 text-small text-muted-foreground">{{ updateNote }}</p>
    </Sidebar>

    <main class="flex min-w-0 flex-1 flex-col">
      <p v-if="listError" class="border-b px-page py-2 text-small text-destructive">{{ listError }}</p>
      <ItemView
        v-if="project"
        :key="`${project.repo}/${shownKey}`"
        class="min-h-0 flex-1"
        :repo="project.repo"
        :base="project.base"
        :project="shownProject"
        :item="item"
        :run="runFor(runs.value.value[project.repo], shownProject)"
        @ran="reloadRuns()"
      />
      <div v-else class="flex h-full flex-col items-center justify-center gap-3 p-8 text-center text-muted-foreground">
        <template v-if="status.value.value?.ok">Loading the server's projects…</template>
        <template v-else>
          <p class="max-w-sm">{{ status.value.value?.error || "Connecting to the server…" }}</p>
          <Button v-if="status.value.value" size="sm" variant="outline" @click="settings = 'server'">Server settings</Button>
        </template>
      </div>
    </main>

    <OutputPanel
      :projects="projects.value.value ?? []"
      :items="items"
      :runs="runs.value.value"
      :looking="selected.repo"
      @ran="reloadRuns()"
    />

    <Dialog :open="!!newIssueFor" @update:open="(o) => !o && (newIssueFor = '')">
      <DialogContent class="sm:max-w-xl">
        <form class="grid gap-4" @submit.prevent="createIssue">
          <DialogHeader>
            <DialogTitle>New issue</DialogTitle>
            <DialogDescription>
              On {{ newIssueFor }}, as you. {{ issueScope ? "A bot picks it up on its next look." : "No bot works an issue with no scope." }}
            </DialogDescription>
          </DialogHeader>
          <label class="grid gap-1.5 text-small">
            <span class="text-muted-foreground">For</span>
            <select v-model="issueScope" aria-label="What the issue is for" class="h-control rounded-md border bg-background px-2">
              <option v-for="o in scopes" :key="o.value" :value="o.value">{{ o.label }}</option>
            </select>
          </label>
          <Input v-model="issueTitle" placeholder="Title" aria-label="Title" autofocus />
          <Textarea v-model="issueBody" class="min-h-36" placeholder="What should happen?" aria-label="Description" />
          <p v-if="newError" class="text-destructive text-small">{{ newError }}</p>
          <DialogFooter>
            <Button type="button" variant="ghost" @click="newIssueFor = ''">Cancel</Button>
            <Button type="submit">Open the issue</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>

    <SettingsDialog
      v-model:open="settings"
      :status="status.value.value"
      :login="login.value.value"
      :login-error="login.error.value"
      @saved="saved"
    />
  </div>
</template>
