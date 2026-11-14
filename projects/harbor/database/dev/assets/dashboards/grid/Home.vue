<script setup lang="ts">
// The house: Homebase's sensors and disks, the internet and what is on the
// network from the router, and electricity use from Brightline.
import { call, useCall, useData, Grid, ChartWidget, KeyValue, MetersWidget, Stack, StatusList, StatusWidget, Widget } from "harbor"

const sensors = useCall("homebase", "get_sensors")
const storage = useCall("homebase", "get_storage")
const usage = useData(async () => (await call("brightline", "get_usage", { by: "month" })).usage.slice(-12), {
  source: "Brightline Energy",
})
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
    <StatusWidget title="Sensors" :items="sensors" label="name" :value="(s) => `${s.value} ${s.unit}`" tone="info" />
    <MetersWidget title="Disks" :items="storage" label="name" value="used_gb" max="size_gb" unit="GB" />
    <Widget title="Network" :from="network" :count="network.data?.clients_online">
      <Stack gap="loose">
        <KeyValue
          :value="network.data"
          :fields="[
            { key: 'wan_status', label: 'Internet' },
            { key: 'uptime_hours', label: 'Up', value: (s) => `${Math.round(s.uptime_hours / 24)} days` },
            { key: 'wan_ip', label: 'Address' },
          ]"
        />
        <StatusList
          :items="network.data?.clients ?? []"
          label="name"
          detail="kind"
          :status="(c) => (c.online ? 'online' : 'offline')"
          :value="(c) => (c.online ? c.band : '')"
        />
      </Stack>
    </Widget>
    <ChartWidget title="Electricity, kWh a month" :items="usage" x="period:month" y="kwh" span="full" />
  </Grid>
</template>
