// The providers Harbor's agent can use. Provider and Setting match Manifest
// and Setting in backend/provider/provider.go.

export interface Setting {
  key: string;
  title: string;
  description?: string;
  secret?: boolean;
  optional?: boolean;
  default?: string;
}

export interface Provider {
  name: string;
  title: string;
  description?: string;
  settings: Setting[];
}

/**
 * Why a turn stops before it runs when there is no provider, in the words the
 * backend writes as the chat's error: errNoProvider in backend/providers.go.
 * The chat shows it in its needs-you voice rather than its failure one.
 */
export const noProviderError = "Harbor's agent has no provider yet. Set one up in Settings, under Providers";

/**
 * Why the agent cannot run yet, or "" when it can. It matches the backend's
 * check before each turn.
 */
export function providerProblem(
  providers: readonly Provider[],
  active: string | undefined,
  values: readonly { provider: string; key: string; value: string }[],
): string {
  const provider = providers.find((p) => p.name === active);
  if (!provider) {
    return "Harbor's agent has no provider yet.";
  }
  const missing = provider.settings.find(
    (s) => !s.optional && !s.default && !values.some((v) => v.provider === provider.name && v.key === s.key && v.value !== ""),
  );
  return missing ? `${provider.title} needs its ${missing.title}.` : "";
}
