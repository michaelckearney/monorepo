<script setup lang="ts">
// The house: Homebase's sensors and disks, the internet and what is on the
// network from the router, and electricity use from Brightline, by month or
// by day. The sensors read again every minute.
import { computed, ref } from "vue"
import { useService, ChartWidget, DetailsWidget, Grid, MetersWidget, StatusWidget } from "harbor"

const homebase = useService("homebase")
const router = useService("homebase-router")
const brightline = useService("brightline")

const sensors = computed(() =>
  homebase.get_sensors({}, { every: 60 }).sensors.map((s) => ({ label: s.name, value: `${s.value} ${s.unit}`, tone: "info" })),
)
const disks = computed(() => homebase.get_storage().volumes.map((v) => ({ label: v.name, value: v.used_gb, max: v.size_gb })))

const status = computed(() => router.get_status())
const internet = computed(() => [
  { label: "Internet", value: status.value.wan_status },
  { label: "Up", value: `${Math.round(status.value.uptime_hours / 24)} days` },
  { label: "Address", value: status.value.wan_ip },
])
const clients = computed(() =>
  [...router.list_clients().clients]
    .sort((a, b) => Number(b.online) - Number(a.online))
    .map((c) => ({ label: c.name, detail: c.kind, status: c.online ? "Online" : "Offline", value: c.online ? c.band : "" })),
)

// The last 12 months, or the last 31 days.
const by = ref("month")
const usage = computed(() => {
  const { usage } = brightline.get_usage({ by: by.value })
  return [{ name: "kWh", points: usage.slice(by.value === "day" ? -31 : -12).map((u) => ({ x: u.period, y: u.kwh })) }]
})
</script>

<template>
  <Grid>
    <StatusWidget title="Sensors" :items="sensors" />
    <MetersWidget title="Disks" :gauges="disks" unit="GB" />
    <DetailsWidget title="Internet" :rows="2" :facts="internet" />
    <StatusWidget title="On the network" :rows="2" :count="status.clients_online" :items="clients" />
    <ChartWidget
      :title="by === 'month' ? 'Electricity, kWh a month' : 'Electricity, kWh a day'"
      span="full"
      :series="usage"
      :x-format="by === 'month' ? 'month' : 'date'"
      :views="[{ value: 'month', label: 'By month' }, { value: 'day', label: 'By day' }]"
      v-model:view="by"
    />
  </Grid>
</template>
