<!--
description: Messages: email, a patient portal's messages from the care team, a school's messages, chat mentions. Each shows who sent it, its subject, a line of it, and how long ago, unread ones in bold; the title shows how many are unread. Clicking one can run an action, such as marking it read.
keywords: inbox, email, mail, messages, message center, portal messages, care team, doctor, nurse, school messages, teacher, mentions, dms, direct messages, chat, notifications, unread, conversations, mark read
example:
  <InboxWidget title="Messages" :messages="[{ from: 'Dr. Anika Patel', subject: 'Lab results are ready', preview: 'Everything looks normal.', at: '2026-11-01T09:12:00', unread: true }, { from: 'Front desk', subject: 'Appointment reminder', preview: 'Checkup on Thursday at 3:30.', at: '2026-10-30T16:40:00' }]" />
example:
  <InboxWidget title="Mentions" :messages="[{ from: 'Priya', subject: '#eng', preview: '@you can you check the deploy?', at: '2026-11-01T10:02:00', unread: true }]" action="Mark all read" />
-->
<script setup lang="ts">
import { computed } from "vue";
import type { Message } from "../shapes";
import Avatar from "../primitives/Avatar.vue";
import Item from "../primitives/Item.vue";
import Time from "../primitives/Time.vue";
import Widget from "./Widget.vue";

// Messages, as Message says, unread ones counted beside the title. A
// message without a subject shows its preview in its place. limit shows
// only the first so many. @open, given the message, runs when you click
// one, in place of following its link.
const props = withDefaults(
  defineProps<{
    messages: Message[];
    limit?: number;
    onOpen?: (message: Message) => void;
    empty?: string;
  }>(),
  { empty: "No messages." },
);

const shown = computed(() => (props.limit ? props.messages.slice(0, props.limit) : props.messages));
const unread = computed(() => props.messages.filter((m) => m.unread).length || undefined);
const href = (m: Message) => (props.onOpen ? undefined : m.link);
</script>

<template>
  <Widget :count="unread" :more="limit ? Math.max(0, messages.length - limit) : 0">
    <p v-if="shown.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <ul v-else class="grid">
      <li
        v-for="(m, i) in shown"
        :key="i"
        class="border-b last:border-0"
        :class="onOpen && 'cursor-pointer hover:bg-accent/50'"
        @click="onOpen?.(m)"
      >
        <Item :link="href(m)" :unread="m.unread">
          <template #leading>
            <Avatar :name="m.from" :src="m.avatar" class="mt-0.5 self-start" />
          </template>
          <template #label>
            <span class="min-w-0 flex-1 truncate" :class="m.unread ? 'font-semibold' : 'font-medium'">{{ m.from }}</span>
            <Time v-if="m.at" :value="m.at" as="relative" class="shrink-0 text-caption text-muted-foreground" />
          </template>
          <template #detail>
            <a v-if="href(m)" :href="href(m)" class="block truncate text-body text-foreground hover:underline" :class="m.unread && 'font-medium'">{{
              m.subject ?? m.preview
            }}</a>
            <span v-else class="block truncate text-body text-foreground" :class="m.unread && 'font-medium'">{{ m.subject ?? m.preview }}</span>
          </template>
          <p v-if="m.subject && m.preview" class="truncate text-small text-muted-foreground">{{ m.preview }}</p>
        </Item>
      </li>
    </ul>
  </Widget>
</template>
