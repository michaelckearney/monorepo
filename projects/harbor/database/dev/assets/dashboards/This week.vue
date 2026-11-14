<script setup lang="ts">
// Everything with a date in the next seven days, from every service that
// has dates, as one list by day: appointments, school, bills, time off, and
// deliveries on the way. Each service loads on its own: one you are signed
// out of says so in its own card, and the rest still show.
import { computed } from "vue"
import { call, Card, format, Grid, List, useCall, useData } from "harbor"

const pad = (n) => String(n).padStart(2, "0")
const iso = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const days = Array.from({ length: 7 }, (_, i) => {
  const d = new Date()
  d.setDate(d.getDate() + i)
  return iso(d)
})

// Each source gives items: { date, time?, what, detail, source }.
const sources = [
  useData(
    async () => {
      const { people } = await call("clearwater", "list_people")
      const items = []
      for (const p of people) {
        const { appointments } = await call("clearwater", "list_appointments", { person: p.id, upcoming: true })
        for (const a of appointments) {
          // The clinic's own date and time, as the appointment says them.
          items.push({ date: a.start.slice(0, 10), time: a.start.slice(11, 16), what: `${p.name}: ${a.reason}`, detail: `${a.clinician}, ${a.location}`, source: "Clearwater" })
        }
      }
      return items
    },
    { source: "Clearwater Health" },
  ),
  useData(
    async () => {
      const [{ events }, { assignments }] = await Promise.all([call("maplegrove", "list_events"), call("maplegrove", "list_assignments")])
      return [
        ...events.map((e) => ({ date: e.date, what: e.title, detail: e.school, source: "Maple Grove" })),
        ...assignments
          .filter((a) => a.status === "assigned")
          .map((a) => ({ date: a.due, what: `${a.student}: ${a.title} due`, detail: a.course, source: "Maple Grove" })),
      ]
    },
    { source: "Maple Grove Schools" },
  ),
  useData(
    async () => {
      const [{ bills }, account] = await Promise.all([call("brightline", "list_bills", { status: "due" }), call("brightline", "get_account")])
      return bills.map((b) => ({
        date: b.due,
        what: `Electricity bill, ${format(b.amount, "money")}`,
        detail: account.autopay ? "Paid by autopay" : "Not on autopay",
        source: "Brightline",
      }))
    },
    { source: "Brightline Energy" },
  ),
  useData(
    async () => {
      const { requests } = await call("payday", "get_time_off")
      return requests
        .filter((r) => r.status !== "cancelled")
        .flatMap((r) =>
          days
            .filter((date) => date >= r.start && date <= r.end)
            .map((date) => ({ date, what: `Time off, ${r.type}`, detail: `${r.status}${r.note ? `, ${r.note}` : ""}`, source: "Payday" })),
        )
    },
    { source: "Payday" },
  ),
]
const orders = useCall("parcel", "list_orders", { limit: 10 })

const week = computed(() =>
  sources
    .flatMap((s) => s.data ?? [])
    .filter((it) => it.date >= days[0] && it.date <= days[6])
    .sort((a, b) => a.date.localeCompare(b.date) || (a.time ?? "").localeCompare(b.time ?? "")),
)
const loading = computed(() => sources.some((s) => s.loading))
const day = (it) => (it.date === days[0] ? "Today" : it.date === days[1] ? "Tomorrow" : format(it.date, "date"))
</script>

<template>
  <Grid>
    <List
      title="The next seven days"
      :items="week"
      :group="day"
      label="what"
      :detail="(it) => `${it.detail} · ${it.source}`"
      :value="(it) => it.time ?? ''"
      :empty="loading ? 'Loading…' : 'Nothing in the next seven days.'"
      :span="2"
    />
    <List
      title="On the way"
      :items="(orders.data?.orders ?? []).filter((o) => o.status !== 'delivered' && o.status !== 'cancelled')"
      :from="orders"
      label="summary"
      detail="status"
      empty="Nothing on the way."
    />
    <template v-for="s in sources" :key="s.source">
      <Card v-if="s.needsSignIn || s.error" :title="`Not shown: ${s.source}`" :from="s" />
    </template>
  </Grid>
</template>
