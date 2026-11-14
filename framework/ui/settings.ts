import type { InjectionKey } from "vue";

/** What closes the SettingsDialog a SettingsPane is in. */
export const closeSettingsKey: InjectionKey<() => void> = Symbol("closeSettings");
