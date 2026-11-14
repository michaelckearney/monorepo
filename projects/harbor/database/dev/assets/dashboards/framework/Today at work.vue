<script setup lang="ts">
// What needs you at Northwind today: your issues in Tasklane that are under
// way, the Huddle messages that mention you, a search of Huddle, and your
// time off from Payday.
import { ref } from "vue"
import { call, useAction, useCall, useData, Agenda, Grid, Inbox, Stats, Tasks, Widget } from "harbor"

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
const update = useAction("tasklane", "update_issue")

const mentions = useCall("huddle", "list_mentions")
const markRead = useAction("huddle", "mark_mentions_read")
const mentionControls = [{ button: "Mark all read", run: () => markRead.run() }]

// Huddle is searched only once something is typed.
const query = ref("")
const found = useCall("huddle", "search_messages", () => (query.value.trim() ? { query: query.value.trim() } : null))
const searchControls = [{ search: "Search Huddle", model: query }]

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
    <Widget title="Your issues under way" :from="[issues, update]" :span="8">
      <Tasks
        :items="issues"
        key-field="key"
        label="title"
        status="state"
        priority="priority"
        link="url"
        act="Done"
        empty="Nothing in progress or in review."
        @act="(issue) => update.run({ key: issue.key, state: 'DONE' })"
      />
    </Widget>
    <Widget title="Mentions" :from="[mentions, markRead]" :controls="mentionControls">
      <Inbox
        :items="mentions"
        sender="author"
        :subject="(m) => `#${m.channel}`"
        preview="text"
        time="sent"
        :unread="(m) => !m.read"
        :limit="6"
        empty="No mentions."
      />
    </Widget>
    <Widget title="Huddle" :from="found" :controls="searchControls">
      <Inbox :items="found" sender="author" :subject="(m) => `#${m.channel}`" preview="text" time="sent" empty="No messages match." />
    </Widget>
    <Widget title="Time off coming up" :from="timeOff">
      <Agenda
        :items="timeOff"
        field="upcoming"
        start="start"
        end="end"
        :label="(r) => r.type.charAt(0).toUpperCase() + r.type.slice(1)"
        :detail="(r) => r.status + (r.note ? `, ${r.note}` : '')"
        status="status"
        empty="None booked."
      />
    </Widget>
    <Widget title="Time off left" :from="timeOff">
      <Stats :items="timeOff" field="balances" label="label" value="hours" note="hours" />
    </Widget>
  </Grid>
</template>
