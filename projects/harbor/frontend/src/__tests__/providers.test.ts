import { describe, expect, it } from "vitest";
import { providerProblem, type Provider } from "../providers";

const openai: Provider = {
  name: "openai",
  title: "OpenAI",
  settings: [
    { key: "apiKey", title: "API key", secret: true },
    { key: "model", title: "Model", default: "gpt-5" },
  ],
};

describe("providerProblem", () => {
  it("says what is missing, until nothing is", () => {
    expect(providerProblem([openai], undefined, [])).toBe("Harbor's agent has no provider yet.");
    expect(providerProblem([openai], "gone", [])).toBe("Harbor's agent has no provider yet.");
    expect(providerProblem([openai], "openai", [])).toBe("OpenAI needs its API key.");
    expect(providerProblem([openai], "openai", [{ provider: "other", key: "apiKey", value: "sk" }])).toBe(
      "OpenAI needs its API key.",
    );
    expect(providerProblem([openai], "openai", [{ provider: "openai", key: "apiKey", value: "sk" }])).toBe("");
  });

  it("does not ask for an optional setting", () => {
    const local = { ...openai, settings: [{ ...openai.settings[0], optional: true }, openai.settings[1]] };
    expect(providerProblem([local], "openai", [])).toBe("");
  });
});
