<!--
description: Money in and out from a bank or a bill: transactions, charges, purchases, deposits, payslips, bills. Each shows its date, what it was, its account, its category, and the amount, money in in green; total adds them up in the footer.
keywords: ledger, transactions, recent transactions, payments, charges, purchases, spending, expenses, income, deposits, withdrawals, bills, payslips, money, bank, checking, credit card, statement, account activity, history, largest charges
example:
  <LedgerWidget title="Recent transactions" :entries="[{ date: '2026-10-30', title: 'Harbor Grocers', amount: -84.2, category: 'Groceries' }, { date: '2026-10-31', title: 'Northwind payroll', amount: 2140.5, category: 'Income' }]" total search="Search transactions" />
-->
<script setup lang="ts">
import { computed } from "vue";
import type { LedgerEntry } from "../shapes";
import Badge from "../primitives/Badge.vue";
import Item from "../primitives/Item.vue";
import Time from "../primitives/Time.vue";
import Value from "../primitives/Value.vue";
import Widget from "./Widget.vue";

// Money in and out, as LedgerEntry says, in currency (USD unless set).
// total adds up the amounts shown, in the footer. limit shows only the
// first so many.
const props = withDefaults(
  defineProps<{
    entries: LedgerEntry[];
    currency?: string;
    total?: boolean;
    limit?: number;
    empty?: string;
  }>(),
  { empty: "No transactions." },
);

const shown = computed(() => (props.limit ? props.entries.slice(0, props.limit) : props.entries));
const sum = computed(() =>
  props.total ? { value: shown.value.reduce((s, e) => s + e.amount, 0), format: "money" as const, currency: props.currency, signed: true } : undefined,
);
</script>

<template>
  <Widget :total="sum" :more="limit ? Math.max(0, entries.length - limit) : 0">
    <p v-if="shown.length === 0" class="py-2 text-muted-foreground">{{ empty }}</p>
    <ul v-else class="grid">
      <li v-for="(e, i) in shown" :key="i" class="border-b last:border-0">
        <Item :label="e.title" :detail="e.account" :link="e.link">
          <template v-if="e.date" #leading>
            <Time :value="e.date" class="w-12 shrink-0 self-start pt-px text-small text-muted-foreground" />
          </template>
          <template #trailing>
            <Badge v-if="e.category">{{ e.category }}</Badge>
            <Value :value="e.amount" format="money" :currency="currency" signed class="w-24 text-right font-medium" />
          </template>
        </Item>
      </li>
    </ul>
  </Widget>
</template>
