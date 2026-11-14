import { describe, expect, it } from "vitest";
import { highlight, type Language } from "../highlight";

// Each coloured token, as "class text".
const coloured = (text: string, language: Language) =>
  highlight(text, language)
    .filter((t) => t.class)
    .map((t) => `${t.class} ${t.text}`);

describe("highlight", () => {
  it("tells JSON's keys from its values", () => {
    expect(coloured('{ "a": 1, "b": [true, "x"] }', "json")).toEqual([
      'tok-property "a"',
      "tok-literal 1",
      'tok-property "b"',
      "tok-literal true",
      'tok-string "x"',
    ]);
  });

  it("colours a Vue file's template and script", () => {
    const tokens = coloured('<script setup lang="ts">\nconst a = 1;\n</script>\n<template><p class="x"></p></template>', "vue");
    expect(tokens).toContain("tok-keyword const");
    expect(tokens).toContain("tok-type template");
    expect(tokens).toContain("tok-property class");
  });

  it("keeps every character, with line breaks as tokens", () => {
    const text = 'export default {\n  name: "gitea",\n};';
    const tokens = highlight(text, "typescript");
    expect(tokens.map((t) => t.text).join("")).toBe(text);
    expect(tokens.filter((t) => t.text === "\n")).toHaveLength(2);
  });
});
