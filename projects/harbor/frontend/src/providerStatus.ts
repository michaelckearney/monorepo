import { computed, onMounted, ref } from "vue";
import { sql, useQuery } from "@desktop";
import { Providers } from "@backend";
import { providerProblem, type Provider } from "./providers";

/**
 * The providers, the one in use, and its settings, as they change. status is
 * why the agent cannot run yet, or "".
 */
export function useProviders() {
  const providers = ref<Provider[]>([]);
  const failure = ref("");
  const active = useQuery<{ value: string }>(() => sql`SELECT value FROM settings WHERE key = 'provider'`);
  const values = useQuery<{ provider: string; key: string; value: string }>(
    () => sql`SELECT provider, key, value FROM provider_settings`,
  );
  const inUse = computed(() => active.value[0]?.value);
  const status = computed(() => (providers.value.length ? providerProblem(providers.value, inUse.value, values.value) : ""));
  onMounted(async () => {
    try {
      // Go's slices may be null, as far as the generated types know.
      providers.value = ((await Providers.List()) ?? []).map((p) => ({ ...p, settings: p.settings ?? [] }));
    } catch (e) {
      failure.value = e instanceof Error ? e.message : String(e);
    }
  });
  return { providers, failure, inUse, values, status };
}
