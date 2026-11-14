<script setup lang="ts">
// Tasklane's issues as a board, one column for each state: yours, or
// everyone's, in one project or all of them.
import { computed, ref } from "vue"
import { useCall, BoardWidget, Grid, Select, Toggle, Toolbar } from "harbor"

const columns = [
  { value: "TODO", title: "To do" },
  { value: "IN_PROGRESS", title: "In progress" },
  { value: "IN_REVIEW", title: "In review" },
  { value: "DONE", title: "Done" },
]

const project = ref("")
const mine = ref(true)
const projects = useCall("tasklane", "list_projects")
const options = computed(() => [
  { value: "", label: "All projects" },
  ...(projects.data?.projects ?? []).map((p) => ({ value: p.key, label: p.name })),
])
// Runs again when the filters change.
const issues = useCall("tasklane", "list_issues", () => ({
  limit: 200,
  ...(project.value && { project: project.value }),
  ...(mine.value && { assignee: "me" }),
}))
</script>

<template>
  <Toolbar>
    <Select v-model="project" :options="options" label="Project" />
    <Toggle v-model="mine" label="Only mine" />
  </Toolbar>
  <Grid>
    <BoardWidget
      :title="mine ? 'Your issues' : 'Everyone\'s issues'"
      :items="issues"
      column="state"
      :columns="columns"
      key-field="key"
      label="title"
      priority="priority"
      :who="mine ? undefined : (i) => i.assignee?.name"
      link="url"
    />
  </Grid>
</template>
