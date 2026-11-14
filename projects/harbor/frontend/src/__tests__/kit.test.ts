import { describe, expect, it } from "vitest";
import { ref } from "vue";
import { column, format, label } from "../kit/data";
import { computed, runtime } from "../kit/runtime";
import { parseThemeCSS } from "../themes";

describe("the dashboard kit", () => {
  it("shows values as their format says", () => {
    expect(format(1234.5, "money")).toBe("$1,234.50");
    expect(format(0.25, "percent")).toBe("25%");
    expect(format(25, "percent")).toBe("25%");
    expect(format("2026-03-04", "date")).toMatch(/^Mar 4/);
    expect(format(undefined)).toBe("—");
    expect(label("due_date")).toBe("Due date");
    expect(label("dueDate")).toBe("Due date");
    expect(column("amount:money")).toMatchObject({ key: "amount", label: "Amount", format: "money" });
  });
});

describe("the dashboard runtime", () => {
  it("throws again from a computed whose read is loading, each time it is read", () => {
    const loading = ref(true);
    const c = computed(() => {
      if (loading.value) {
        throw new Error("loading");
      }
      return 1;
    });
    expect(() => c.value).toThrow("loading");
    expect(() => c.value).toThrow("loading");
    loading.value = false;
    expect(c.value).toBe(1);
  });

  it("keeps a computed's value while a read it makes for a new input loads", async () => {
    const r = runtime({ call: async (_s, _t, input) => ({ n: input.n }), needsSignIn() {}, shown() {} });
    r.setUses([{ service: "s", tool: "get", title: "Get", read: true, destructive: false }]);
    const s = r.useService("s");
    const n = ref(1);
    const c = computed(() => s.get({ n: n.value }).n);
    expect(() => c.value).toThrow("s.get is still loading");
    await new Promise((resolve) => setTimeout(resolve));
    expect(c.value).toBe(1);
    n.value = 2;
    expect(c.value).toBe(1);
    await new Promise((resolve) => setTimeout(resolve));
    expect(c.value).toBe(2);
  });

  it("gives a read's output once it is there, and a stand-in that throws until then", async () => {
    let calls = 0;
    const r = runtime({
      call: async () => (calls++, { items: [1, 2] }),
      needsSignIn() {},
      shown() {},
    });
    r.setUses([{ service: "s", tool: "list", title: "List", read: true, destructive: false }]);
    const s = r.useService("s");
    const first = s.list({ a: 1 });
    expect(() => first.items).toThrow("s.list is still loading");
    // A stand-in can sit in a ref, as Vue checks what it is.
    expect(ref(first).value).toBe(first);
    s.list({ a: 1 });
    await new Promise((resolve) => setTimeout(resolve));
    expect(s.list({ a: 1 }).items).toEqual([1, 2]);
    expect(calls).toBe(1);
    // A tool the dashboard does not use is not there.
    expect(s.other).toBeUndefined();
  });

  it("runs a write only from a widget", async () => {
    const r = runtime({ call: async () => ({}), needsSignIn() {}, shown() {} });
    r.setUses([{ service: "s", tool: "close", title: "Close", read: false, destructive: false }]);
    await expect(r.useService("s").close({})).rejects.toThrow("s.close changes something, so it runs only from something you do in a widget");
  });
});

describe("a theme's CSS", () => {
  it("is its light and dark variables, and only the colour ones", () => {
    expect(
      parseThemeCSS(":root { --primary: oklch(0.5 0.1 200); --radius: 0.5rem; --font-sans: Inter; }\n.dark { --primary: #fff; }"),
    ).toEqual({
      light: { primary: "oklch(0.5 0.1 200)" },
      dark: { primary: "#fff" },
    });
    expect(() => parseThemeCSS("body { color: red }")).toThrow(/:root/);
  });
});
