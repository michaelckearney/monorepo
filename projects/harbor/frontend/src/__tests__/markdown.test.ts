import { describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";
import Markdown from "@ui/components/Markdown.vue";

// How links open is slipway's openLinksInBrowser, tested in slipway.

describe("Markdown", () => {
  const html = (text: string) => mount(Markdown, { props: { text } }).html();

  it("formats the agent's reply", () => {
    const out = html("**Sam** is due:\n\n- Leaf collection\n- Book report\n\nSee https://example.com");
    expect(out).toContain("<strong>Sam</strong>");
    expect(out).toContain("<li>Leaf collection</li>");
    expect(out).toContain('<a href="https://example.com">https://example.com</a>');
  });

  it("shows HTML as text, and makes no link that runs code", () => {
    const out = html('<img src=x onerror="alert(1)"> [go](javascript:alert(1))');
    expect(out).not.toContain("<img");
    expect(out).toContain("&lt;img");
    expect(out).not.toContain("<a");
  });
});
