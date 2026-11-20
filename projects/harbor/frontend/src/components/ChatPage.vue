<script setup lang="ts">
import { computed, nextTick, ref, watch } from "vue";
import { ArrowUp, Square } from "lucide-vue-next";
import { sql, useQuery } from "@desktop";
import { Chats } from "@backend";
import { Button } from "@ui/components/button";
import { chatItems, describe, type ToolStep } from "../chat";
import { openChat, openSettings, prefill, settings } from "../navigation";
import { useProviders } from "../providerStatus";
import { noProviderError } from "../providers";
import ChatTranscript from "./ChatTranscript.vue";
import IconButton from "@ui/components/IconButton.vue";
import Markdown from "@ui/components/Markdown.vue";

// A chat, in the agent's panel. id "" is a new chat, saved when you send
// its first message. looking is what you are looking at, which each message
// tells the agent.
const props = defineProps<{ id: string; looking: string | null }>();

const chats = useQuery<{ working: number; draft: string; error: string }>(() =>
  props.id ? sql`SELECT working, draft, error FROM chats WHERE id = ${props.id}` : null,
);
const messages = useQuery<{ seq: number; role: string; parts: string }>(() =>
  props.id ? sql`SELECT seq, role, parts FROM messages WHERE chat_id = ${props.id} ORDER BY seq` : null,
);
const chat = computed(() => chats.value[0]);
const items = computed(() => chatItems(messages.value));
// Why the agent cannot run yet, if it cannot.
const { status: providerStatus } = useProviders();
const working = computed(() => chat.value?.working === 1);
// The last turn stopped because there is no provider: that is setup waiting
// on you, not a failure. The panel says it once, under the message, in its
// needs-you voice — the footer's words, with the button right there as the
// way to Settings — and the footer steps aside.
const waiting = computed(() => providerStatus.value !== "" && chat.value?.error === noProviderError);

const message = ref("");
const problem = ref("");
const sending = ref(false);

async function send() {
  const text = message.value.trim();
  if (!text || sending.value || working.value) {
    return;
  }
  sending.value = true;
  problem.value = "";
  const looking = props.looking ?? "";
  try {
    if (props.id) {
      await Chats.Send(props.id, text, looking);
      message.value = "";
    } else {
      const id = await Chats.NewChat();
      await Chats.Send(id, text, looking);
      openChat(id);
    }
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e);
  } finally {
    sending.value = false;
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
    e.preventDefault();
    void send();
  }
}

async function answer(step: ToolStep, allowed: boolean) {
  problem.value = "";
  try {
    await Chats.Answer(props.id, step.id, allowed);
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e);
  }
}

// A message another page asked the agent, waiting for you to send it.
const box = ref<HTMLTextAreaElement | null>(null);
watch(
  prefill,
  async (text) => {
    if (text) {
      message.value = text;
      prefill.value = "";
      await nextTick();
      box.value?.focus();
    }
  },
  { immediate: true },
);

// Follow the chat as it grows.
const log = ref<HTMLElement | null>(null);
watch([() => messages.value.length, () => chat.value?.draft], async () => {
  await nextTick();
  log.value?.scrollTo({ top: log.value.scrollHeight });
});
</script>

<template>
  <div ref="log" class="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto p-4" aria-label="Chat" role="log">
    <ChatTranscript :items="items" :working="working">
      <template #asking="{ step }">
        <div class="my-2 grid gap-2">
          <span>Let the agent {{ describe(step, "ask").charAt(0).toLowerCase() + describe(step, "ask").slice(1) }}?</span>
          <div class="flex gap-2">
            <Button size="sm" :disabled="working" @click="answer(step, true)">Allow</Button>
            <Button size="sm" variant="outline" :disabled="working" @click="answer(step, false)">Don't allow</Button>
          </div>
        </div>
      </template>
    </ChatTranscript>

    <Markdown v-if="chat?.draft" :text="chat.draft" class="leading-relaxed" aria-label="Agent" />
    <p v-else-if="working" class="m-0 text-muted-foreground" role="status">Working…</p>
    <div v-if="waiting" class="grid gap-2">
      <p class="need m-0" role="alert">{{ providerStatus }}</p>
      <div>
        <Button size="sm" variant="outline" @click="openSettings('providers')">Set up a provider</Button>
      </div>
    </div>
    <p v-else-if="chat?.error" class="m-0 whitespace-pre-wrap text-destructive" role="alert">{{ chat.error }}</p>
    <p v-if="problem" class="m-0 whitespace-pre-wrap text-destructive" role="alert">{{ problem }}</p>
  </div>

  <!-- Settings says it too, in Providers, so it is not said twice. -->
  <p v-if="providerStatus && !settings && !waiting" class="setup flex items-center gap-3 border-t px-4 py-2 text-muted-foreground" role="status">
    <span class="flex-1">{{ providerStatus }}</span>
    <Button size="sm" variant="outline" @click="openSettings('providers')">Set up a provider</Button>
  </p>
  <form class="grid shrink-0 gap-2 border-t p-3" @submit.prevent="send">
    <div class="flex items-end gap-2 rounded-lg border bg-card p-2 focus-within:ring-2 focus-within:ring-ring/40">
      <textarea
        ref="box"
        v-model="message"
        aria-label="Message"
        placeholder="Ask anything…"
        rows="3"
        class="min-w-0 flex-1 resize-none bg-transparent outline-none placeholder:text-muted-foreground"
        @keydown="onKey"
      ></textarea>
      <IconButton v-if="working" type="button" variant="outline" label="Stop" @click="Chats.Stop(id)"><Square /></IconButton>
      <IconButton v-else type="submit" variant="default" label="Send" :disabled="sending || !message.trim()"><ArrowUp /></IconButton>
    </div>
  </form>
</template>

<style scoped>
/* Setup waiting on you is the app's needs-you amber, as the Signed out dot
   and a run waiting on a sign-in use it; only a real failure is red. */
.need {
  color: var(--warning);
}
</style>
