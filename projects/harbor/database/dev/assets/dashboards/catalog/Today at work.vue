<script setup lang="ts">
// What needs you at Northwind today: your issues in Tasklane that are under
// way, the Huddle messages that mention you, a search of Huddle, and your
// time off from Payday.
import { computed, ref } from "vue"
import { useService, AgendaWidget, Grid, InboxWidget, StatsWidget, TasksWidget } from "harbor"

const tasklane = useService("tasklane")
const huddle = useService("huddle")
const payday = useService("payday")

// "IN_REVIEW" is "In review".
const words = (s) => s.charAt(0) + s.slice(1).toLowerCase().replace(/_/g, " ")

// Both lists are asked for at once: neither is used until both are.
const underWay = computed(() => {
  const review = tasklane.list_issues({ assignee: "me", state: "IN_REVIEW" })
  const progress = tasklane.list_issues({ assignee: "me", state: "IN_PROGRESS" })
  return [...review.issues, ...progress.issues].map((i) => ({
    key: i.key,
    title: i.title,
    status: words(i.state),
    priority: words(i.priority),
    link: i.url,
  }))
})

const message = (m) => ({ from: m.author, subject: `#${m.channel}`, preview: m.text, at: m.sent, unread: !m.read })
const mentions = computed(() => huddle.list_mentions().mentions.map(message))

// Huddle is searched only once something is typed.
const query = ref("")
const found = computed(() => (query.value.trim() ? huddle.search_messages({ query: query.value.trim() }).results.map(message) : []))

// Today where you are, as YYYY-MM-DD.
const today = new Date().toLocaleDateString("en-CA")
const timeOff = computed(() => payday.get_time_off())
const upcoming = computed(() =>
  timeOff.value.requests
    .filter((r) => r.end >= today && r.status !== "cancelled")
    .map((r) => ({
      title: words(r.type),
      start: r.start,
      end: r.end,
      detail: words(r.status) + (r.note ? `, ${r.note}` : ""),
      tone: r.status === "pending" ? "warning" : "good",
    })),
)
const balances = computed(() => timeOff.value.balances.map((b) => ({ label: b.label, value: b.hours, note: "hours" })))
</script>

<template>
  <Grid>
    <TasksWidget
      title="Your issues under way"
      :span="8"
      :tasks="underWay"
      act="Done"
      empty="Nothing in progress or in review."
      @act="(t) => tasklane.update_issue({ key: t.key, state: 'DONE' })"
    />
    <InboxWidget
      title="Mentions"
      :messages="mentions"
      :limit="6"
      empty="No mentions."
      action="Mark all read"
      @action="huddle.mark_mentions_read()"
    />
    <InboxWidget
      title="Huddle"
      :messages="found"
      search="Search Huddle"
      v-model:query="query"
      :empty="query.trim() ? 'No messages match.' : ''"
    />
    <AgendaWidget title="Time off coming up" :events="upcoming" empty="None booked." />
    <StatsWidget title="Time off left" :stats="balances" />
  </Grid>
</template>
