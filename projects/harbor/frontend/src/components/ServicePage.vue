<script setup lang="ts">
import { MessageSquare, Pencil } from "lucide-vue-next";
import { Button } from "@ui/components/button";
import { computed, ref } from "vue";
import { sql, useQuery } from "@desktop";
import { Documents, Tools } from "@backend";
import { type Manifest, takesInput } from "../manifest";
import { askAgent, open, openServices } from "../navigation";
import DeleteButton from "@ui/components/DeleteButton.vue";
import EffectBadge from "./EffectBadge.vue";
import SignInStatus from "./SignInStatus.vue";
import ToolRunner from "./ToolRunner.vue";
import PageHeader from "./PageHeader.vue";
import IconButton from "@ui/components/IconButton.vue";
import ServiceIcon from "./ServiceIcon.vue";

const props = defineProps<{ id: number }>();

const rows = useQuery<{ name: string; manifest: string | null; error: string | null }>(
  () => sql`SELECT name, manifest, manifest_error AS error FROM services WHERE id = ${props.id}`,
);
const service = computed(() => (rows.value[0]?.manifest ? (JSON.parse(rows.value[0].manifest) as Manifest) : null));
// A service whose file Harbor could not read has no manifest, only the error.
const unread = computed(() => (rows.value[0] && !rows.value[0].manifest ? rows.value[0] : null));
// Where to sign in: the login path on the service's site, as the engine
// opens it. A url with a path of its own, such as /accounts, does not prefix it.
const siteURL = (path: string) => new URL(path, service.value!.url).href;
const loginURL = computed(() => service.value?.login && siteURL(service.value.login.url));

// A tool with an input runs from the Run beside its box; one with no input
// runs from the row's Run, which starts the runner through its ref.
const runners = new Map<string, { run: () => void }>();
const running = ref(new Set<string>());
function setRunner(name: string, el: unknown) {
  if (el) {
    runners.set(name, el as { run: () => void });
  } else {
    runners.delete(name);
  }
}
function setRunning(name: string, is: boolean) {
  const next = new Set(running.value);
  if (is) {
    next.add(name);
  } else {
    next.delete(name);
  }
  running.value = next;
}
const signInState = ref("");

// Shows the service's tab in Harbor's window until you are done.
const browseProblem = ref("");
async function browse() {
  browseProblem.value = "";
  try {
    await Tools.Browse(service.value!.name);
  } catch (e) {
    browseProblem.value = e instanceof Error ? e.message : String(e);
  }
}

async function remove() {
  await Documents.DeleteService(props.id);
  openServices();
}
</script>

<template>
  <template v-if="service">
    <PageHeader :title="service.title" back @back="openServices">
      <template #icon><ServiceIcon :url="service.url" /></template>
      <Button size="sm" variant="ghost" @click="browse">Open in Harbor's browser</Button>
      <IconButton label="Edit" @click="open('service', id, true)"><Pencil /></IconButton>
      <DeleteButton :what="service.title" size="icon" @confirm="remove" />
    </PageHeader>
    <div class="min-h-0 flex-1 overflow-y-auto">
      <article class="grid max-w-3xl gap-3 p-page">
        <p class="text-muted-foreground"><code class="font-mono text-body">{{ service.name }}</code> · {{ service.url }}</p>
        <p v-if="browseProblem" class="text-destructive" role="alert">{{ browseProblem }}</p>
        <p v-if="service.description">{{ service.description }}</p>
        <p v-if="service.login && signInState !== 'signed-in' && signInState !== 'signing-in'" class="text-muted-foreground">Sign in at {{ loginURL }}</p>
        <SignInStatus v-if="service.login" :key="service.name" :service="service" @state="(s) => (signInState = s)" />

        <section aria-label="Tools" class="mt-4">
          <h2 class="mb-2 font-semibold">Tools</h2>
          <ul class="divide-y border-y">
            <li v-for="tool in service.tools" :key="tool.name" class="py-2">
              <div class="flex items-center gap-2">
                <code class="font-mono text-body">{{ tool.name }}</code>
                <span v-if="tool.title">{{ tool.title }}</span>
                <EffectBadge :tool="tool" />
                <Button
                  v-if="!takesInput(tool.inputSchema)"
                  size="xs"
                  variant="outline"
                  class="ml-auto"
                  :disabled="running.has(tool.name)"
                  :aria-label="`Run ${tool.name}`"
                  @click="runners.get(tool.name)?.run()"
                >
                  {{ running.has(tool.name) ? "Running…" : "Run" }}
                </Button>
              </div>
              <p class="mt-1 text-muted-foreground">{{ tool.description }}</p>
              <p v-if="tool.login" class="mt-1 text-muted-foreground">
                Before it runs, the site may want you to confirm it's you at {{ siteURL(tool.login.url) }}.
              </p>
              <ToolRunner
                :ref="(el) => setRunner(tool.name, el)"
                :service="service.name"
                :tool="tool"
                @running="(is) => setRunning(tool.name, is)"
              />
            </li>
          </ul>
          <p v-if="service.tools.length === 0" class="text-muted-foreground">This service has no tools yet.</p>
        </section>
      </article>
    </div>
  </template>
  <template v-else-if="unread">
    <PageHeader :title="unread.name" back @back="openServices">
      <IconButton label="Edit" @click="open('service', id, true)"><Pencil /></IconButton>
      <DeleteButton :what="unread.name" size="icon" @confirm="remove" />
    </PageHeader>
    <div class="min-h-0 flex-1 overflow-y-auto">
      <article class="grid max-w-3xl gap-3 p-page">
        <p class="whitespace-pre-wrap text-destructive" role="alert">Couldn't read this service's file: {{ unread.error }}</p>
        <Button size="sm" variant="outline" class="justify-self-start" @click="askAgent(`Fix this service's file. Harbor could not read it:\n\n${unread.error}`)">
          <MessageSquare />Ask the agent to fix it
        </Button>
      </article>
    </div>
  </template>
</template>
