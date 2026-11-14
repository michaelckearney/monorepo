<script setup lang="ts">
import { computed } from "vue";
import { MessageSquare, Plus } from "lucide-vue-next";
import { sql, useQuery } from "@desktop";
import { Chats } from "@backend";
import { chat, defaultPanels, openChat, place, setPanel, sidePanels } from "../navigation";
import ChatPage from "./ChatPage.vue";
import DeleteButton from "./DeleteButton.vue";
import IconButton from "@ui/components/IconButton.vue";
import SidePanel from "@ui/components/SidePanel.vue";

// The agent, beside whatever you are looking at, which it is told about.
const panels = sidePanels();
const chats = useQuery<{ title: string }>(() => (chat.value ? sql`SELECT title FROM chats WHERE id = ${chat.value}` : null));

const at = computed(() => {
  const p = place.value;
  return p && "id" in p && p.id !== 0 ? p : null;
});
const dashboard = useQuery<{ name: string }>(() =>
  at.value?.kind === "dashboard" ? sql`SELECT name FROM dashboards WHERE id = ${at.value.id}` : null,
);
const service = useQuery<{ name: string }>(() =>
  at.value?.kind === "service" ? sql`SELECT name FROM services WHERE id = ${at.value.id}` : null,
);

/** What you are looking at, in the words the agent is told. */
const looking = computed(() => {
  const editing = at.value?.editing ? " in the code editor" : "";
  if (dashboard.value[0]) {
    return `the dashboard "${dashboard.value[0].name}"${editing}`;
  }
  if (service.value[0]) {
    return `the service "${service.value[0].name}"${editing}`;
  }
  if (place.value?.kind === "services") {
    return "the list of services";
  }
  return null;
});

async function remove() {
  await Chats.Delete(chat.value);
  openChat();
}
</script>

<template>
  <SidePanel
    side="right"
    :panel="panels.agent"
    :other="panels.sidebar"
    :initial="defaultPanels.agent"
    element="aside"
    landmark="Agent panel"
    label="the agent"
    :icon="MessageSquare"
    :minimum="300"
    shortcut="j"
    @save="(p) => setPanel('agent', p)"
  >
    <template #title>{{ chats[0]?.title || "New chat" }}</template>
    <template #actions>
      <DeleteButton v-if="chat" what="this chat" size="icon" @confirm="remove" />
      <IconButton label="New chat" name="Start a new chat" @click="openChat()"><Plus /></IconButton>
    </template>
    <ChatPage :key="chat" :id="chat" :looking="looking" />
  </SidePanel>
</template>
