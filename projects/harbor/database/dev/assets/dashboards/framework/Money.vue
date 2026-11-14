<script setup lang="ts">
// Money in and money out: balances at Tidewater Bank, the electricity bill
// from Brightline, with a switch for its autopay, and the latest pay from
// Payday, against the one before. Each widget loads on its own, so a
// service you are signed out of asks in its own widget.
import { computed } from "vue"
import { call, format, useAction, useCall, useData, Grid, Ledger, List, Metric, Widget } from "harbor"

const accounts = useCall("tidewater", "list_accounts")
const pay = useCall("payday", "list_payslips", { limit: 2 })

const bill = useData(
  async () => {
    const [{ bills }, account] = await Promise.all([
      call("brightline", "list_bills", { status: "due", limit: 1 }),
      call("brightline", "get_account"),
    ])
    return { due: bills[0] ?? null, autopay: account.autopay }
  },
  { source: "Brightline Energy" },
)

// The switch shows autopay as Brightline has it, and turning it changes it there.
const setAutopay = useAction("brightline", "set_autopay")
const autopay = computed({
  get: () => !!bill.data?.autopay,
  set: (enabled) => void setAutopay.run({ enabled }),
})
const billControls = [{ toggle: "Autopay", model: autopay }]

// The largest charges in the 30 days up to the newest transaction: further
// back, every month's rent would crowd out the rest.
const charges = useData(
  async () => {
    const { accounts: list } = await call("tidewater", "list_accounts")
    const all = []
    for (const a of list) {
      const { transactions } = await call("tidewater", "list_transactions", { account: a.id, limit: 60 })
      for (const t of transactions) {
        if (t.amount <= -50 && t.category !== "Transfer") {
          all.push({ ...t, account: a.name })
        }
      }
    }
    const newest = all.reduce((m, c) => (c.date > m ? c.date : m), "")
    const from = new Date(Date.parse(newest + "T00:00:00Z") - 30 * 86_400_000).toISOString().slice(0, 10)
    return all
      .filter((c) => c.date > from)
      .sort((a, b) => a.amount - b.amount)
      .slice(0, 8)
  },
  { source: "Tidewater Bank" },
)
</script>

<template>
  <Grid>
    <Widget title="Accounts" :from="accounts" :span="8">
      <List :items="accounts" label="name" detail="number" value="balance" format="money" total />
    </Widget>
    <Widget title="Electricity bill" :from="[bill, setAutopay]" :controls="billControls" :rows="2">
      <Metric
        :value="bill"
        field="due.amount"
        format="money"
        :note="bill.data?.due ? `Due ${format(bill.data.due.due, 'date')}` : 'Nothing due.'"
      />
    </Widget>
    <Widget title="Latest pay from Northwind" :from="pay" :rows="2">
      <Metric
        :value="pay"
        field="payslips.0.net"
        previous="payslips.1.net"
        format="money"
        :note="pay.data?.payslips[0] && `Paid ${format(pay.data.payslips[0].pay_date, 'date')}, against the pay before`"
      />
    </Widget>
    <Widget title="Largest charges in the last month" :from="charges" span="full">
      <Ledger
        :items="charges"
        date="date"
        description="description"
        amount="amount"
        account="account"
        category="category"
        total
        search="Search charges"
      />
    </Widget>
  </Grid>
</template>
