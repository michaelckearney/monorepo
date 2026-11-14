<!--
description: Things to do: issues, tickets, homework and assignments, chores, to-dos. Each shows its title, key, who it's for, when it's due (amber when close, red when late), its priority, and its status as a coloured badge; the title shows how many. A button on each can run an action, such as marking it done.
keywords: tasks, to do, todo, to-dos, issues, open issues, open tickets, tickets, bugs, assignments, homework, schoolwork, coursework, chores, work, my issues, assigned to me, due, overdue, missing, status, priority, backlog, project, mark done
example:
  <TasksWidget title="Your issues" :tasks="[{ key: 'WEB-142', title: 'Fix the login redirect', status: 'In review', priority: 'High', link: 'https://example.com/WEB-142' }, { key: 'WEB-150', title: 'Update the pricing page', status: 'In progress', priority: 'Medium' }]" />
example:
  <TasksWidget title="Schoolwork" :tasks="[{ group: 'Sam', title: 'Fractions worksheet', status: 'Missing', detail: 'Math 5' }, { group: 'Jo', title: 'Read chapter 4', status: 'Assigned', tone: 'neutral', due: '2026-11-05', detail: 'English' }]" />
example:
  <TasksWidget title="Chores" :tasks="[{ title: 'Take out the bins', status: 'Open' }, { title: 'Water the plants', status: 'Closed' }]" :done="['Closed']" hide-done act="Done" />
-->
<script setup lang="ts">
import { computed, ref } from "vue";
import type { Task } from "../shapes";
import { due, doneBy, priorityToneBy } from "../tasks";
import { guessTone, toneText, type Tone } from "../tone";
import Toggle from "../controls/Toggle.vue";
import Badge from "../primitives/Badge.vue";
import Button from "../primitives/Button.vue";
import Item from "../primitives/Item.vue";
import ItemList from "../primitives/ItemList.vue";
import Widget from "./Widget.vue";

// Tasks, as Task says. done is the statuses that mean done, if their words
// do not say so ("Closed", "Merged"): a done task is never late, and has
// no act button. hideDone adds a switch, on at first, that hides them.
// priorityTone maps a priority to a tone, where the words do not say it
// ("P2": "warning"). limit shows only the first so many. act is a button on
// each task, and @act, given the task, runs when you press it.
const props = withDefaults(
  defineProps<{
    tasks: Task[];
    done?: string[];
    hideDone?: boolean;
    priorityTone?: Partial<Record<string, Tone>>;
    limit?: number;
    act?: string;
    onAct?: (task: Task) => void;
    empty?: string;
  }>(),
  { empty: "Nothing to do." },
);

const hiding = ref(true);
const isDone = (t: Task) => doneBy(props.done)(t.status);
const all = computed(() => (props.hideDone && hiding.value ? props.tasks.filter((t) => !isDone(t)) : props.tasks));
const shown = computed(() => (props.limit ? all.value.slice(0, props.limit) : all.value));
const toneOf = (t: Task) => t.tone ?? guessTone(t.status);
const meta = (t: Task) => ({ due: due(t.due, isDone(t)), rest: [t.who, t.detail].filter(Boolean).join(" · ") });
const hasGroup = computed(() => props.tasks.some((t) => t.group));
</script>

<template>
  <Widget :count="all.length || undefined" :more="limit ? Math.max(0, all.length - limit) : 0">
    <template v-if="hideDone" #controls><Toggle v-model="hiding" label="Hide done" /></template>
    <p v-if="shown.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <ItemList v-else :items="shown" :group="hasGroup ? 'group' : undefined">
      <template #item="{ item }">
        <Item :link="item.link">
          <template #label>
            <span v-if="item.key" class="shrink-0 font-mono text-small text-muted-foreground">{{ item.key }}</span>
            <a v-if="item.link" :href="item.link" class="truncate hover:underline">{{ item.title }}</a>
            <span v-else class="truncate">{{ item.title }}</span>
          </template>
          <template v-if="meta(item).due || meta(item).rest" #detail>
            <span v-if="meta(item).due" :class="meta(item).due!.tone !== 'neutral' && ['font-medium', toneText[meta(item).due!.tone]]">{{
              meta(item).due!.text
            }}</span>
            <template v-if="meta(item).due && meta(item).rest"> · </template>{{ meta(item).rest }}
          </template>
          <template v-if="item.status || item.priority || act" #trailing>
            <span v-if="item.priority" class="text-caption" :class="toneText[priorityToneBy(priorityTone)(item.priority)]">{{ item.priority }}</span>
            <Badge v-if="item.status" :tone="toneOf(item)">{{ item.status }}</Badge>
            <Button v-if="act && !isDone(item)" size="sm" variant="secondary" @click="onAct?.(item)">{{ act }}</Button>
          </template>
        </Item>
      </template>
    </ItemList>
  </Widget>
</template>
