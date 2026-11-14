<!--
description: What's coming up, by day: appointments, classes, school events, meetings, deadlines, bills due, time off, deliveries. Under "Today", "Tomorrow", and then dates, each with its time, who it's for, and where. To merge several services into one agenda, put their events together in one computed list.
keywords: agenda, calendar, schedule, upcoming, coming up, this week, next week, today, events, appointments, visits, classes, school events, meetings, deadlines, due dates, time off, vacation, by day, plan, itinerary
example:
  <AgendaWidget title="Coming up" :events="[{ start: '2026-11-03T15:30:00', title: 'Checkup', where: 'Clearwater Pediatrics', who: 'Sam' }, { start: '2026-11-04', title: 'Book fair', where: 'Maple Grove Elementary', who: 'Jo' }]" />
example:
  <AgendaWidget title="Time off" :events="[{ start: '2026-11-24', end: '2026-11-26', title: 'Vacation', detail: 'Approved', tone: 'good' }, { start: '2026-12-22', end: '2026-12-31', title: 'Vacation', detail: 'Pending', tone: 'warning' }]" />
-->
<script setup lang="ts">
import { computed } from "vue";
import type { AgendaEvent } from "../shapes";
import { dayText, instant, today, when } from "../time";
import { toneFill } from "../tone";
import Avatar from "../primitives/Avatar.vue";
import Time from "../primitives/Time.vue";
import Widget from "./Widget.vue";

// Events, as AgendaEvent says, sorted by when they start, under a heading
// for each day. One that started before today and is still going shows
// under Today. limit shows only the first so many, by time. @open, given
// the event, runs when you click one, in place of following its link.
const props = withDefaults(
  defineProps<{
    events: AgendaEvent[];
    limit?: number;
    onOpen?: (event: AgendaEvent) => void;
    empty?: string;
  }>(),
  { empty: "Nothing coming up." },
);

const rows = computed(() =>
  props.events
    .map((event) => ({ event, s: when(event.start), e: event.end === undefined ? null : when(event.end) }))
    .filter((r) => r.s)
    .sort((a, b) => instant(a.s!) - instant(b.s!)),
);
const days = computed(() => {
  const now = today();
  const out: { day: string; rows: typeof rows.value }[] = [];
  for (const r of props.limit ? rows.value.slice(0, props.limit) : rows.value) {
    const day = r.s!.day < now && r.e && r.e.day >= now ? now : r.s!.day;
    const last = out.find((d) => d.day === day);
    if (last) {
      last.rows.push(r);
    } else {
      out.push({ day, rows: [r] });
    }
  }
  return out.sort((a, b) => a.day.localeCompare(b.day));
});
const href = (e: AgendaEvent) => (props.onOpen ? undefined : e.link);
const line = (e: AgendaEvent) => [e.detail, e.where].filter(Boolean).join(" · ");
</script>

<template>
  <Widget :more="limit ? Math.max(0, rows.length - limit) : 0">
    <p v-if="days.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <div v-else class="grid">
      <section v-for="d in days" :key="d.day" class="grid">
        <h4 class="pb-0.5 pt-3 text-small font-medium text-muted-foreground first:pt-0">{{ dayText(d.day) }}</h4>
        <ul class="grid">
          <li
            v-for="({ event, s, e }, i) in d.rows"
            :key="i"
            class="flex min-w-0 items-center gap-3 border-b py-2 last:border-0"
            :class="onOpen && 'cursor-pointer hover:bg-accent/50'"
            @click="onOpen?.(event)"
          >
            <span class="w-16 shrink-0 text-small tabular-nums text-muted-foreground">
              <Time v-if="s!.time" :value="event.start" as="time" />
              <template v-else>All day</template>
            </span>
            <span class="w-0.5 self-stretch rounded-full" :class="toneFill[event.tone ?? 'info']"></span>
            <div class="grid min-w-0 flex-1 gap-0.5">
              <a v-if="href(event)" :href="href(event)" class="truncate hover:underline">{{ event.title }}</a>
              <span v-else class="truncate">{{ event.title }}</span>
              <span v-if="line(event) || (e && e.day !== s!.day)" class="truncate text-small text-muted-foreground">
                <Time v-if="e && e.day !== s!.day" :value="s!.day" :end="e.day" /><template v-if="e && e.day !== s!.day && line(event)"> · </template>{{ line(event) }}
              </span>
            </div>
            <Avatar v-if="event.who" :name="event.who" size="sm" />
          </li>
        </ul>
      </section>
    </div>
  </Widget>
</template>
