<script setup lang="ts">
// Tasklane's issues as a board, one column for each state: yours, or
// everyone's, in one project or all of them. Dragging an issue to another
// column moves it to that state in Tasklane.
import { computed, ref } from "vue"
import { useService, BoardWidget, Grid } from "harbor"

const tasklane = useService("tasklane")

const columns = [
  { id: "TODO", title: "To do" },
  { id: "IN_PROGRESS", title: "In progress" },
  { id: "IN_REVIEW", title: "In review" },
  { id: "DONE", title: "Done" },
]
// "HIGH" is "High".
const word = (s) => s.charAt(0) + s.slice(1).toLowerCase()

const project = ref("")
const mine = ref(true)
const projects = computed(() => [
  { value: "", label: "All projects" },
  ...tasklane.list_projects().projects.map((p) => ({ value: p.key, label: p.name })),
])
const cards = computed(() =>
  tasklane
    .list_issues({
      limit: 200,
      ...(project.value && { project: project.value }),
      ...(mine.value && { assignee: "me" }),
    })
    .issues.map((i) => ({
      key: i.key,
      title: i.title,
      column: i.state,
      priority: word(i.priority),
      who: mine.value ? undefined : i.assignee?.name,
      link: i.url,
    })),
)
</script>

<template>
  <Grid>
    <BoardWidget
      :title="mine ? 'Your issues' : 'Everyone\'s issues'"
      span="full"
      :rows="8"
      :cards="cards"
      :columns="columns"
      :done="['DONE']"
      filter-label="Project"
      :filters="projects"
      v-model:filter="project"
      toggle="Only mine"
      v-model:toggled="mine"
      @move="(card, state) => tasklane.update_issue({ key: card.key, state })"
    />
  </Grid>
</template>
