<script setup lang="ts">
// Tasklane's issues as a board, one column for each state: yours, or
// everyone's, in one project or all of them. Dragging an issue to another
// column moves it to that state in Tasklane.
import { computed, ref } from "vue"
import { useAction, useCall, Board, Grid, Widget } from "harbor"

const columns = [
  { value: "TODO", title: "To do" },
  { value: "IN_PROGRESS", title: "In progress" },
  { value: "IN_REVIEW", title: "In review" },
  { value: "DONE", title: "Done" },
]

const project = ref("")
const mine = ref(true)
const projects = useCall("tasklane", "list_projects")
const issues = useCall("tasklane", "list_issues", () => ({
  limit: 200,
  ...(project.value && { project: project.value }),
  ...(mine.value && { assignee: "me" }),
}))
const update = useAction("tasklane", "update_issue")

const controls = [
  {
    select: "Project",
    model: project,
    options: computed(() => [
      { value: "", label: "All projects" },
      ...(projects.data?.projects ?? []).map((p) => ({ value: p.key, label: p.name })),
    ]),
  },
  { toggle: "Only mine", model: mine },
]
</script>

<template>
  <Grid>
    <Widget :title="mine ? 'Your issues' : 'Everyone\'s issues'" :from="[issues, update]" :controls="controls" span="full" :rows="8">
      <Board
        :items="issues"
        column="state"
        :columns="columns"
        :done="['DONE']"
        key-field="key"
        label="title"
        priority="priority"
        :who="mine ? undefined : (i) => i.assignee?.name"
        link="url"
        @move="(issue, state) => update.run({ key: issue.key, state })"
      />
    </Widget>
  </Grid>
</template>
