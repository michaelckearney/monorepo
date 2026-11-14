<!--
description: Issues, tickets, or tasks as a board, a column for each state (To do, In progress, In review, Done) with its count, each a small card. Dragging a card to another column can run an action that changes its state. One widget the width of the dashboard.
keywords: board, kanban, issue board, task board, sprint board, columns, swimlanes, by status, by state, workflow, issues, tickets, tasks, project, scrum, trello, jira, linear, drag, move
example:
  <BoardWidget title="Board" span="full" :columns="[{ id: 'TODO', title: 'To do' }, { id: 'IN_PROGRESS', title: 'In progress' }, { id: 'DONE', title: 'Done' }]" :cards="[{ key: 'WEB-142', title: 'Fix the login redirect', column: 'TODO', priority: 'High' }, { key: 'WEB-150', title: 'Update the pricing page', column: 'IN_PROGRESS', who: 'Ana' }, { key: 'WEB-133', title: 'Cache the avatars', column: 'DONE' }]" :done="['DONE']" />
example:
  <BoardWidget title="Releases" span="full" :columns="[{ id: 'design', title: 'Design' }, { id: 'build', title: 'Build' }, { id: 'shipped', title: 'Shipped' }]" :cards="[{ key: 'R-12', title: 'Billing export', column: 'design' }, { key: 'R-9', title: 'Dark mode', column: 'shipped', due: '2026-10-01' }, { key: 'R-14', title: 'SSO', column: 'blocked' }]" :done="['shipped']" hide-done />
-->
<script setup lang="ts">
import { computed, ref, watch } from "vue";
import type { BoardCard, BoardColumn } from "../shapes";
import { due, doneBy, priorityToneBy, words } from "../tasks";
import { toneText, type Tone } from "../tone";
import Toggle from "../controls/Toggle.vue";
import Widget from "./Widget.vue";

// Cards, as BoardCard says, in columns, in order. A card whose column is
// not one of them gets a column of its own after them, titled by its id.
// done is the columns that mean done, if their words do not say so: a done
// card is never late. hideDone adds a switch, on at first, that hides
// them. priorityTone maps a priority to a tone, where the words do not say
// it.
//
// @move, given the card and the column's id, makes the cards draggable: a
// card dropped on another column moves there at once, and stays until the
// cards change, as when what it ran has run and the dashboard loads again.
const props = defineProps<{
  cards: BoardCard[];
  columns: BoardColumn[];
  done?: string[];
  hideDone?: boolean;
  priorityTone?: Partial<Record<string, Tone>>;
  onMove?: (card: BoardCard, column: string) => void;
}>();

const isDone = (column: string) => doneBy(props.done)(column);
const hiding = ref(true);

// Where cards were dropped, until the cards change.
const moved = ref(new Map<BoardCard, string>());
watch(
  () => props.cards,
  () => (moved.value = new Map()),
);
const columnOf = (card: BoardCard) => moved.value.get(card) ?? card.column;

const shown = computed(() => (props.hideDone && hiding.value ? props.cards.filter((c) => !isDone(columnOf(c))) : props.cards));
const lanes = computed(() => {
  const listed = props.columns.map((c) => ({ ...c, cards: [] as BoardCard[] }));
  const others: typeof listed = [];
  for (const card of shown.value) {
    const id = columnOf(card);
    let lane = listed.find((c) => c.id === id) ?? others.find((c) => c.id === id);
    if (!lane) {
      lane = { id, title: words(id) || "None", cards: [] };
      others.push(lane);
    }
    lane.cards.push(card);
  }
  return [...listed, ...others];
});
const dueOf = (card: BoardCard) => due(card.due, isDone(columnOf(card)));

const dragged = ref<BoardCard | null>(null);
const over = ref<string | undefined>(undefined);
function drop(id: string) {
  const card = dragged.value;
  over.value = undefined;
  dragged.value = null;
  if (card === null || columnOf(card) === id) {
    return;
  }
  moved.value = new Map(moved.value).set(card, id);
  props.onMove!(card, id);
}
</script>

<template>
  <Widget :count="shown.length || undefined">
    <template v-if="hideDone" #controls><Toggle v-model="hiding" label="Hide done" /></template>
    <div class="-mx-1 overflow-x-auto px-1">
      <div class="grid gap-3" :style="{ gridTemplateColumns: `repeat(${lanes.length}, minmax(12rem, 1fr))` }">
        <section
          v-for="lane in lanes"
          :key="lane.id"
          class="flex min-w-0 flex-col gap-2 rounded-md bg-muted/50 p-2 transition-colors"
          :class="onMove && over === lane.id && dragged !== null && 'bg-accent'"
          :aria-label="lane.title"
          @dragover.prevent="onMove && (over = lane.id)"
          @dragleave="over === lane.id && (over = undefined)"
          @drop.prevent="onMove && drop(lane.id)"
        >
          <h4 class="flex items-center gap-2 px-1 text-small font-medium text-muted-foreground">
            {{ lane.title }}<span class="rounded-full bg-muted px-1.5 text-caption tabular-nums">{{ lane.cards.length }}</span>
          </h4>
          <p v-if="lane.cards.length === 0" class="px-1 py-2 text-small text-muted-foreground">None.</p>
          <article
            v-for="(card, i) in lane.cards"
            :key="i"
            class="grid gap-1 rounded-md border bg-card p-2"
            :class="onMove && 'cursor-grab active:cursor-grabbing'"
            :draggable="!!onMove"
            @dragstart="dragged = card"
            @dragend="dragged = null"
          >
            <div v-if="card.key || card.priority" class="flex items-center justify-between gap-2 text-caption">
              <span class="font-mono text-muted-foreground">{{ card.key }}</span>
              <span v-if="card.priority" :class="toneText[priorityToneBy(priorityTone)(card.priority)]">{{ card.priority }}</span>
            </div>
            <a v-if="card.link" :href="card.link" class="line-clamp-2 hover:underline" draggable="false">{{ card.title }}</a>
            <p v-else class="line-clamp-2">{{ card.title }}</p>
            <div v-if="card.who || dueOf(card)" class="flex items-center gap-2 text-small text-muted-foreground">
              <span v-if="card.who" class="truncate">{{ card.who }}</span>
              <span v-if="dueOf(card)" class="ml-auto shrink-0" :class="dueOf(card)!.tone !== 'neutral' && toneText[dueOf(card)!.tone]">{{
                dueOf(card)!.text
              }}</span>
            </div>
          </article>
        </section>
      </div>
    </div>
  </Widget>
</template>
