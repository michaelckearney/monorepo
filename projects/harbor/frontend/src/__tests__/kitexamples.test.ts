import { describe, expect, it, vi } from "vitest";
import { mount } from "@vue/test-utils";
import { defineComponent, nextTick } from "vue";
import { kit } from "../dashboards";

// The kit's files, as kit.go reads them: each starts with a comment whose
// "example:" lines, indented, are a template that uses it.
const files = import.meta.glob<string>("../kit/*/*.vue", { query: "?raw", import: "default", eager: true });

function examples(source: string): string[] {
  const head = source.slice("<!--".length, source.indexOf("-->"));
  const out: string[] = [];
  let current: string[] | null = null;
  for (const line of head.split("\n")) {
    if (current && (line.startsWith("  ") || line.trim() === "")) {
      if (line.trim()) {
        current.push(line.slice(2));
      }
      continue;
    }
    if (current?.length) {
      out.push(current.join("\n"));
    }
    current = line.startsWith("example:") ? [] : null;
  }
  if (current?.length) {
    out.push(current.join("\n"));
  }
  return out;
}

// jsdom has no layout, so nothing to observe: Grid and Chart keep their
// first size.
vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    disconnect() {}
  },
);

describe("the kit's examples", () => {
  for (const [path, source] of Object.entries(files)) {
    const name = path.slice(path.lastIndexOf("/") + 1, -".vue".length);
    examples(source).forEach((template, i) => {
      it(`${name} ${i + 1} shows without a warning`, () => {
        const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
        const wrapper = mount(defineComponent({ components: kit, template }));
        expect(wrapper.text().length + wrapper.findAll("*").length).toBeGreaterThan(0);
        expect(warn.mock.calls.map((c) => String(c[0]))).toEqual([]);
        warn.mockRestore();
        wrapper.unmount();
      });
    });
  }

  it("covers every component", () => {
    expect(Object.keys(files).length).toBe(Object.keys(kit).length);
    for (const source of Object.values(files)) {
      expect(examples(source).length).toBeGreaterThan(0);
    }
  });
});

describe("a widget", () => {
  it("hides done tasks with its own switch", async () => {
    const wrapper = mount(
      defineComponent({
        components: kit,
        template: `<TasksWidget title="Chores" :tasks="[{ title: 'Take out the bins', status: 'Open' }, { title: 'Water the plants', status: 'Closed' }]" :done="['Closed']" hide-done />`,
      }),
    );
    await nextTick();
    expect(wrapper.text()).toContain("Take out the bins");
    expect(wrapper.text()).not.toContain("Water the plants");
    await wrapper.get('input[role="switch"]').setValue(false);
    expect(wrapper.text()).toContain("Water the plants");
    wrapper.unmount();
  });

  it("says what is typed in its search once you stop typing, and leaves what it means to the dashboard", async () => {
    vi.useFakeTimers();
    const typed: string[] = [];
    const wrapper = mount(
      defineComponent({
        components: kit,
        setup: () => ({ typed: (q: string) => typed.push(q) }),
        template: `<ListWidget title="Accounts" :items="[{ title: 'Checking' }]" search="Search accounts" @update:query="typed" />`,
      }),
    );
    await wrapper.get('input[type="search"]').setValue("che");
    await wrapper.get('input[type="search"]').setValue("check");
    expect(typed).toEqual([]);
    vi.advanceTimersByTime(300);
    expect(typed).toEqual(["check"]);
    // It filters nothing itself.
    expect(wrapper.text()).toContain("Checking");
    vi.useRealTimers();
    wrapper.unmount();
  });
});
