// What a sign-in asks you beside the page (engine.Prompt in Go), as the
// live_views row has it.
export interface Prompt {
  id: number;
  kind: string;
  site: string;
  realm?: string;
  wrong?: boolean;
  create?: boolean;
  account?: string;
  passkeys?: { id: string; account: string }[];
  keyOnly?: boolean;
  step?: string;
  retries?: number;
  problem?: string;
  dialog?: string;
  message?: string;
  default?: string;
  multiple?: boolean;
  accept?: string;
  name?: string;
  saved?: string;
}
