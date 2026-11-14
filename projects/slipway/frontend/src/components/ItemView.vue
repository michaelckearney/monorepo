<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from "vue";
import {
  Bot,
  CircleCheck,
  CircleDashed,
  CircleX,
  ExternalLink,
  GitBranch,
  GitPullRequest,
  Loader2,
  Play,
  ScrollText,
  Terminal,
} from "lucide-vue-next";
import { sql, useQuery } from "@desktop";
import { GitHub, Server } from "@backend";
import type { Conversation, Run } from "@backend";
import { Button } from "@ui/components/button";
import { Textarea } from "@ui/components/textarea";
import { Popover, PopoverContent, PopoverTrigger } from "@ui/components/popover";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@ui/components/dialog";
import IconButton from "@ui/components/IconButton.vue";
import Markdown from "@ui/components/Markdown.vue";
import BotTurn from "./BotTurn.vue";
import { images } from "../images";
import { status, type Item } from "../items";
import { ago, message, usePoll } from "../poll";
import { expand } from "../preferences";
import { busy, runHere, runsHere } from "../runs";
import { took, turns, type Line, type Turn } from "../turns";

// One item: the issue, its pull request, and the bot's thread, as one
// timeline in the order things happened, the bot's turns among the
// comments. With no item, it is the project's default branch, which has
// only its run. project is the project it runs: the item's, or the default
// branch's, or "" for whichever runs.
const props = defineProps<{ repo: string; base: string; project: string; item: Item | null; run: Run | null }>();
const emit = defineEmits<{ ran: [] }>();

const issueNo = computed(() => props.item?.issue?.number ?? props.item?.thread?.number ?? 0);
const prNo = computed(() => props.item?.pr?.number ?? props.item?.thread?.pr ?? 0);
const issue = usePoll(
  () => (issueNo.value ? GitHub.Conversation(props.repo, issueNo.value, false) : Promise.resolve(null)),
  null as Conversation | null,
  30_000,
  [issueNo],
);
const pr = usePoll(
  () => (prNo.value ? GitHub.Conversation(props.repo, prNo.value, true) : Promise.resolve(null)),
  null as Conversation | null,
  30_000,
  [prNo],
);
const title = computed(() => props.item?.title || issue.value.value?.title || pr.value.value?.title || "");
const url = computed(() => (issue.value.value ?? pr.value.value)?.url ?? `https://github.com/${props.repo}`);

// What the bot did, from the server's live stream, which the backend writes
// into the app's database as it arrives.
const thread = computed(() => props.item?.thread ?? null);
const lines = useQuery<Line>(() =>
  thread.value
    ? sql`SELECT id, time, kind, text FROM lines WHERE repo = ${props.repo} AND number = ${thread.value.number} ORDER BY id DESC LIMIT 3000`
    : null,
);
const botTurns = computed(() => turns([...(lines.value ?? [])].reverse()));
onMounted(() => {
  if (thread.value) void Server.Open(props.repo, thread.value.number).catch(() => {});
});

// The issue's and the pull request's comments and the bot's turns, in the
// order they happened, so the whole thing reads as one.
type Entry =
  | { key: string; kind: "comment"; at: string; author: string; body: string; where: string; url: string; number: number; first?: boolean }
  | { key: string; kind: "event"; at: string; text: string; url: string }
  | { key: string; kind: "turn"; at: string; turn: Turn; working: boolean };
const timeline = computed<Entry[]>(() => {
  const out: Entry[] = [];
  const i = issue.value.value;
  const p = pr.value.value;
  if (i) {
    out.push({ key: "issue", kind: "comment", at: String(i.createdAt), author: i.author, body: i.body || "_No description._", where: "", url: i.url, number: i.number, first: true });
    (i.comments ?? []).forEach((c, n) =>
      out.push({ key: `i${n}`, kind: "comment", at: String(c.createdAt), author: c.author, body: c.body, where: "", url: c.url, number: i.number }),
    );
  }
  if (p) {
    const by = isBot(p.author) ? "The bot" : p.author;
    out.push({ key: "pr", kind: "event", at: String(p.createdAt), text: `${by} opened ${p.draft ? "draft " : ""}pull request #${p.number} from ${p.branch}`, url: p.url });
    if (p.body && !i) out.push({ key: "prbody", kind: "comment", at: String(p.createdAt), author: p.author, body: p.body, where: "", url: p.url, number: p.number, first: true });
    (p.comments ?? []).forEach((c, n) =>
      out.push({ key: `p${n}`, kind: "comment", at: String(c.createdAt), author: c.author, body: c.body, where: i ? `on #${p.number}` : "", url: c.url, number: p.number }),
    );
  }
  const last = botTurns.value.length - 1;
  botTurns.value.forEach((t, n) => out.push({ key: t.key, kind: "turn", at: t.start, turn: t, working: n === last && thread.value?.state === "running" }));
  return out.sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
});
// A GitHub App's login ends in [bot], as the backend writes it.
const isBot = (author: string) => author.endsWith("[bot]");

// The issue is folded once there is more below it: you are here to follow
// what happened since, not to read the spec again.
const unfolded = ref(false);
const folds = (e: Entry) => e.kind === "comment" && !!e.first && !unfolded.value && timeline.value.length > 2 && e.body.length > 600;

// Keep the newest in view: at the end when it opens, and as lines arrive
// while you are at the end.
const scroller = ref<HTMLElement | null>(null);
let placed = false;
watch(
  () => [timeline.value.length, lines.value?.length ?? 0],
  async () => {
    const el = scroller.value;
    const atEnd = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 120;
    if (placed && !atEnd) return;
    await nextTick();
    scroller.value?.scrollTo({ top: scroller.value.scrollHeight });
    if (timeline.value.length) placed = true;
  },
);

// Replies go on the issue, where the bot reads them; with no issue, on the
// pull request.
const reply = ref("");
const sending = ref(false);
const replyError = ref("");
async function send() {
  if (!reply.value.trim()) return;
  replyError.value = "";
  sending.value = true;
  try {
    await GitHub.Reply(props.repo, issueNo.value || prNo.value, reply.value);
    reply.value = "";
    await Promise.all([issue.reload(), pr.reload()]);
  } catch (e) {
    replyError.value = message(e);
  } finally {
    sending.value = false;
  }
}

// The bot.
const st = computed(() => (props.item ? status(props.item) : "none"));
const botError = ref("");
async function botAct(what: "stop" | "retry") {
  botError.value = "";
  try {
    if (what === "stop") await Server.Stop(props.repo, thread.value!.number);
    else await Server.Retry(props.repo, thread.value!.number);
  } catch (e) {
    botError.value = message(e);
  }
}
const log = ref("");
const logOpen = ref(false);
async function showLog() {
  try {
    log.value = await Server.Log(props.repo, thread.value!.number);
    logOpen.value = true;
  } catch (e) {
    botError.value = message(e);
  }
}

// This computer.
const runPR = computed(() => (props.item ? (props.item.pr?.number ?? -1) : 0));
const canRun = computed(() => runPR.value >= 0);
const here = computed(() => runsHere(props.run, runPR.value, props.project));
const starting = computed(() => busy(props.run));
const runError = ref("");
async function startRun() {
  runError.value = "";
  try {
    await runHere(props.repo, runPR.value, !!props.item?.pr?.fork, props.project);
  } catch (e) {
    runError.value = message(e);
  }
  emit("ran");
}

const checks = computed(() => props.item?.pr?.checks ?? "");
const checkWords: Record<string, string> = { passed: "Checks passed", failed: "Checks failed", pending: "Checks running", skipped: "Checks skipped" };
</script>

<template>
  <section class="flex h-full min-w-0 flex-col">
    <header class="flex h-14 shrink-0 items-center gap-3 border-b px-page">
      <div class="flex min-w-0 flex-1 items-baseline gap-2">
        <h1 v-if="item" class="min-w-0 truncate text-title font-semibold tracking-tight" :title="`#${item.number} ${title}`">
          <span class="font-normal text-muted-foreground">#{{ item.number }}</span> {{ title }}
        </h1>
        <h1 v-else class="min-w-0 truncate text-title font-semibold tracking-tight">
          {{ project }} <span class="font-mono font-normal text-muted-foreground">{{ base }}</span>
        </h1>
      </div>
      <div class="flex shrink-0 items-center gap-1.5">
        <Button v-if="item?.pr" size="sm" variant="outline" as="a" :href="`${item.pr.url}/files`">Files changed</Button>
        <Button v-if="thread?.state === 'running'" size="sm" variant="outline" @click="botAct('stop')">Stop turn</Button>
        <Button v-else-if="thread" size="sm" variant="outline" @click="botAct('retry')">Another turn</Button>
        <IconButton v-if="thread" label="The bot's whole log" @click="showLog"><ScrollText /></IconButton>
        <IconButton label="Open on GitHub" as="a" :href="url"><ExternalLink /></IconButton>
      </div>
    </header>

    <!-- What the bot, the pull request, and this computer are doing, in one line -->
    <div class="text-muted-foreground flex shrink-0 flex-wrap items-center gap-x-4 gap-y-1.5 border-b px-page py-2 text-small">
      <template v-if="item">
        <span v-if="!thread && item.issue" class="flex items-center gap-1.5"><Bot class="size-3.5" />No bot thread yet</span>
        <span v-else-if="st === 'working'" class="flex items-center gap-1.5 font-medium text-foreground">
          <span class="bg-success size-1.5 animate-pulse rounded-full" />Bot working · {{ took(String(thread!.started), new Date().toISOString()) }}
        </span>
        <span v-else-if="st === 'needs-you'" class="flex items-center gap-1.5 font-medium text-foreground">
          <span class="bg-warning size-1.5 rounded-full" />Waiting for you
        </span>
        <span v-else-if="thread" class="flex items-center gap-1.5">
          <span class="bg-muted-foreground size-1.5 rounded-full" />
          {{ thread.waiting ? `${thread.waiting} new event${thread.waiting === 1 ? "" : "s"} for its next turn` : "Bot idle" }}
        </span>

        <a v-if="item.pr" :href="item.pr.url" class="hover:text-foreground flex items-center gap-1.5">
          <GitPullRequest class="size-3.5" />
          {{ item.pr.draft ? "Draft PR" : "PR" }} #{{ item.pr.number }} · <span class="font-mono">{{ item.pr.branch }}</span> ·
          <span class="text-success">+{{ item.pr.additions }}</span>
          <span class="text-destructive">−{{ item.pr.deletions }}</span>
          <template v-if="item.pr.fork"> · from a fork</template>
        </a>
        <span v-else-if="item.issue && thread" class="flex items-center gap-1.5"><GitPullRequest class="size-3.5" />No pull request yet</span>

        <Popover v-if="checks">
          <PopoverTrigger class="hover:text-foreground flex items-center gap-1.5">
            <CircleCheck v-if="checks === 'passed'" class="text-success size-3.5" />
            <CircleX v-else-if="checks === 'failed'" class="text-destructive size-3.5" />
            <CircleDashed v-else class="text-warning size-3.5" />
            {{ checkWords[checks] ?? `Checks ${checks}` }}
          </PopoverTrigger>
          <PopoverContent align="start" class="w-80 p-1">
            <a
              v-for="c in pr.value.value?.checkRuns ?? []"
              :key="c.name + c.url"
              :href="c.url || undefined"
              class="hover:bg-accent flex items-center gap-2 rounded-md px-2 py-1.5"
            >
              <CircleCheck v-if="c.state === 'passed'" class="text-success size-3.5 shrink-0" />
              <CircleX v-else-if="c.state === 'failed'" class="text-destructive size-3.5 shrink-0" />
              <CircleDashed v-else class="text-muted-foreground size-3.5 shrink-0" />
              <span class="min-w-0 flex-1 truncate">{{ c.name }}</span>
              <span v-if="c.state !== 'passed'" class="text-muted-foreground text-small">{{ c.state }}</span>
            </a>
            <p v-if="!(pr.value.value?.checkRuns ?? []).length" class="text-muted-foreground px-2 py-1.5">Loading the checks…</p>
          </PopoverContent>
        </Popover>
      </template>

      <template v-if="canRun">
        <span v-if="!here && starting" class="flex items-center gap-1.5">
          <Loader2 class="size-3.5 animate-spin" />{{ run?.phase === "cloning" ? `Cloning ${repo}…` : "Starting…" }}
        </span>
        <span v-else-if="here && run?.state === 'building'" class="flex items-center gap-1.5">
          <Loader2 class="size-3.5 animate-spin" />Building…
        </span>
        <button
          v-else-if="here && run?.state === 'failed'"
          type="button"
          class="flex items-center gap-1.5 text-destructive"
          :title="run.message"
          @click="expand('right')"
        >
          <Terminal class="size-3.5" />Failed to run here
        </button>
        <button v-else-if="here" type="button" class="flex items-center gap-1.5 hover:text-foreground" title="Show its output (⌘J)" @click="expand('right')">
          <Terminal class="size-3.5" />Runs here
        </button>
        <Button v-else size="xs" variant="outline" class="text-foreground ml-auto" :title="run?.running ? `Runs this in place of what ${run.project || 'the project'} runs on this computer now` : 'Run it in a window on this computer'" @click="startRun">
          <Play />Run here
        </Button>
      </template>
      <span v-if="runError || run?.error" class="text-destructive w-full">{{ runError || run?.error }}</span>
      <span v-if="botError" class="text-destructive w-full">{{ botError }}</span>
    </div>

    <!-- The timeline -->
    <template v-if="item">
      <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto">
        <ol class="mx-auto flex max-w-3xl flex-col gap-5 px-page pt-6 pb-8">
          <li v-if="issue.error.value || pr.error.value" class="text-destructive">{{ issue.error.value || pr.error.value }}</li>
          <li v-for="e in timeline" :key="e.key">
            <BotTurn v-if="e.kind === 'turn'" :turn="e.turn" :working="e.working" />
            <div v-else-if="e.kind === 'event'" class="text-muted-foreground flex items-center gap-3 text-small">
              <span class="flex w-6 shrink-0 justify-center"><GitPullRequest class="size-3.5" /></span>
              <span>{{ e.text }} · <a :href="e.url" class="hover:text-foreground">{{ ago(e.at) }}</a></span>
            </div>
            <article v-else class="flex gap-3">
              <span
                class="bg-muted text-sidebar-foreground flex size-6 shrink-0 items-center justify-center rounded-full text-caption font-semibold uppercase"
              >
                <Bot v-if="isBot(e.author)" class="size-3.5" />
                <template v-else>{{ e.author.slice(0, 2) }}</template>
              </span>
              <div class="flex min-w-0 flex-1 flex-col gap-1.5">
                <p class="text-muted-foreground text-small">
                  <strong class="text-foreground font-medium">{{ isBot(e.author) ? "Bot" : e.author }}</strong>
                  {{ e.first ? "opened it" : isBot(e.author) ? "commented on GitHub" : "commented" }}<template v-if="e.where"> {{ e.where }}</template> ·
                  <a :href="e.url" class="hover:text-foreground">{{ ago(e.at) }}</a>
                </p>
                <div class="relative" :class="folds(e) && 'max-h-28 overflow-hidden'">
                  <Markdown :text="e.body" :image="images(repo, e.number)" class="leading-relaxed" />
                  <div v-if="folds(e)" class="from-background/0 to-background absolute inset-x-0 bottom-0 h-12 bg-gradient-to-b" />
                </div>
                <button v-if="folds(e)" type="button" class="text-muted-foreground hover:text-foreground self-start text-small" @click="unfolded = true">
                  Show the whole {{ e.key === "issue" ? "issue" : "description" }}
                </button>
              </div>
            </article>
          </li>
          <li v-if="!timeline.length && (issue.loading.value || pr.loading.value)" class="text-muted-foreground">Loading…</li>
        </ol>
      </div>

      <form class="shrink-0 border-t px-page py-3" @submit.prevent="send">
        <div class="mx-auto flex max-w-3xl flex-col gap-2">
          <Textarea
            v-model="reply"
            class="max-h-40 min-h-16"
            :placeholder="`Reply on #${issueNo || prNo}, as you. The bot reads it on its next turn.`"
            :aria-label="`Reply on #${issueNo || prNo}`"
            @keydown.meta.enter.prevent="send"
          />
          <div class="flex items-center justify-end gap-2">
            <span v-if="replyError" class="mr-auto text-small text-destructive">{{ replyError }}</span>
            <Button type="submit" size="sm" title="⌘↩" :disabled="sending || !reply.trim()">{{ sending ? "Sending…" : "Comment" }}</Button>
          </div>
        </div>
      </form>
    </template>
    <div v-else class="flex flex-1 flex-col items-center justify-center gap-2 p-8 text-muted-foreground">
      <GitBranch class="size-6" />
      <p>{{ project }} on {{ base }}, as it is on GitHub.</p>
    </div>

    <Dialog v-model:open="logOpen">
      <DialogContent class="flex max-h-[85vh] flex-col sm:max-w-4xl">
        <DialogHeader><DialogTitle>The bot's log of #{{ thread?.number }}</DialogTitle></DialogHeader>
        <pre class="bg-muted min-h-0 flex-1 overflow-auto rounded-md p-3 font-mono text-small whitespace-pre-wrap">{{ log || "The log is empty." }}</pre>
      </DialogContent>
    </Dialog>
  </section>
</template>
