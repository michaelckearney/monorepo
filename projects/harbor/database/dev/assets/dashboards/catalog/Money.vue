<script setup lang="ts">
// Money in and money out: balances at Tidewater Bank, the electricity bill
// from Brightline, with a switch for its autopay, and the latest pay from
// Payday, against the one before. Each widget loads on its own, so a
// service you are signed out of asks in its own widget.
import { computed, ref } from "vue"
import { format, useService, Grid, LedgerWidget, ListWidget, MetricWidget } from "harbor"

const tidewater = useService("tidewater")
const brightline = useService("brightline")
const payday = useService("payday")

const accounts = computed(() => tidewater.list_accounts().accounts)
const balances = computed(() => accounts.value.map((a) => ({ title: a.name, detail: a.number, value: a.balance })))

const bill = computed(() => brightline.list_bills({ status: "due", limit: 1 }).bills[0] ?? null)
// The switch shows autopay as Brightline has it, and turning it changes it there.
const autopay = computed({
  get: () => brightline.get_account().autopay,
  set: (enabled) => brightline.set_autopay({ enabled }),
})

const payslips = computed(() => payday.list_payslips({ limit: 2 }).payslips)

// The largest charges in the 30 days up to the newest transaction: further
// back, every month's rent would crowd out the rest. Every account's
// transactions are asked for at once.
const charges = computed(() => {
  const asked = accounts.value.map((a) => [a, tidewater.list_transactions({ account: a.id, limit: 60 })])
  const all = asked.flatMap(([a, out]) =>
    out.transactions.filter((t) => t.amount <= -50 && t.category !== "Transfer").map((t) => ({ ...t, account: a.name })),
  )
  const newest = all.reduce((m, c) => (c.date > m ? c.date : m), "")
  const from = new Date(Date.parse(newest + "T00:00:00Z") - 30 * 86_400_000).toISOString().slice(0, 10)
  return all
    .filter((c) => c.date > from)
    .sort((a, b) => a.amount - b.amount)
    .slice(0, 8)
    .map((c) => ({ date: c.date, title: c.description, amount: c.amount, account: c.account, category: c.category }))
})

// The search filters the charges shown.
const query = ref("")
const found = computed(() => {
  const q = query.value.trim().toLowerCase()
  return q ? charges.value.filter((c) => `${c.title} ${c.account} ${c.category}`.toLowerCase().includes(q)) : charges.value
})
</script>

<template>
  <Grid>
    <ListWidget title="Accounts" :span="8" :items="balances" format="money" total />
    <MetricWidget
      title="Electricity bill"
      :rows="2"
      :value="bill?.amount ?? null"
      format="money"
      :note="bill ? `Due ${format(bill.due, 'date')}` : 'Nothing due.'"
      toggle="Autopay"
      v-model:toggled="autopay"
    />
    <MetricWidget
      title="Latest pay from Northwind"
      :rows="2"
      :value="payslips[0]?.net ?? null"
      :previous="payslips[1]?.net"
      format="money"
      :note="payslips[0] && `Paid ${format(payslips[0].pay_date, 'date')}, against the pay before`"
    />
    <LedgerWidget
      title="Largest charges in the last month"
      span="full"
      :entries="found"
      total
      search="Search charges"
      v-model:query="query"
      empty="No charges match."
    />
  </Grid>
</template>
