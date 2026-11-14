<script setup lang="ts">
// What needs you at Northwind today: your issues in Tasklane that are under
// way, the Huddle messages that mention you, and your time off from Payday.
import { call, useCall, useData, AgendaWidget, Grid, InboxWidget, StatsWidget, TasksWidget } from "harbor"

const issues = useData(
  async () => {
    const [review, progress] = await Promise.all([
      call("tasklane", "list_issues", { assignee: "me", state: "IN_REVIEW" }),
      call("tasklane", "list_issues", { assignee: "me", state: "IN_PROGRESS" }),
    ])
    return [...review.issues, ...progress.issues]
  },
  { source: "Tasklane" },
)
const mentions = useCall("huddle", "list_mentions")

// Today where you are, as YYYY-MM-DD.
const today = new Date().toLocaleDateString("en-CA")
const timeOff = useData(
  async () => {
    const { balances, requests } = await call("payday", "get_time_off")
    return { balances, upcoming: requests.filter((r) => r.end >= today && r.status !== "cancelled") }
  },
  { source: "Payday" },
)
</script>

<template>
  <Grid>
    <TasksWidget
      title="Your issues under way"
      :items="issues"
      key-field="key"
      label="title"
      status="state"
      priority="priority"
      link="url"
      empty="Nothing in progress or in review."
      :span="8"
    />
    <InboxWidget
      title="Mentions"
      :items="mentions"
      sender="author"
      :subject="(m) => `#${m.channel}`"
      preview="text"
      time="sent"
      :unread="(m) => !m.read"
      :limit="6"
      empty="No mentions."
    />
    <AgendaWidget
      title="Time off coming up"
      :items="timeOff"
      field="upcoming"
      start="start"
      end="end"
      :label="(r) => r.type.charAt(0).toUpperCase() + r.type.slice(1)"
      :detail="(r) => r.status + (r.note ? `, ${r.note}` : '')"
      status="status"
      empty="None booked."
      :span="8"
    />
    <StatsWidget title="Time off left" :items="timeOff" field="balances" label="label" value="hours" note="hours" :span="4" :rows="4" />
  </Grid>
</template>
