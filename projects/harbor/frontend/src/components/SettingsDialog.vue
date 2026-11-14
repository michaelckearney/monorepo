<script setup lang="ts">
import { Bot, Cable, KeyRound, Palette } from "lucide-vue-next";
import SettingsDialog from "@ui/components/SettingsDialog.vue";
import { settings, type SettingsSection } from "../navigation";
import Appearance from "./Appearance.vue";
import Connections from "./Connections.vue";
import Logins from "./Logins.vue";
import ProviderSettings from "./ProviderSettings.vue";

// Harbor's Settings. hidden keeps it closed while something else must be on
// top, such as a sign-in's live view, and it comes back as it was after.
defineProps<{ hidden?: boolean }>();

const sections: { value: SettingsSection; label: string; icon: typeof Bot }[] = [
  { value: "providers", label: "Providers", icon: Bot },
  { value: "appearance", label: "Appearance", icon: Palette },
  { value: "logins", label: "Logins & passkeys", icon: KeyRound },
  { value: "connections", label: "Connections", icon: Cable },
];
</script>

<template>
  <SettingsDialog :sections="sections" :open="settings" :hidden="hidden" @update:open="(s) => (settings = s as SettingsSection | null)">
    <template #providers><ProviderSettings /></template>
    <template #appearance><Appearance /></template>
    <template #logins><Logins /></template>
    <template #connections><Connections /></template>
  </SettingsDialog>
</template>
