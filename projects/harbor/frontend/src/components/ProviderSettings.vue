<script setup lang="ts">
import { computed, ref } from "vue";
import { Pencil } from "lucide-vue-next";
import { Providers } from "@backend";
import { Badge } from "@ui/components/badge";
import { Button } from "@ui/components/button";
import { Input } from "@ui/components/input";
import { useProviders } from "../providerStatus";
import IconButton from "@ui/components/IconButton.vue";
import SettingsList from "./SettingsList.vue";
import SettingsPane from "@ui/components/SettingsPane.vue";
import SettingsRow from "./SettingsRow.vue";

// The providers Harbor's agent can use, a row each. A provider's settings
// open in place of the list. Saving one makes it the one the agent uses.
const { providers, failure, inUse, values, status } = useProviders();
const problem = ref("");

// The provider whose settings are open, and what its form holds, by key.
const editing = ref("");
const provider = computed(() => providers.value.find((p) => p.name === editing.value));
const form = ref<Record<string, string>>({});

function edit(name: string) {
  const p = providers.value.find((p) => p.name === name)!;
  form.value = Object.fromEntries(
    p.settings.map((s) => [s.key, values.value.find((v) => v.provider === name && v.key === s.key)?.value ?? ""]),
  );
  problem.value = "";
  editing.value = name;
}

async function save() {
  problem.value = "";
  try {
    await Providers.Save(editing.value, form.value);
    editing.value = "";
  } catch (e) {
    problem.value = e instanceof Error ? e.message : String(e);
  }
}
</script>

<template>
  <SettingsPane v-if="provider" :title="provider.title" back @back="editing = ''">
    <form :aria-label="provider.title" class="grid gap-4" @submit.prevent="save">
      <label v-for="s in provider.settings" :key="s.key" class="grid gap-1.5">
        <span class="font-medium">{{ s.title }}</span>
        <Input
          v-model="form[s.key]"
          :type="s.secret ? 'password' : 'text'"
          :placeholder="s.default ?? (s.optional ? 'Optional' : '')"
          autocomplete="off"
          spellcheck="false"
        />
        <span v-if="s.description" class="text-muted-foreground">{{ s.description }}</span>
      </label>
      <p v-if="problem" class="whitespace-pre-wrap text-destructive" role="alert">{{ problem }}</p>
      <Button type="submit" size="sm" class="justify-self-start">{{ inUse === provider.name ? "Save" : "Save and use" }}</Button>
    </form>
  </SettingsPane>

  <SettingsPane v-else title="Providers">
    <p v-if="status" class="status text-muted-foreground">{{ status }}</p>
    <p v-if="problem || failure" class="whitespace-pre-wrap text-destructive" role="alert">{{ problem || failure }}</p>
    <SettingsList>
      <SettingsRow v-for="p in providers" :key="p.name" :title="p.title" :detail="p.description">
        <template #status>
          <Badge v-if="inUse === p.name" variant="secondary" class="in-use">In use</Badge>
        </template>
        <IconButton v-if="inUse === p.name" label="Edit" :name="`Edit ${p.title}`" size="icon-xs" @click="edit(p.name)">
          <Pencil />
        </IconButton>
        <Button v-else size="xs" variant="outline" @click="edit(p.name)">Set up</Button>
      </SettingsRow>
    </SettingsList>
  </SettingsPane>
</template>
