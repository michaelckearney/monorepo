import { describe, expect, it, vi } from "vitest";
import { flushPromises, mount } from "@vue/test-utils";
import { nextTick } from "vue";
import { serviceOrigin } from "../icons";
import { dark } from "../themes";
import ServiceIcon from "../components/ServiceIcon.vue";

// The rows of service_icons the window's one icon query sees.
const rows = await vi.hoisted(async () => {
  const { ref } = await import("vue");
  return ref<Record<string, unknown>[]>([]);
});
vi.mock("@desktop", () => ({
  sql: (strings: TemplateStringsArray, ...args: unknown[]) => ({ text: strings.join("?"), args }),
  useQuery: () => rows,
}));

describe("a service's origin", () => {
  // backend/favicon/origin_test.go checks Origin against this same table.
  it.each([
    ["https://example.com", "https://example.com"],
    ["https://example.com/", "https://example.com"],
    ["https://example.com/a/b?c=d#e", "https://example.com"],
    ["HTTPS://Example.COM/x", "https://example.com"],
    ["https://example.com:443/x?y#z", "https://example.com"],
    ["http://example.com:80/", "http://example.com"],
    ["http://example.com:8080/path", "http://example.com:8080"],
    ["https://gitea.internal:3000", "https://gitea.internal:3000"],
    ["https://user:pw@example.com/x", "https://example.com"],
    ["http://[::1]:8080/x", "http://[::1]:8080"],
    ["http://[::1]:80/", "http://[::1]"],
    ["http://localhost:9245/app", "http://localhost:9245"],
    ["  https://example.com/x  ", "https://example.com"],
    ["example.com", ""],
    ["//example.com", ""],
    ["mailto:someone@example.com", ""],
    ["file:///Users/x/index.html", ""],
    ["", ""],
    ["   ", ""],
    ["https:/example.com/x", "https://example.com"],
    ["https:example.com", "https://example.com"],
    ["https:///path", "https://path"],
    ["http:////a/b", "http://a"],
    ["https://exa_mple.com/", "https://exa_mple.com"],
    ["https://EXAMPLE.com.", "https://example.com."],
    ["http://例え.jp/", "http://xn--r8jz45g.jp"],
    ["https://example.com:0/", "https://example.com:0"],
  ])("of %j is %j", (url, origin) => {
    expect(serviceOrigin(url)).toBe(origin);
  });
});

describe("a service's icon", () => {
  const light = { origin: "https://bank.localhost:8443", mime_type: "image/png", bytes: "TElHSFQ=" };
  const src = (url = "https://bank.localhost:8443/accounts") => mount(ServiceIcon, { props: { url } });

  it("is the site's favicon, or a plug when it has none", async () => {
    rows.value = [{ ...light, dark_mime_type: null, dark_bytes: null }];
    expect(src().find("img").attributes("src")).toBe("data:image/png;base64,TElHSFQ=");
    expect(src("https://other.localhost").find("img").exists()).toBe(false);
    expect(src("https://other.localhost").find("svg").exists()).toBe(true);
  });

  it("is the site's dark icon in a dark window, and the light one if the dark will not draw", async () => {
    rows.value = [{ ...light, dark_mime_type: "image/svg+xml", dark_bytes: "REFSSw==" }];
    dark.value = true;
    const icon = src();
    expect(icon.find("img").attributes("src")).toBe("data:image/svg+xml;base64,REFSSw==");
    await icon.find("img").trigger("error");
    await nextTick();
    expect(icon.find("img").attributes("src")).toBe("data:image/png;base64,TElHSFQ=");
    dark.value = false;
  });

  it("is a plug when its icon will not draw, until the icons change", async () => {
    rows.value = [{ ...light, dark_mime_type: null, dark_bytes: null }];
    await flushPromises();
    const icon = src();
    await icon.find("img").trigger("error");
    await nextTick();
    expect(icon.find("img").exists()).toBe(false);
    rows.value = [{ ...light, bytes: "TkVX", dark_mime_type: null, dark_bytes: null }];
    await flushPromises();
    expect(icon.find("img").attributes("src")).toBe("data:image/png;base64,TkVX");
  });
});
