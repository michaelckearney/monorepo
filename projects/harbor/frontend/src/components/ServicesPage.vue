<script setup lang="ts">
import { computed } from "vue";
import { Plus } from "lucide-vue-next";
import { sql, useQuery } from "@desktop";
import { Button } from "@ui/components/button";
import { open } from "../navigation";
import { signIn, signingIn, signInProblem, useServices } from "../services";
import PageHeader from "./PageHeader.vue";
import ServiceIcon from "./ServiceIcon.vue";

// Every service: the sites Harbor works with for you.
const { services } = useServices();
const details = useQuery<{ name: string; tools: number; description: string }>(
  () => sql`
    SELECT name, json_array_length(manifest, '$.tools') AS tools,
      coalesce(json_extract(manifest, '$.description'), '') AS description
    FROM services`,
);
const usedBy = useQuery<{ service: string; dashboards: string }>(
  () => sql`
    SELECT s.service, group_concat(d.name, ', ') AS dashboards
    FROM dashboard_services s JOIN dashboards d ON d.id = s.dashboard_id GROUP BY s.service`,
);
const rows = computed(() =>
  services.value.map((s) => ({
    ...s,
    ...details.value.find((d) => d.name === s.name),
    usedBy: usedBy.value.find((u) => u.service === s.name)?.dashboards ?? "",
  })),
);
</script>

<template>
  <PageHeader title="Services">
    <Button size="sm" @click="open('service', 0)"><Plus />New service</Button>
  </PageHeader>
  <div class="min-h-0 flex-1 overflow-y-auto p-page">
    <p v-if="signInProblem" class="mb-3 text-destructive" role="alert">{{ signInProblem }}</p>
    <section aria-label="Services" class="rounded-lg border bg-card">
      <ul class="divide-y">
        <li v-for="s in rows" :key="s.id" class="flex min-h-row items-center gap-2 px-3 py-1">
          <span class="size-1.5 shrink-0 rounded-full" :class="{ 'bg-warning': s.needed.length > 0 }"></span>
          <ServiceIcon :url="s.url" />
          <button type="button" class="shrink-0 font-medium hover:underline" @click="open('service', s.id)">{{ s.title }}</button>
          <span v-if="s.error" class="min-w-0 flex-1 truncate text-destructive" :title="s.error">Its file could not be read: {{ s.error }}</span>
          <span v-else class="min-w-0 flex-1 truncate text-muted-foreground">
            {{ s.url }} · {{ s.tools === 1 ? "1 tool" : `${s.tools} tools` }}<template v-if="s.usedBy"> · used by {{ s.usedBy }}</template>
          </span>
          <Button
            v-for="n in s.needed"
            :key="n.tool"
            size="xs"
            class="shrink-0"
            :title="n.heading"
            :disabled="signingIn !== ''"
            @click="signIn(s.name, n.tool)"
          >
            {{ signingIn === `${s.name}/${n.tool}` ? "Signing in…" : n.tool ? "Confirm" : "Sign in" }}
          </Button>
        </li>
      </ul>
      <p v-if="rows.length === 0" class="px-4 py-6 text-center text-muted-foreground">
        No services yet.
      </p>
    </section>
  </div>
</template>
