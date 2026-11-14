<script setup lang="ts">
// The house: Homebase's sensors and disks, the internet and what is on the
// network from the router, and electricity use from Brightline, by month or
// by day. The sensors read again every minute.
import { ref } from "vue"
import { call, useCall, useData, Chart, Details, Grid, Meters, Stack, StatusList, Widget } from "harbor"

const sensors = useCall("homebase", "get_sensors", {}, { every: 60 })
const storage = useCall("homebase", "get_storage")

// The last 12 months, or the last 31 days.
const by = ref("month")
const usage = useData(
  async () => {
    const days = by.value === "day"
    const { usage } = await call("brightline", "get_usage", { by: by.value })
    return usage.slice(days ? -31 : -12)
  },
  { source: "Brightline Energy" },
)
const usageControls = [
  {
    segmented: "Period",
    model: by,
    options: [
      { value: "month", label: "By month" },
      { value: "day", label: "By day" },
    ],
  },
]

const network = useData(
  async () => {
    const [status, { clients }] = await Promise.all([
      call("homebase-router", "get_status"),
      call("homebase-router", "list_clients"),
    ])
    return { ...status, clients: [...clients].sort((a, b) => Number(b.online) - Number(a.online)) }
  },
  { source: "Homebase router" },
)
</script>

<template>
  <Grid>
    <Widget title="Sensors" :from="sensors">
      <StatusList :items="sensors" label="name" :value="(s) => `${s.value} ${s.unit}`" tone="info" />
    </Widget>
    <Widget title="Disks" :from="storage">
      <Meters :items="storage" label="name" value="used_gb" max="size_gb" unit="GB" />
    </Widget>
    <Widget title="Network" :from="network" :count="network.data?.clients_online">
      <Stack gap="loose">
        <Details
          :value="network"
          :fields="[
            { key: 'wan_status', label: 'Internet' },
            { key: 'uptime_hours', label: 'Up', value: (s) => `${Math.round(s.uptime_hours / 24)} days` },
            { key: 'wan_ip', label: 'Address' },
          ]"
        />
        <StatusList
          :items="network"
          field="clients"
          label="name"
          detail="kind"
          :status="(c) => (c.online ? 'online' : 'offline')"
          :value="(c) => (c.online ? c.band : '')"
        />
      </Stack>
    </Widget>
    <Widget :title="by === 'month' ? 'Electricity, kWh a month' : 'Electricity, kWh a day'" :from="usage" :controls="usageControls" span="full">
      <Chart :items="usage" :x="by === 'month' ? 'period:month' : 'period:date'" y="kwh" />
    </Widget>
  </Grid>
</template>
