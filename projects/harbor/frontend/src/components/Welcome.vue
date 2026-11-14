<script setup lang="ts">
import { LayoutGrid, Plus } from "lucide-vue-next";
import { Button } from "@ui/components/button";
import { open, openChat, sidePanels } from "../navigation";
import PageHeader from "./PageHeader.vue";

// What the window shows before you have a dashboard.
const panels = sidePanels();
</script>

<template>
  <PageHeader title="Harbor" />
  <div class="grid flex-1 place-items-center p-8">
    <div class="grid max-w-sm justify-items-center gap-3 text-center">
      <span class="grid size-11 place-items-center rounded-xl bg-muted text-muted-foreground"><LayoutGrid /></span>
      <h2 class="text-heading font-semibold tracking-tight">No dashboards yet</h2>
      <div class="flex gap-2">
        <Button v-if="panels.agent.collapsed" @click="openChat()">Ask the agent</Button>
        <Button variant="outline" @click="open('dashboard', 0)"><Plus />New dashboard</Button>
      </div>
    </div>
  </div>
</template>
