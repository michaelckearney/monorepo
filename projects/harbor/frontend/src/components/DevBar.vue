<script setup lang="ts">
import { Anchor, Blocks, FlaskConical, Wrench } from "lucide-vue-next";
import { devView, type DevView } from "../navigation";

// The developer bar, across the top of the window in dev mode only: Harbor
// itself, then each tool for working on Harbor.
const views: { view: DevView; label: string; icon: typeof Anchor }[] = [
  { view: "harbor", label: "Harbor", icon: Anchor },
  { view: "tools", label: "Tools", icon: Wrench },
  { view: "benchmarks", label: "Benchmarks", icon: FlaskConical },
  { view: "components", label: "Components", icon: Blocks },
];
</script>

<template>
  <nav aria-label="Developer" class="flex shrink-0 items-center gap-1 border-b bg-sidebar px-2 py-1">
    <button
      v-for="v in views"
      :key="v.view"
      type="button"
      class="flex items-center gap-1.5 rounded-md px-2 py-1 text-small text-sidebar-foreground hover:bg-sidebar-accent"
      :class="devView === v.view && 'bg-sidebar-accent font-medium text-sidebar-accent-foreground'"
      :aria-current="devView === v.view ? 'page' : undefined"
      @click="devView = v.view"
    >
      <component :is="v.icon" class="size-3.5 text-muted-foreground" />{{ v.label }}
    </button>
  </nav>
</template>
