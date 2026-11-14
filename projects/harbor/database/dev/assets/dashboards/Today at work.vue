<script setup lang="ts">
// What needs you at Northwind today: your issues in Tasklane that are under
// way, the Huddle messages that mention you, and your time off from Payday.
import { call, format, Grid, List, useCall, useData } from "harbor"

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
const mentions = useCall("huddle", "list_mentions", { unread: true })

// Today where you are, as YYYY-MM-DD.
const today = new Date().toLocaleDateString("en-CA")
const timeOff = useData(
  async () => {
    const { balances, requests } = await call("payday", "get_time_off")
    return { balances, upcoming: requests.filter((r) => r.end >= today && r.status !== "cancelled") }
  },
  { source: "Payday" },
)

const days = (r) => format(r.start, "date") + (r.end !== r.start ? ` to ${format(r.end, "date")}` : "")
</script>

<template>
  <Grid>
    <List
      title="Your issues under way"
      :items="issues"
      :label="(i) => `${i.key} ${i.title}`"
      :detail="(i) => `${i.state === 'IN_REVIEW' ? 'in review' : 'in progress'} · ${i.priority.toLowerCase()}`"
      link="url"
      empty="Nothing in progress or in review."
      :span="2"
    />
    <List
      title="Mentions"
      :items="mentions"
      label="text"
      :detail="(m) => `${m.author} in #${m.channel} · ${format(m.sent, 'datetime')}`"
      empty="No unread mentions."
    />
    <List title="Time off left" :items="timeOff" field="balances" label="label" :value="(b) => `${b.hours} hours`" />
    <List
      title="Time off coming up"
      :items="timeOff"
      field="upcoming"
      :label="(r) => `${days(r)}, ${r.type}`"
      :detail="(r) => r.status + (r.note ? `, ${r.note}` : '')"
      empty="None booked."
      :span="2"
    />
  </Grid>
</template>
