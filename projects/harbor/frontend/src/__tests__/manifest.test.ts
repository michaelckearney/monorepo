import { describe, expect, it } from "vitest";
import { effect, exampleInput, type Tool } from "../manifest";

const tool = (annotations?: Tool["annotations"]): Tool => ({
  name: "t",
  description: "",
  inputSchema: { type: "object" },
  annotations,
});

describe("effect", () => {
  it("follows MCP's defaults", () => {
    expect(effect(tool({ readOnlyHint: true }))).toBe("read-only");
    expect(effect(tool({ readOnlyHint: true, destructiveHint: true }))).toBe("read-only");
    expect(effect(tool({ destructiveHint: false }))).toBe("changes data");
    expect(effect(tool())).toBe("destructive");
    expect(effect(tool({ readOnlyHint: false }))).toBe("destructive");
  });
});

describe("exampleInput", () => {
  it("fills each required property with an empty value of its type", () => {
    const schema = {
      type: "object",
      required: ["owner", "limit", "state", "labels", "draft", "since"],
      properties: {
        owner: { type: "string" },
        limit: { type: "integer" },
        state: { type: "string", enum: ["open", "closed"] },
        labels: { type: "array" },
        draft: { type: "boolean", default: true },
        optional: { type: "string" },
      },
    };
    expect(JSON.parse(exampleInput(schema))).toEqual({
      owner: "",
      limit: 0,
      state: "open",
      labels: [],
      draft: true,
      since: "",
    });
  });

  it("is an empty object for a tool with no input", () => {
    expect(exampleInput({ type: "object" })).toBe("{}");
  });
});
