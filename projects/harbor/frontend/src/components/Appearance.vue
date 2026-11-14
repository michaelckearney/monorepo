<script setup lang="ts">
import { Preferences } from "@backend";
import Appearance from "@ui/components/Appearance.vue";
import SettingsPane from "@ui/components/SettingsPane.vue";
import { useThemes } from "../themes";

// The theme, and whether the window is light or dark. The list of themes, the
// "Add a theme" button, and the preview or editor on the right are the
// shared Appearance part; Harbor keeps the themes in its database and does
// what the part asks. The window wears the editor's draft as you make it.
const { themes, current, mode } = useThemes();

function saveTheme(original: string, name: string, tokens: string) {
  return Preferences.SaveTheme(original, name, tokens);
}
</script>

<template>
  <SettingsPane title="Appearance">
    <Appearance
      :themes="themes"
      :current="current"
      :mode="mode"
      app-name="Harbor"
      :save="saveTheme"
      @mode="Preferences.SetMode"
      @use="Preferences.UseTheme"
      @remove="Preferences.DeleteTheme"
    />
  </SettingsPane>
</template>
