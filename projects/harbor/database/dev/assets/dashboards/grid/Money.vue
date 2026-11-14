<script setup lang="ts">
// Money in and money out: balances at Tidewater Bank, the electricity bill
// from Brightline, and the latest pay from Payday, against the one before.
// Each widget loads on its own, so a service you are signed out of asks in
// its own widget.
import { call, format, useCall, useData, Grid, LedgerWidget, ListWidget, MetricWidget } from "harbor"

const accounts = useCall("tidewater", "list_accounts")
const pay = useCall("payday", "list_payslips", { limit: 2 })

const bill = useData(
  async () => {
    const [{ bills }, account] = await Promise.all([
      call("brightline", "list_bills", { status: "due", limit: 1 }),
      call("brightline", "get_account"),
    ])
    return bills[0] ? { ...bills[0], autopay: account.autopay } : null
  },
  { source: "Brightline Energy" },
)

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
    <ListWidget title="Accounts" :items="accounts" label="name" detail="number" value="balance" format="money" total :span="8" />
    <MetricWidget
      title="Electricity bill"
      :value="bill"
      field="amount"
      format="money"
      :span="4"
      :note="bill.data ? `Due ${format(bill.data.due, 'date')}${bill.data.autopay ? ', paid by autopay' : ', not on autopay'}` : 'Nothing due.'"
    />
    <MetricWidget
      title="Latest pay from Northwind"
      :value="pay"
      field="payslips.0.net"
      previous="payslips.1.net"
      format="money"
      :span="4"
      :note="pay.data?.payslips[0] && `Paid ${format(pay.data.payslips[0].pay_date, 'date')}, against the pay before`"
    />
    <LedgerWidget
      title="Largest charges in the last month"
      :items="charges"
      date="date"
      description="description"
      amount="amount"
      account="account"
      category="category"
      total
      span="full"
      :rows="4"
    />
  </Grid>
</template>
