// The manifest Harbor reads from a service file when it is saved. It matches
// Manifest in backend/service.go.

export interface Annotations {
  readOnlyHint?: boolean;
  destructiveHint?: boolean;
  idempotentHint?: boolean;
  openWorldHint?: boolean;
}

export interface Tool {
  name: string;
  title?: string;
  description: string;
  annotations?: Annotations;
  inputSchema: Record<string, unknown>;
  outputSchema?: Record<string, unknown>;
  login?: Login;
  /** The frame from another site the tool runs in, on page. */
  frame?: { site: string; page?: string; pageFromInput?: boolean };
}

/** Where to sign in; run is set when Harbor can sign in on its own. */
export interface Login {
  url: string;
  run?: boolean;
}

export interface Manifest {
  name: string;
  title: string;
  description?: string;
  url: string;
  login?: Login;
  tools: Tool[];
}

/**
 * What a tool does to the site, as MCP reads its annotations: a tool that is
 * not marked read-only changes data, and is destructive unless it is marked
 * otherwise.
 */
export function effect(tool: Tool): "read-only" | "destructive" | "changes data" {
  if (tool.annotations?.readOnlyHint) {
    return "read-only";
  }
  return tool.annotations?.destructiveHint === false ? "changes data" : "destructive";
}

type Schema = {
  type?: string | string[];
  properties?: Record<string, Schema>;
  required?: string[];
  default?: unknown;
  enum?: unknown[];
};

/**
 * Whether a tool takes an input: its schema declares properties to fill in.
 * A tool that takes one runs from beside its input box; one that takes none
 * runs from a Run button in its row.
 */
export function takesInput(schema: Record<string, unknown>): boolean {
  return Object.keys((schema as Schema).properties ?? {}).length > 0;
}

/**
 * A starting input for a tool, from its inputSchema: each required property
 * with an empty value of its type, for the user to fill in.
 */
export function exampleInput(schema: Record<string, unknown>): string {
  const s = schema as Schema;
  const input: Record<string, unknown> = {};
  for (const name of s.required ?? []) {
    input[name] = emptyValue(s.properties?.[name] ?? {});
  }
  return JSON.stringify(input, null, 2);
}

function emptyValue(s: Schema): unknown {
  if (s.default !== undefined) {
    return s.default;
  }
  if (s.enum?.length) {
    return s.enum[0];
  }
  const type = Array.isArray(s.type) ? s.type[0] : s.type;
  switch (type) {
    case "integer":
    case "number":
      return 0;
    case "boolean":
      return false;
    case "array":
      return [];
    case "object":
      return {};
    default:
      return "";
  }
}
