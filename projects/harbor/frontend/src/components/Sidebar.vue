<script setup lang="ts">
import { Anchor, LayoutGrid, MessageSquare, Plug, Plus, Settings } from "lucide-vue-next";
import { sql, useQuery } from "@desktop";
import { useServices } from "../services";
import { chat, defaultPanels, open, openChat, openServices, openSettings, place, setPanel, settings, sidePanels } from "../navigation";
import SidePanel from "@ui/components/SidePanel.vue";
import Tip from "@ui/components/Tip.vue";

// A dashboard is marked when a service it used last time needs you to sign in.
const dashboards = useQuery<{ id: number; name: string; needs: number }>(
  () => sql`
    SELECT d.id, d.name, EXISTS (
      SELECT 1 FROM dashboard_services s JOIN sign_ins_needed n ON n.service = s.service
      WHERE s.dashboard_id = d.id) AS needs
    FROM dashboards d ORDER BY d.name COLLATE NOCASE`,
);

// The newest first. A chat is named after its first message.
const chats = useQuery<{ id: string; title: string }>(
  () => sql`SELECT id, title FROM chats ORDER BY updated_at DESC LIMIT 30`,
);
const { needYou } = useServices();
const panels = sidePanels();

const isOpen = (kind: string, id?: number) =>
  place.value?.kind === kind && (id === undefined || ("id" in place.value && place.value.id === id));

const item =
  "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sidebar-foreground hover:bg-sidebar-accent";
const selected = "bg-sidebar-accent font-medium text-sidebar-accent-foreground";
const heading = "flex items-center justify-between px-2 py-1 text-small font-medium text-muted-foreground";
const add = "grid size-5 place-items-center rounded hover:bg-sidebar-accent";
</script>

<template>
  <SidePanel
    side="left"
    :panel="panels.sidebar"
    :other="panels.agent"
    :initial="defaultPanels.sidebar"
    element="nav"
    landmark="Harbor"
    label="the sidebar"
    :icon="Anchor"
    :minimum="180"
    shortcut="b"
    :alert="needYou.length ? 'A service needs you to sign in' : undefined"
    @save="(p) => setPanel('sidebar', p)"
  >
    <template #title>Harbor</template>
    <div class="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-2 py-3">
      <section aria-label="Dashboards">
        <header :class="heading">
          <h2>Dashboards</h2>
          <Tip label="New dashboard">
            <button type="button" :class="add" aria-label="New dashboard" @click="open('dashboard', 0)">
              <Plus class="size-3.5" />
            </button>
          </Tip>
        </header>
        <ul>
          <li v-for="d in dashboards" :key="d.id">
            <button
              type="button"
              :class="[item, isOpen('dashboard', d.id) && selected]"
              :aria-current="isOpen('dashboard', d.id) ? 'page' : undefined"
              @click="open('dashboard', d.id)"
            >
              <LayoutGrid class="size-4 shrink-0 text-muted-foreground" />
              <span class="min-w-0 flex-1 truncate">{{ d.name }}</span>
              <template v-if="d.needs">
                <span class="size-1.5 shrink-0 rounded-full bg-warning" title="A service it uses needs you to sign in"></span>
                <span class="sr-only">(a service it uses needs you to sign in)</span>
              </template>
            </button>
          </li>
        </ul>
        <p v-if="dashboards.length === 0" class="px-2 py-1 text-muted-foreground">No dashboards yet.</p>
      </section>

      <section aria-label="Chats">
        <header :class="heading">
          <h2>Chats</h2>
          <Tip label="New chat">
            <button type="button" :class="add" aria-label="New chat" @click="openChat()">
              <Plus class="size-3.5" />
            </button>
          </Tip>
        </header>
        <ul>
          <li v-for="c in chats" :key="c.id">
            <button
              type="button"
              :class="[item, chat === c.id && selected]"
              @click="openChat(c.id)"
            >
              <MessageSquare class="size-4 shrink-0 text-muted-foreground" />
              <span class="min-w-0 flex-1 truncate">{{ c.title || "New chat" }}</span>
            </button>
          </li>
        </ul>
        <p v-if="chats.length === 0" class="px-2 py-1 text-muted-foreground">No chats yet.</p>
      </section>
    </div>

    <footer class="grid gap-0.5 border-t p-2">
      <button
        type="button"
        :class="[item, isOpen('services') && selected]"
        :aria-description="needYou.length ? 'A service needs you to sign in' : undefined"
        @click="openServices()"
      >
        <Plug class="size-4 text-muted-foreground" /><span class="flex-1">Services</span>
        <span v-if="needYou.length" class="size-1.5 shrink-0 rounded-full bg-warning" aria-hidden="true"></span>
      </button>
      <button type="button" :class="[item, settings && selected]" @click="openSettings()">
        <Settings class="size-4 text-muted-foreground" />Settings
      </button>
    </footer>
  </SidePanel>
</template>
