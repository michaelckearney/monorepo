// slipway's openLinksInBrowser, from the module slipway generates for every
// app: tested here, in slipway's own app, rather than in each app.
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { openLinksInBrowser } from "@desktop";

const OpenURL = vi.hoisted(() => vi.fn(async (_url: string) => {}));
vi.mock("@wailsio/runtime", () => ({ Browser: { OpenURL }, Call: {}, Events: { On: () => () => {} } }));

beforeAll(() => openLinksInBrowser());
beforeEach(() => {
  OpenURL.mockClear();
  document.body.innerHTML = "";
});

/** Clicks the innermost element in html, with button, and returns whether the window was left to follow the link. */
function click(html: string, button = 0) {
  document.body.innerHTML = html;
  const e = new MouseEvent(button === 0 ? "click" : "auxclick", { button, bubbles: true, cancelable: true });
  [...document.body.querySelectorAll("*")].at(-1)!.dispatchEvent(e);
  return !e.defaultPrevented;
}

describe("links", () => {
  it("open in your browser, not the window", () => {
    expect(click('<a href="https://example.com/a"><span>Read</span></a>')).toBe(false);
    expect(OpenURL).toHaveBeenCalledWith("https://example.com/a");
  });

  it("open with the middle button too", () => {
    expect(click('<a href="mailto:ms.cole@example.com">Email</a>', 1)).toBe(false);
    expect(OpenURL).toHaveBeenCalledWith("mailto:ms.cole@example.com");
  });

  it("go nowhere if they lead nowhere a browser opens", () => {
    expect(click('<a href="/dashboards">Home</a>')).toBe(false);
    expect(OpenURL).not.toHaveBeenCalled();
  });

  it("leave a download to save its file", () => {
    expect(click('<a href="blob:x" download="a.txt">Save</a>')).toBe(true);
    expect(OpenURL).not.toHaveBeenCalled();
  });

  it("open window.open in your browser", () => {
    expect(window.open("https://example.com/b")).toBeNull();
    expect(OpenURL).toHaveBeenCalledWith("https://example.com/b");
    window.open("javascript:alert(1)");
    expect(OpenURL).toHaveBeenCalledTimes(1);
  });
});
