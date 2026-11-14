<script setup lang="ts">
// A dashboard shows what services' tools return, with the kit in "harbor":
// const s = useService("service") reaches a service, s.tool({ ...input })
// reads one of its tools in a computed, and a widget, such as InboxWidget,
// AgendaWidget, or ListWidget, shows what the computed makes of it.
import { Grid, Text, Widget } from "harbor"
</script>

<template>
  <Grid>
    <Widget title="A new dashboard" span="full">
      <Text>Read a service's tool with useService, and show it in a widget.</Text>
    </Widget>
  </Grid>
</template>
