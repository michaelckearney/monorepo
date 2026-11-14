<script setup lang="ts">
// Everything with a date in the next seven days, from every service that
// has dates, as one agenda by day: appointments, school, bills, and time
// off; and deliveries on the way. Each service loads on its own: one you
// are signed out of says so in a line of the agenda, and the rest still
// show.
import { computed } from "vue"
import { call, format, useCall, useData, AgendaWidget, Grid, StatusWidget } from "harbor"

const pad = (n) => String(n).padStart(2, "0")
const iso = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const first = iso(new Date())
const last = iso(new Date(Date.now() + 6 * 86_400_000))

// Each source gives items: { start, end?, what, detail, tone? }. A start
// with a time is the clinic's own, as the appointment says it.
const sources = [
  useData(
    async () => {
      const { people } = await call("clearwater", "list_people")
      const items = []
      for (const p of people) {
        const { appointments } = await call("clearwater", "list_appointments", { person: p.id, upcoming: true })
        for (const a of appointments) {
          items.push({ start: a.start, what: `${p.name}: ${a.reason}`, detail: `${a.clinician}, ${a.location} · Clearwater` })
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
        ...events.map((e) => ({ start: e.date, what: e.title, detail: `${e.school} · Maple Grove` })),
        ...assignments
          .filter((a) => a.status === "assigned")
          .map((a) => ({ start: a.due, what: `${a.student}: ${a.title} due`, detail: `${a.course} · Maple Grove` })),
      ]
    },
    { source: "Maple Grove Schools" },
  ),
  useData(
    async () => {
      const [{ bills }, account] = await Promise.all([call("brightline", "list_bills", { status: "due" }), call("brightline", "get_account")])
      return bills.map((b) => ({
        start: b.due,
        what: `Electricity bill, ${format(b.amount, "money")}`,
        detail: `${account.autopay ? "Paid by autopay" : "Not on autopay"} · Brightline`,
        tone: account.autopay ? undefined : "warning",
      }))
    },
    { source: "Brightline Energy" },
  ),
  useData(
    async () => {
      const { requests } = await call("payday", "get_time_off")
      return requests
        .filter((r) => r.status !== "cancelled")
        .map((r) => ({
          start: r.start,
          end: r.end,
          what: `Time off, ${r.type}`,
          detail: `${r.status}${r.note ? `, ${r.note}` : ""} · Payday`,
          tone: r.status === "pending" ? "warning" : undefined,
        }))
    },
    { source: "Payday" },
  ),
]
const orders = useCall("parcel", "list_orders", { limit: 10 })
const onTheWay = computed(() => (orders.data?.orders ?? []).filter((o) => o.status !== "delivered" && o.status !== "cancelled"))

// What overlaps the next seven days.
const week = computed(() =>
  sources.flatMap((s) => s.data ?? []).filter((it) => it.start.slice(0, 10) <= last && (it.end ?? it.start).slice(0, 10) >= first),
)
</script>

<template>
  <Grid>
    <AgendaWidget
      title="The next seven days"
      :items="week"
      :from="sources"
      start="start"
      end="end"
      label="what"
      detail="detail"
      :tone="(it) => it.tone ?? 'info'"
      empty="Nothing in the next seven days."
      :span="8"
      :rows="8"
    />
    <StatusWidget
      title="On the way"
      :items="onTheWay"
      :from="orders"
      label="summary"
      :detail="(o) => `Ordered ${format(o.placed, 'date')}, ${o.item_count} ${o.item_count === 1 ? 'item' : 'items'}`"
      status="status"
      :value="(o) => format(o.total, 'money')"
      empty="Nothing on the way."
      :rows="8"
    />
  </Grid>
</template>
