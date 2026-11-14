<script setup lang="ts">
// Tasklane's issues as a board, one column for each state: yours, or
// everyone's, in one project or all of them.
import { ref } from "vue"
import { Grid, List, useCall } from "harbor"

const columns = [
  { state: "TODO", title: "To do" },
  { state: "IN_PROGRESS", title: "In progress" },
  { state: "IN_REVIEW", title: "In review" },
  { state: "DONE", title: "Done" },
]

const project = ref("")
const mine = ref(true)
const projects = useCall("tasklane", "list_projects")
// Runs again when the filters change.
const issues = useCall("tasklane", "list_issues", () => ({
  limit: 200,
  ...(project.value && { project: project.value }),
  ...(mine.value && { assignee: "me" }),
}))

const inState = (state) => (issues.data?.issues ?? []).filter((i) => i.state === state)
</script>

<template>
  <div class="filters">
    <select v-model="project" aria-label="Project">
      <option value="">All projects</option>
      <option v-for="p in projects.data?.projects ?? []" :key="p.key" :value="p.key">{{ p.name }}</option>
    </select>
    <label><input v-model="mine" type="checkbox" /> Only mine</label>
    <span v-if="issues.loading" class="loading">Loading…</span>
  </div>
  <Grid :columns="4">
    <List
      v-for="c in columns"
      :key="c.state"
      :title="`${c.title} · ${inState(c.state).length}`"
      :items="inState(c.state)"
      :from="issues"
      label="title"
      link="url"
      :detail="(i) => `${i.key} · ${i.priority.toLowerCase()}${!mine && i.assignee ? ` · ${i.assignee.name}` : ''}`"
      empty="None."
    />
  </Grid>
</template>

<style scoped>
.filters {
  display: flex;
  align-items: center;
  gap: 1rem;
  margin-bottom: 1rem;
}
select {
  font: inherit;
  color: inherit;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: calc(var(--radius) - 2px);
  padding: 0.25rem 0.5rem;
}
.loading {
  color: var(--muted-foreground);
}
</style>
