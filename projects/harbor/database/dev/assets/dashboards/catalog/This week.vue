<script setup lang="ts">
// Everything with a date in the next seven days, from every service that
// has dates, as one agenda by day: appointments, school, bills, and time
// off; deliveries on the way; and time off waiting for approval, which you
// can cancel.
import { computed } from "vue"
import { format, useService, AgendaWidget, Grid, StatusWidget, TasksWidget } from "harbor"

const clearwater = useService("clearwater")
const maplegrove = useService("maplegrove")
const brightline = useService("brightline")
const payday = useService("payday")
const parcel = useService("parcel")

const pad = (n) => String(n).padStart(2, "0")
const iso = (d) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
const first = iso(new Date())
const last = iso(new Date(Date.now() + 6 * 86_400_000))
const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1)

// Each service's events. A start with a time is the clinic's own, as the
// appointment says it.
const appointments = computed(() => {
  const asked = clearwater.list_people().people.map((p) => [p, clearwater.list_appointments({ person: p.id, upcoming: true })])
  return asked.flatMap(([p, out]) =>
    out.appointments.map((a) => ({ start: a.start, title: `${p.name}: ${a.reason}`, detail: `${a.clinician}, ${a.location} · Clearwater` })),
  )
})
const school = computed(() => {
  const { events } = maplegrove.list_events()
  const { assignments } = maplegrove.list_assignments()
  return [
    ...events.map((e) => ({ start: e.date, title: e.title, detail: `${e.school} · Maple Grove` })),
    ...assignments
      .filter((a) => a.status === "assigned")
      .map((a) => ({ start: a.due, title: `${a.student}: ${a.title} due`, detail: `${a.course} · Maple Grove` })),
  ]
})
const bills = computed(() => {
  const { bills } = brightline.list_bills({ status: "due" })
  const { autopay } = brightline.get_account()
  return bills.map((b) => ({
    start: b.due,
    title: `Electricity bill, ${format(b.amount, "money")}`,
    detail: `${autopay ? "Paid by autopay" : "Not on autopay"} · Brightline`,
    tone: autopay ? "info" : "warning",
  }))
})
const requests = computed(() => payday.get_time_off().requests.filter((r) => r.status !== "cancelled"))
const timeOff = computed(() =>
  requests.value.map((r) => ({
    start: r.start,
    end: r.end,
    title: `Time off, ${r.type}`,
    detail: `${r.status}${r.note ? `, ${r.note}` : ""} · Payday`,
    tone: r.status === "pending" ? "warning" : "info",
  })),
)

// What overlaps the next seven days. Every source's reads are asked for
// before any is used, so they load together, and a widget signed out of
// several names them all.
const week = computed(() => {
  clearwater.list_people()
  maplegrove.list_events()
  maplegrove.list_assignments()
  brightline.list_bills({ status: "due" })
  brightline.get_account()
  payday.get_time_off()
  return [...appointments.value, ...school.value, ...bills.value, ...timeOff.value].filter(
    (e) => e.start.slice(0, 10) <= last && (e.end ?? e.start).slice(0, 10) >= first,
  )
})

const onTheWay = computed(() =>
  parcel
    .list_orders({ limit: 10 })
    .orders.filter((o) => o.status !== "delivered" && o.status !== "cancelled")
    .map((o) => ({
      label: o.summary,
      detail: `Ordered ${format(o.placed, "date")}, ${o.item_count} ${o.item_count === 1 ? "item" : "items"}`,
      status: cap(o.status.replace(/_/g, " ")),
      value: format(o.total, "money"),
    })),
)

const pending = computed(() =>
  requests.value
    .filter((r) => r.status === "pending")
    .map((r) => ({ id: r.id, title: cap(r.type), detail: `${format(r.start, "date")} to ${format(r.end, "date")}, ${r.hours} hours` })),
)
</script>

<template>
  <Grid>
    <AgendaWidget title="The next seven days" :span="8" :rows="8" :events="week" empty="Nothing in the next seven days." />
    <StatusWidget title="On the way" :items="onTheWay" empty="Nothing on the way." />
    <TasksWidget
      title="Time off waiting for approval"
      :tasks="pending"
      act="Cancel"
      empty="Nothing waiting."
      @act="(r) => payday.cancel_time_off({ id: r.id })"
    />
  </Grid>
</template>
