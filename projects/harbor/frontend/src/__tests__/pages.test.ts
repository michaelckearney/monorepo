import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { enableAutoUnmount, flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h, ref } from "vue";
import { AgentTools, Chats, Documents, MCP, Preferences, Providers, Tools } from "@backend";
import { chat, devView, open, openChat, openSettings, place, settings } from "../navigation";
import App from "../App.vue";
import type { Use } from "../kit/runtime";

// Live queries answer from these tables, chosen by the query's text.
const tables = vi.hoisted(() => ({
  services: [] as Record<string, unknown>[],
  dashboards: [] as Record<string, unknown>[],
  chats: [] as Record<string, unknown>[],
  messages: [] as Record<string, unknown>[],
  settings: [] as Record<string, unknown>[],
  providerSettings: [] as Record<string, unknown>[],
  liveViews: [] as Record<string, unknown>[],
  logins: [] as Record<string, unknown>[],
  passkeys: [] as Record<string, unknown>[],
  themes: [] as Record<string, unknown>[],
  // The themes queries asked, which saving a theme answers again.
  themesQueries: [] as { value: Record<string, unknown>[] }[],
  dashboardServices: [] as Record<string, unknown>[],
  // The settings queries asked, which saveSettings answers again.
  settingsQueries: [] as { keys: string[]; rows: { value: Record<string, unknown>[] } }[],
  // The browser's tabs, by ID, as useTab has them.
  tabs: {} as Record<string, Record<string, unknown>>,
}));

// What useTab's tabs are asked to do, with the tab's ID first.
const tabActions = vi.hoisted(() => ({
  navigate: vi.fn(async (..._args: unknown[]) => {}),
  back: vi.fn(async (..._args: unknown[]) => {}),
  forward: vi.fn(async (..._args: unknown[]) => {}),
  reload: vi.fn(async (..._args: unknown[]) => {}),
  close: vi.fn(async (..._args: unknown[]) => {}),
}));

vi.mock("@desktop", async () => {
  const { defineComponent, h } = await import("vue");
  const blank = { url: "", title: "", loading: false, canGoBack: false, canGoForward: false, opened: [], closed: false };
  return {
  // LiveView draws the tab it shows, and what is over it.
  LiveView: defineComponent({
    props: { tab: { type: String, required: true } },
    setup: (props, { slots }) => () => h("div", { class: "live-view", "data-tab": props.tab }, slots.default?.()),
  }),
  useTab: (id: () => string) =>
    new Proxy(
      {},
      {
        get: (_, key: string) =>
          key in tabActions
            ? (...args: unknown[]) => tabActions[key as keyof typeof tabActions](id(), ...args)
            : key === "id"
              ? id()
              : ({ ...blank, ...tables.tabs[id()] } as Record<string, unknown>)[key],
      },
    ),
  sql: (strings: TemplateStringsArray, ...args: unknown[]) => ({ text: strings.join("?"), args }),
  useQuery: (query: () => { text: string } | null) => {
    const q = query();
    if (!q) {
      return ref([]);
    }
    if (q.text.includes("FROM messages")) {
      return ref(tables.messages);
    }
    if (q.text.includes("FROM settings")) {
      const keys = [...(q.text.match(/key (?:= |IN \()([^)]*)/)?.[1] ?? "").matchAll(/'(\w+)'/g)].map((m) => m[1]);
      const rows = ref(tables.settings.filter((s) => keys.includes(s.key as string)));
      tables.settingsQueries.push({ keys, rows });
      return rows;
    }
    if (q.text.includes("FROM service_icons")) {
      return ref([]);
    }
    if (q.text.includes("FROM themes")) {
      const rows = ref(tables.themes);
      tables.themesQueries.push(rows);
      return rows;
    }
    if (q.text.includes("SELECT s.service")) {
      return ref(tables.dashboardServices);
    }
    if (q.text.includes("FROM provider_settings")) {
      return ref(tables.providerSettings);
    }
    if (q.text.includes("FROM chats")) {
      return ref(tables.chats);
    }
    if (q.text.includes("FROM live_views")) {
      return ref(tables.liveViews);
    }
    if (q.text.includes("FROM logins")) {
      return ref(tables.logins);
    }
    if (q.text.includes("FROM passkeys")) {
      return ref(tables.passkeys);
    }
    return ref(q.text.includes("FROM services") ? tables.services : tables.dashboards);
  },
  };
});

vi.mock("@backend", () => ({
  Documents: {
    ServiceStarter: vi.fn(async () => "starter"),
    DashboardStarter: vi.fn(async () => "<template><p>new</p></template>"),
    SaveService: vi.fn(async () => 7),
    SaveDashboard: vi.fn(async () => 8),
    CompiledDashboard: vi.fn(async () => ({
      js: `module.exports.default = { render: () => require("vue").h("p", { class: "made" }, "made by Vue") }`,
      css: ".made { color: red }",
    })),
    DeleteService: vi.fn(async () => {}),
    DeleteDashboard: vi.fn(async () => {}),
  },
  Chats: {
    NewChat: vi.fn(async () => "chat-1"),
    Send: vi.fn(async () => {}),
    Answer: vi.fn(async () => {}),
    Stop: vi.fn(async () => {}),
    Delete: vi.fn(async () => {}),
  },
  Providers: {
    List: vi.fn(async () => [
      {
        name: "openai",
        title: "OpenAI",
        description: "OpenAI's models.",
        settings: [
          { key: "apiKey", title: "API key", description: "Make one.", secret: true },
          { key: "model", title: "Model", default: "gpt-5" },
        ],
      },
    ]),
    Save: vi.fn(async () => {}),
  },
  AgentTools: {
    List: vi.fn(async () => [
      { name: "list_dashboards", description: "List the dashboards.", inputSchema: '{"type":"object"}' },
      {
        name: "read_dashboard",
        description: "Read a dashboard's file.",
        inputSchema: '{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}',
      },
    ]),
    Asks: vi.fn(async (_tool: string, input: string) => input.includes("Secret")),
    Call: vi.fn(async () => '{"error":"there is no dashboard named Nope"}'),
  },
  Preferences: {
    UseTheme: vi.fn(async () => {}),
    SetMode: vi.fn(async () => {}),
    SaveTheme: vi.fn(async (original: string, name: string, tokens: string) => {
      const rows = tables.themes.filter((t) => t.name !== name && t.name !== original).concat({ name, tokens });
      tables.themes = rows;
      for (const q of tables.themesQueries) q.value = rows;
      saveSettings(tables.settings.filter((s) => s.key !== "theme").concat({ key: "theme", value: name }));
    }),
    DeleteTheme: vi.fn(async () => {}),
    OpenedDashboard: vi.fn(async () => {}),
    SetPanel: vi.fn(async () => {}),
  },
  MCP: {
    Connection: vi.fn(async () => ({ url: "http://127.0.0.1:7331/mcp", token: "secret" })),
    NewToken: vi.fn(async () => ({ url: "http://127.0.0.1:7331/mcp", token: "fresh" })),
  },
  Tools: {
    SignedIn: vi.fn(async () => false),
    SignIn: vi.fn(async () => true),
    Call: vi.fn(async () => '{"issues":[1,2]}'),
    CallForDashboard: vi.fn(async () => '{"issues":[1,2]}'),
    CloseView: vi.fn(async () => {}),
    Browse: vi.fn(async () => {}),
    SaveLogin: vi.fn(async () => {}),
    DeleteLogin: vi.fn(async () => {}),
    UsePasskeyWithoutAsking: vi.fn(async () => {}),
    DeletePasskey: vi.fn(async () => {}),
    File: vi.fn(async () => ({ name: "march.pdf", type: "application/pdf", data: "JVBERg==" })),
    AddFile: vi.fn(async () => ({ file: "f9", name: "cat.png", type: "image/png", size: 3 })),
    Answer: vi.fn(async () => {}),
  },
}));

// CodeMirror needs a real browser; the e2e tests use it. Here the editor is a
// textarea with the same model.
const CodeEditor = defineComponent({
  props: { modelValue: String, label: String },
  emits: ["update:modelValue"],
  setup: (props, { emit }) => () =>
    h("textarea", {
      "aria-label": props.label,
      value: props.modelValue,
      onInput: (e: Event) => emit("update:modelValue", (e.target as HTMLTextAreaElement).value),
    }),
});

const manifest = {
  name: "gitea",
  title: "Gitea",
  description: "Read issues.",
  url: "http://localhost:7100",
  login: { url: "/user/login" },
  tools: [
    {
      name: "list_issues",
      description: "List issues.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", required: ["repo"], properties: { repo: { type: "string" } } },
    },
    { name: "close_issue", description: "Close an issue.", inputSchema: {} },
  ],
};

// A window left from an earlier test would open a dashboard when place is reset.
enableAutoUnmount(afterEach);

// Settings saved while the window is open, as its live queries see them.
function saveSettings(rows: Record<string, unknown>[]) {
  tables.settings = rows;
  for (const q of tables.settingsQueries) {
    q.rows.value = rows.filter((s) => q.keys.includes(s.key as string));
  }
}

const mountApp = () => mount(App, { global: { stubs: { CodeEditor } } });
const button = (app: { findAll: ReturnType<typeof mountApp>["findAll"] }, text: string) =>
  app.findAll("button").find((b) => b.text() === text)!;

// jsdom has none of these; the window has all of them.
window.matchMedia = vi.fn(() => ({ matches: false, addEventListener() {} })) as unknown as typeof window.matchMedia;
const colourWords = new Set(["red", "pink", "blue", "green", "grey", "navy", "white", "black", "transparent"]);
globalThis.CSS = {
  supports: (property: string, value: string) =>
    property === "color" &&
    (colourWords.has(value.toLowerCase()) || /^#[0-9a-f]{6}$/i.test(value) || /^(rgb|hsl|oklch|oklab|hwb|color)\(/.test(value)),
} as unknown as typeof globalThis.CSS;
globalThis.ResizeObserver = class {
  constructor(private fn: ResizeObserverCallback) {}
  observe() {
    this.fn([{ contentRect: { width: 900 } } as ResizeObserverEntry], this as unknown as ResizeObserver);
  }
  disconnect() {}
} as unknown as typeof ResizeObserver;

beforeEach(() => {
  vi.clearAllMocks();
  place.value = null;
  settings.value = null;
  chat.value = "";
  tables.services = [
    { id: 1, title: "Gitea", name: "gitea", source: "old source", manifest: JSON.stringify(manifest) },
  ];
  tables.dashboards = [{ id: 2, name: "Inbox", source: "<template><p>inbox</p></template>" }];
  tables.chats = [];
  tables.messages = [];
  tables.settings = [{ key: "provider", value: "openai" }];
  tables.providerSettings = [{ provider: "openai", key: "apiKey", value: "sk-1" }];
  tables.liveViews = [];
  tables.tabs = {};
  tables.logins = [];
  tables.passkeys = [];
  tables.themes = [];
  tables.themesQueries = [];
  tables.dashboardServices = [];
});

describe("the window", () => {
  it("opens to the dashboard you last opened, or your first", async () => {
    tables.dashboards = [
      { id: 2, name: "Inbox", updated_at: "2026-10-12 10:00:00" },
      { id: 3, name: "Money", updated_at: "2026-10-12 10:00:00" },
    ];
    mountApp();
    expect(place.value).toEqual({ kind: "dashboard", id: 2, editing: false });
    place.value = null;
    tables.settings = [{ key: "last_dashboard", value: "3" }];
    mountApp();
    expect(place.value).toEqual({ kind: "dashboard", id: 3, editing: false });
  });

  it("offers to make a dashboard when there is none", async () => {
    tables.dashboards = [];
    const app = mountApp();
    expect(place.value).toBeNull();
    expect(app.text()).toContain("No dashboards yet");
    await button(app, "New dashboard").trigger("click");
    expect(place.value).toEqual({ kind: "dashboard", id: 0, editing: true });
  });

  it("lists dashboards and chats in the sidebar, and remembers the dashboard you open", async () => {
    tables.chats = [{ id: "c9", title: "Fix the inbox" }];
    const app = mountApp();
    expect(app.get('[aria-label="Dashboards"] li').text()).toBe("Inbox");
    expect(app.get('[aria-label="Chats"] li').text()).toBe("Fix the inbox");
    await app.get('[aria-label="Dashboards"] li button').trigger("click");
    expect(Preferences.OpenedDashboard).toHaveBeenCalledWith(2);
    await app.get('[aria-label="Chats"] li button').trigger("click");
    expect(chat.value).toBe("c9");
  });

  it("marks Services in the sidebar when one needs you, and signs in from its row", async () => {
    tables.services = [
      { id: 1, name: "gitea", title: "Gitea", manifest: JSON.stringify(manifest), tool: "", heading: "Sign in to Gitea" },
      { id: 4, name: "bank", title: "Bank", manifest: JSON.stringify(manifest), tool: null, heading: null },
    ];
    const app = mountApp();
    const services = app.findAll("button").find((b) => b.text().startsWith("Services"))!;
    expect(services.attributes("aria-description")).toBe("A service needs you to sign in");
    await services.trigger("click");
    const rows = app.findAll('section[aria-label="Services"] li');
    expect(rows.map((r) => r.find("button").text())).toEqual(["Gitea", "Bank"]);
    await button(rows[0], "Sign in").trigger("click");
    expect(Tools.SignIn).toHaveBeenCalledWith("gitea", "");
  });

  it("lists every service on the Services page, and opens one", async () => {
    const app = mountApp();
    await button(app, "Services").trigger("click");
    const services = app.get('section[aria-label="Services"]');
    await services.get("li button").trigger("click");
    expect(app.get("h1").text()).toBe("Gitea");
  });
});

describe("a service", () => {
  it("shows its tools and what each does to the site", async () => {
    open("service", 1);
    const app = mountApp();
    await flushPromises();
    const tools = app.findAll('[aria-label="Tools"] li');
    expect(tools.map((t) => t.get(".effect").text())).toEqual(["read-only", "destructive"]);
    expect(app.text()).toContain("Sign in at http://localhost:7100/user/login");
  });

  it("opens in Harbor's browser", async () => {
    open("service", 1);
    const app = mountApp();
    await flushPromises();
    await button(app, "Open in Harbor's browser").trigger("click");
    expect(Tools.Browse).toHaveBeenCalledWith("gitea");
  });

  it("asks before it deletes", async () => {
    open("service", 1);
    const app = mountApp();
    await app.get('main button[aria-label^="Delete "]').trigger("click");
    expect(Documents.DeleteService).not.toHaveBeenCalled();
    await button(app, "Yes, delete").trigger("click");
    await flushPromises();
    expect(Documents.DeleteService).toHaveBeenCalledWith(1);
    expect(place.value).toEqual({ kind: "services" });
  });

  it("goes back to the services list", async () => {
    open("service", 1);
    const app = mountApp();
    await flushPromises();
    await app.get('main button[aria-label="Back"]').trigger("click");
    expect(place.value).toEqual({ kind: "services" });
  });

  it("goes back from a service whose file it could not read", async () => {
    tables.services = [{ id: 1, name: "gitea", title: "Gitea", manifest: null, error: "no such file" }];
    open("service", 1);
    const app = mountApp();
    await flushPromises();
    expect(app.text()).toContain("Couldn't read this service's file");
    await app.get('main button[aria-label="Back"]').trigger("click");
    expect(place.value).toEqual({ kind: "services" });
  });

  it("edits the saved file and shows it after saving", async () => {
    open("service", 1, true);
    const app = mountApp();
    await flushPromises();
    const editor = app.get("textarea");
    expect((editor.element as HTMLTextAreaElement).value).toBe("old source");
    await editor.setValue("new source");
    await app.get("main form").trigger("submit");
    await flushPromises();
    expect(Documents.SaveService).toHaveBeenCalledWith(1, "new source");
    expect(place.value).toEqual({ kind: "service", id: 7, editing: false });
  });

  it("shows each problem and stays in the editor", async () => {
    vi.mocked(Documents.SaveService).mockRejectedValueOnce(new Error("name: is required\nurl: is required"));
    open("service", 0);
    const app = mountApp();
    await flushPromises();
    expect((app.get("textarea").element as HTMLTextAreaElement).value).toBe("starter");
    await app.get("main form").trigger("submit");
    await flushPromises();
    expect(app.findAll('[role="alert"] li').map((li) => li.text())).toEqual([
      "name: is required",
      "url: is required",
    ]);
    expect(place.value).toMatchObject({ editing: true });
  });
});

describe("signing in", () => {
  it("checks, then signs in when asked", async () => {
    open("service", 1);
    const app = mountApp();
    await flushPromises();
    const status = app.get('[aria-label="Sign-in"]');
    expect(Tools.SignedIn).toHaveBeenCalledWith("gitea");
    expect(status.text()).toContain("Signed out");
    expect(status.get(".dot.need").exists()).toBe(true);
    await status.get("button").trigger("click");
    await flushPromises();
    expect(Tools.SignIn).toHaveBeenCalledWith("gitea", "");
    expect(status.text()).toBe("Signed in");
    expect(status.get(".dot.ok").exists()).toBe(true);
  });

  it("says what went wrong, and checks again", async () => {
    vi.mocked(Tools.SignedIn).mockRejectedValueOnce(new Error("could not start Chromium"));
    open("service", 1);
    const app = mountApp();
    await flushPromises();
    const status = app.get('[aria-label="Sign-in"]');
    expect(status.text()).toContain("could not start Chromium");
    await status.get("button").trigger("click");
    await flushPromises();
    expect(status.text()).toContain("Signed out");
  });
});

describe("the live view of a sign-in", () => {
  const view = (row: Record<string, unknown>) => ({ tab: "t1", prompt: null, ...row });

  it("shows the service's tab over any page while a sign-in waits", async () => {
    const app = mountApp();
    await flushPromises();
    expect(app.find('[role="dialog"]').exists()).toBe(false);
    tables.liveViews = [view({ service: "gitea", heading: "Sign in to Gitea", browse: 0 })];
    const shown = mountApp();
    await flushPromises();
    const dialog = shown.get('[role="dialog"]');
    expect(dialog.attributes("aria-label")).toBe("Sign in to Gitea");
    expect(dialog.get(".live-view").attributes("data-tab")).toBe("t1");
  });

  it("goes back, and to an address you type, such as an emailed link; and cancels", async () => {
    tables.liveViews = [view({ service: "gitea", heading: "Sign in to Gitea", browse: 0 })];
    tables.tabs.t1 = { url: "https://gitea.localhost/user/login", canGoBack: true };
    const app = mountApp();
    await flushPromises();
    const address = app.get('input[aria-label="Address"]');
    expect((address.element as HTMLInputElement).value).toBe("https://gitea.localhost/user/login");
    expect(app.get('button[aria-label="Forward"]').attributes("disabled")).toBeDefined();
    await app.get('button[aria-label="Back"]').trigger("click");
    expect(tabActions.back).toHaveBeenCalledWith("t1");
    await address.setValue("gitea.localhost/magic?token=1");
    await app.get('form[aria-label="Address bar"]').trigger("submit");
    expect(tabActions.navigate).toHaveBeenCalledWith("t1", "https://gitea.localhost/magic?token=1");
    await button(app, "Cancel").trigger("click");
    expect(Tools.CloseView).toHaveBeenCalledWith("gitea");
  });

  it("lists the tabs it opened, shows the one you choose, and closes one", async () => {
    tables.liveViews = [view({ service: "gitea", heading: "Sign in to Gitea", browse: 0 })];
    tables.tabs.t1 = { url: "https://gitea.localhost/", title: "Gitea", opened: ["t2"] };
    tables.tabs.t2 = { url: "https://accounts.example.com/", title: "Sign in with Example" };
    const app = mountApp();
    await flushPromises();
    const tabs = app.get('[aria-label="Tabs"]');
    expect(tabs.findAll("li").map((li) => li.find("button").text())).toEqual(["Gitea", "Sign in with Example"]);
    await button(app, "Sign in with Example").trigger("click");
    expect(app.get(".live-view").attributes("data-tab")).toBe("t2");
    await app.get('button[aria-label="Close Sign in with Example"]').trigger("click");
    expect(tabActions.close).toHaveBeenCalledWith("t2");
  });

  // A live view that shows prompt over the page.
  function asks(prompt: Record<string, unknown>) {
    tables.liveViews = [view({ service: "router", heading: "Sign in to Router", browse: 0, prompt: JSON.stringify(prompt) })];
  }
  const answered = (reply: Record<string, unknown>) =>
    expect(Tools.Answer).toHaveBeenCalledWith("router", {
      cancel: false,
      username: "",
      password: "",
      passkey: "",
      withoutAsking: false,
      securityKey: false,
      pin: "",
      text: "",
      files: [],
      ...reply,
    });

  it("asks for a password a site asks the browser for, over the page", async () => {
    asks({ id: 3, kind: "password", site: "router.home.localhost:8443", realm: "Homebase router", wrong: true });
    const app = mountApp();
    await flushPromises();
    const form = app.get('.live-view form[aria-label="Password for router.home.localhost:8443"]');
    expect(form.text()).toContain("router.home.localhost:8443 asks for a password for “Homebase router”.");
    expect(form.text()).toContain("That username or password was wrong.");
    const [user, pass] = form.findAll("input");
    await user.setValue("alex");
    await pass.setValue("letmein");
    await form.trigger("submit");
    answered({ prompt: 3, username: "alex", password: "letmein" });
  });

  it("answers a page's dialog", async () => {
    asks({ id: 12, kind: "dialog", site: "router.home.localhost:8443", dialog: "prompt", message: "Your name?", default: "Ada" });
    const app = mountApp();
    await flushPromises();
    const form = app.get('form[aria-label="router.home.localhost:8443 says"]');
    expect(form.text()).toContain("router.home.localhost:8443 says: Your name?");
    expect((form.get("input").element as HTMLInputElement).value).toBe("Ada");
    await form.get("input").setValue("Alex");
    await form.trigger("submit");
    answered({ prompt: 12, text: "Alex" });
  });

  it("says where a download went", async () => {
    asks({ id: 13, kind: "downloaded", site: "router.home.localhost:8443", name: "backup.cfg", saved: "/Users/a/Downloads/backup.cfg" });
    const app = mountApp();
    await flushPromises();
    expect(app.get('[aria-label="Downloaded backup.cfg"]').text()).toContain("It is in /Users/a/Downloads/backup.cfg.");
    await button(app, "OK").trigger("click");
    answered({ prompt: 13 });
  });

  it("ends looking around with Done", async () => {
    tables.liveViews = [view({ service: "gitea", heading: "Gitea", browse: 1 })];
    const app = mountApp();
    await flushPromises();
    expect(app.get('[role="dialog"]').attributes("aria-label")).toBe("Gitea");
    expect(app.text()).not.toContain("Harbor carries on once you have signed in.");
    await button(app, "Done").trigger("click");
    expect(Tools.CloseView).toHaveBeenCalledWith("gitea");
  });

  it("offers to save the login you typed", async () => {
    asks({ id: 8, kind: "save-login", site: "tidewater.localhost:8443", account: "alex@example.localhost" });
    const app = mountApp();
    await flushPromises();
    expect(app.get('[aria-label="Save the login for tidewater.localhost:8443"]').text()).toContain(
      "Save the login for tidewater.localhost:8443 (alex@example.localhost)?",
    );
    await button(app, "Save login").trigger("click");
    answered({ prompt: 8 });
  });

  it("asks before saving a passkey", async () => {
    asks({ id: 4, kind: "passkey", site: "shoebox.localhost", create: true, account: "alex@example.localhost" });
    const app = mountApp();
    await flushPromises();
    expect(app.get('[aria-label="Passkey for shoebox.localhost"]').text()).toContain(
      "shoebox.localhost asks to save a passkey for alex@example.localhost: in Harbor",
    );
    await button(app, "Save passkey").trigger("click");
    answered({ prompt: 4 });
  });

  it("uses a security key: asks for its PIN, then to touch it", async () => {
    asks({ id: 9, kind: "passkey", site: "shoebox.localhost", create: true, account: "alex", keyOnly: true });
    const app = mountApp();
    await flushPromises();
    expect(button(app, "Save passkey")).toBeUndefined();
    await button(app, "Use a security key").trigger("click");
    answered({ prompt: 9, securityKey: true });

    asks({ id: 10, kind: "security-key", site: "shoebox.localhost", step: "pin", retries: 7, problem: "that PIN is not right" });
    const pinApp = mountApp();
    await flushPromises();
    const form = pinApp.get('form[aria-label="Security key for shoebox.localhost"]');
    expect(form.text()).toContain("that PIN is not right");
    expect(form.text()).toContain("7 tries left");
    await form.get('input[type="password"]').setValue("1234");
    await form.trigger("submit");
    answered({ prompt: 10, pin: "1234" });

    asks({ id: 11, kind: "security-key", site: "shoebox.localhost", step: "touch" });
    const touchApp = mountApp();
    await flushPromises();
    const touch = touchApp.get('form[aria-label="Security key for shoebox.localhost"]');
    expect(touch.text()).toContain("Touch your security key.");
    await touch.findAll("button").find((b) => b.text() === "Cancel")!.trigger("click");
    answered({ prompt: 11, cancel: true });
  });

  it("asks which passkey to sign in with, or says there is none", async () => {
    asks({ id: 5, kind: "passkey", site: "shoebox.localhost", passkeys: [{ id: "a1", account: "alex@example.localhost" }] });
    const app = mountApp();
    await flushPromises();
    await app.get('input[type="checkbox"]').setValue(true);
    await button(app, "alex@example.localhost").trigger("click");
    answered({ prompt: 5, passkey: "a1", withoutAsking: true });

    asks({ id: 6, kind: "passkey", site: "shoebox.localhost", passkeys: null });
    const none = mountApp();
    await flushPromises();
    expect(none.text()).toContain("shoebox.localhost asks for a passkey, but Harbor has none for it.");
    await button(none, "OK").trigger("click");
    answered({ prompt: 6, cancel: true });
  });
});

describe("running a tool", () => {
  it("saves a file it returned", async () => {
    vi.mocked(Tools.Call).mockResolvedValueOnce(
      JSON.stringify({ pdf: { file: "f1", name: "march.pdf", type: "application/pdf", size: 4 } }) as never,
    );
    open("service", 1);
    const app = mountApp();
    await flushPromises();
    await app.get('[aria-label="Run list_issues"]').trigger("click");
    const form = app.get('form[aria-label="Run list_issues"]');
    await form.get("textarea").setValue("{}");
    await form.trigger("submit");
    await flushPromises();
    const save = form.findAll("button").find((b) => b.text() === "Save march.pdf")!;
    URL.createObjectURL = vi.fn(() => "blob:x");
    URL.revokeObjectURL = vi.fn();
    await save.trigger("click");
    await flushPromises();
    expect(Tools.File).toHaveBeenCalledWith("f1");
  });

  it("starts from the required inputs and shows the output", async () => {
    open("service", 1);
    const app = mountApp();
    await flushPromises();
    await app.get('[aria-label="Run list_issues"]').trigger("click");
    const form = app.get('form[aria-label="Run list_issues"]');
    const input = form.get("textarea");
    expect(JSON.parse((input.element as HTMLTextAreaElement).value)).toEqual({ repo: "" });
    await input.setValue('{"repo":"harbor"}');
    await form.trigger("submit");
    await flushPromises();
    expect(Tools.Call).toHaveBeenCalledWith("gitea", "list_issues", '{"repo":"harbor"}');
    expect(form.get("pre").text()).toBe(JSON.stringify({ issues: [1, 2] }, null, 2));
  });

  it("shows the error, and does not send input that is not JSON", async () => {
    vi.mocked(Tools.Call).mockRejectedValueOnce(new Error("sign in needed: sign in to Gitea first"));
    open("service", 1);
    const app = mountApp();
    await flushPromises();
    await app.get('[aria-label="Run list_issues"]').trigger("click");
    const form = app.get('form[aria-label="Run list_issues"]');
    await form.get("textarea").setValue("{nope");
    await form.trigger("submit");
    expect(form.get('[role="alert"]').text()).toBe("The input is not JSON.");
    expect(Tools.Call).not.toHaveBeenCalled();
    await form.get("textarea").setValue("{}");
    await form.trigger("submit");
    await flushPromises();
    expect(form.get('[role="alert"]').text()).toBe("sign in needed: sign in to Gitea first");
  });
});

describe("a dashboard", () => {
  it("shows the compiled dashboard and its styles", async () => {
    tables.dashboards = [{ id: 2, name: "Inbox", updated_at: "2026-10-12 10:00:00" }];
    open("dashboard", 2);
    const app = mountApp();
    await flushPromises();
    expect(Documents.CompiledDashboard).toHaveBeenCalledWith(2);
    expect(app.get('[aria-label="Dashboard"] p.made').text()).toBe("made by Vue");
    expect([...document.head.querySelectorAll("style")].some((s) => s.textContent === ".made { color: red }")).toBe(true);
    app.unmount();
  });

  // A dashboard as the compiler makes it: each widget in a Bound, with the
  // tools its props and events reach, and its props worked out inside it.
  function compiled(setup: string, uses: [string, string, Partial<Use>?][]) {
    return {
      js: `const { h, ref, computed } = require("vue")
        const harbor = require("harbor")
        const { useService, Bound } = harbor
        const bound = (uses, title, widget) => h(Bound, { uses, frameTitle: () => title }, () => widget())
        module.exports.default = { setup() {
          ${setup}
        } }`,
      css: "",
      uses: uses.map(([service, tool, u]) => ({ service, tool, title: tool, read: true, destructive: false, ...u })),
    };
  }
  const signInTo = (service: string, title: string) =>
    Object.assign(new Error("sign in needed"), { cause: { service, heading: `Sign in to ${title}`, message: `sign in to ${title} first` } });

  it("shows widgets whose reads load, each asking for its own sign-in", async () => {
    tables.services.push({ id: 4, title: "Bank", name: "bank", manifest: JSON.stringify(manifest) });
    tables.dashboards = [{ id: 2, name: "Inbox", updated_at: "2026-10-12 10:00:00" }];
    vi.mocked(Documents.CompiledDashboard).mockResolvedValueOnce(
      compiled(
        `const gitea = useService("gitea")
        const bank = useService("bank")
        const count = computed(() => gitea.list_issues({ repo: "harbor" }).count)
        const rows = computed(() => gitea.list_issues({ repo: "harbor" }).issues)
        const balance = computed(() => bank.balance().balance)
        return () => h(harbor.Grid, null, () => [
          bound([["gitea", "list_issues"]], "Open", () => h(harbor.MetricWidget, { title: "Open", value: count.value })),
          bound([["gitea", "list_issues"]], "Issues", () => h(harbor.TableWidget, { title: "Issues", records: rows.value, columns: ["title", "votes:number"] })),
          bound([["bank", "balance"]], "Balance", () => h(harbor.MetricWidget, { title: "Balance", value: balance.value, format: "money" })),
        ])`,
        [
          ["gitea", "list_issues"],
          ["bank", "balance"],
        ],
      ),
    );
    vi.mocked(Tools.CallForDashboard).mockImplementation((async (_id: number, service: string) => {
      if (service === "bank") {
        throw signInTo("bank", "Bank");
      }
      return '{"count":2,"issues":[{"title":"Crash","votes":1200}]}';
    }) as never);
    open("dashboard", 2);
    const app = mountApp();
    await flushPromises();
    expect(Tools.CallForDashboard).toHaveBeenCalledWith(2, "gitea", "list_issues", '{"repo":"harbor"}');
    // Two widgets read the same thing, and it was asked for once.
    expect(vi.mocked(Tools.CallForDashboard).mock.calls.filter((c) => c[1] === "gitea")).toHaveLength(1);
    expect(app.get('section[aria-label="Open"]').text()).toContain("2");
    expect(app.get('section[aria-label="Issues"] tbody').text()).toBe("Crash1,200");
    const balance = app.get('section[aria-label="Balance"]');
    expect(balance.text()).toContain("Sign in to Bank to see this.");
    // The widget offers it, so the page does not.
    expect(app.find('[aria-label="Sign-ins needed"]').exists()).toBe(false);
    const calls = vi.mocked(Tools.CallForDashboard).mock.calls.length;
    // Its icon only names the service; the sentence signs in.
    expect(balance.find('[role="img"][aria-label="Bank"]').exists()).toBe(true);
    await button(balance, "Bank").trigger("click");
    await flushPromises();
    expect(Tools.SignIn).toHaveBeenCalledWith("bank", "");
    expect(vi.mocked(Tools.CallForDashboard).mock.calls.length).toBeGreaterThan(calls);
    vi.mocked(Tools.CallForDashboard).mockReset();
    app.unmount();
  });

  it("shows a widget loading until its reads are there, and why one failed, leaving the rest", async () => {
    tables.dashboards = [{ id: 2, name: "Week", updated_at: "2026-10-12 10:00:00" }];
    vi.mocked(Documents.CompiledDashboard).mockResolvedValueOnce(
      compiled(
        `const school = useService("school")
        const clinic = useService("clinic")
        const events = computed(() => school.events().events.map((e) => ({ start: e.date, title: e.what })))
        const visits = computed(() => clinic.visits().visits)
        return () => h(harbor.Grid, null, () => [
          bound([["school", "events"]], "School", () => h(harbor.AgendaWidget, { title: "School", events: events.value })),
          bound([["school", "events"]], "Again", () => h(harbor.AgendaWidget, { title: "Again", events: events.value })),
          bound([["clinic", "visits"]], "Clinic", () => h(harbor.ListWidget, { title: "Clinic", items: visits.value })),
        ])`,
        [
          ["school", "events"],
          ["clinic", "visits"],
        ],
      ),
    );
    let arrive = () => {};
    vi.mocked(Tools.CallForDashboard).mockImplementation((async (_id: number, service: string) => {
      if (service === "clinic") {
        throw new Error("the clinic's site is down");
      }
      await new Promise<void>((resolve) => (arrive = resolve));
      return '{"events":[{"date":"2026-11-04","what":"Book fair"}]}';
    }) as never);
    open("dashboard", 2);
    const app = mountApp();
    await flushPromises();
    // Both widgets that read it show it loading, not just the first.
    expect(app.find('section[aria-label="School"] [aria-label="Loading"]').exists()).toBe(true);
    expect(app.find('section[aria-label="Again"] [aria-label="Loading"]').exists()).toBe(true);
    expect(app.get('section[aria-label="Clinic"] [role="alert"]').text()).toBe("the clinic's site is down");
    arrive();
    await flushPromises();
    expect(app.get('section[aria-label="School"]').text()).toContain("Book fair");
    expect(app.get('section[aria-label="Again"]').text()).toContain("Book fair");
    expect(vi.mocked(Tools.CallForDashboard).mock.calls.filter((c) => c[1] === "school")).toHaveLength(1);
    vi.mocked(Tools.CallForDashboard).mockReset();
    app.unmount();
  });

  it("loads its data again on refresh, showing what it had until then", async () => {
    tables.dashboards = [{ id: 2, name: "Issues", updated_at: "2026-10-12 10:00:00" }];
    vi.mocked(Documents.CompiledDashboard).mockResolvedValueOnce(
      compiled(
        `const gitea = useService("gitea")
        const items = computed(() => gitea.list_issues().issues.map((i) => ({ title: i.title })))
        return () => bound([["gitea", "list_issues"]], "Issues", () => h(harbor.ListWidget, { title: "Issues", items: items.value }))`,
        [["gitea", "list_issues"]],
      ),
    );
    let n = 0;
    let finish = () => {};
    vi.mocked(Tools.CallForDashboard).mockImplementation((async () => {
      n++;
      if (n === 2) {
        await new Promise<void>((resolve) => (finish = resolve));
      }
      return JSON.stringify({ issues: [{ title: `Crash ${n}` }] });
    }) as never);
    open("dashboard", 2);
    const app = mountApp();
    await flushPromises();
    expect(app.get('section[aria-label="Issues"]').text()).toContain("Crash 1");
    await app.get('button[aria-label="Refresh"]').trigger("click");
    await flushPromises();
    expect(app.get('section[aria-label="Issues"]').text()).toContain("Crash 1");
    expect(app.find('section[aria-label="Issues"] [aria-label="Refreshing"]').exists()).toBe(true);
    finish();
    await flushPromises();
    expect(app.get('section[aria-label="Issues"]').text()).toContain("Crash 2");
    vi.mocked(Tools.CallForDashboard).mockReset();
    app.unmount();
  });

  it("reads only once its input is there, as a search does", async () => {
    tables.dashboards = [{ id: 2, name: "Search", updated_at: "2026-10-12 10:00:00" }];
    vi.mocked(Documents.CompiledDashboard).mockResolvedValueOnce(
      compiled(
        `const chat = useService("chat")
        const query = ref("")
        const found = computed(() => (query.value ? chat.search({ query: query.value }).messages.map((m) => ({ title: m.text })) : []))
        return () => bound([["chat", "search"]], "Search", () =>
          h(harbor.ListWidget, { title: "Search", items: found.value, search: "Search messages", query: query.value, "onUpdate:query": (q) => (query.value = q), empty: "" }))`,
        [["chat", "search"]],
      ),
    );
    vi.mocked(Tools.CallForDashboard).mockResolvedValue('{"messages":[{"text":"the deploy is done"}]}');
    open("dashboard", 2);
    const app = mountApp();
    await flushPromises();
    expect(Tools.CallForDashboard).not.toHaveBeenCalled();
    vi.useFakeTimers();
    await app.get('section[aria-label="Search"] input[type="search"]').setValue("deploy");
    vi.advanceTimersByTime(300);
    vi.useRealTimers();
    await flushPromises();
    expect(Tools.CallForDashboard).toHaveBeenCalledWith(2, "chat", "search", '{"query":"deploy"}');
    expect(app.get('section[aria-label="Search"]').text()).toContain("the deploy is done");
    vi.mocked(Tools.CallForDashboard).mockReset();
    app.unmount();
  });

  it("runs a write from a widget, asking there first if it is destructive, then reads again", async () => {
    tables.dashboards = [{ id: 2, name: "Lane", updated_at: "2026-10-12 10:00:00" }];
    vi.mocked(Documents.CompiledDashboard).mockResolvedValueOnce(
      compiled(
        `const lane = useService("lane")
        const tasks = computed(() => lane.list().issues.map((i) => ({ id: i.id, title: i.title, status: i.state })))
        return () => bound([["lane", "list"], ["lane", "close"]], "Issues", () =>
          h(harbor.TasksWidget, { title: "Issues", tasks: tasks.value, act: "Close", onAct: (t) => lane.close({ id: t.id }) }))`,
        [
          ["lane", "list"],
          ["lane", "close", { title: "Close the issue", read: false, destructive: true }],
        ],
      ),
    );
    let closed = false;
    vi.mocked(Tools.CallForDashboard).mockImplementation((async (_id: number, _service: string, tool: string) => {
      if (tool === "close") {
        closed = true;
        return "{}";
      }
      return JSON.stringify({ issues: [{ id: 7, title: "Crash", state: closed ? "Done" : "Open" }] });
    }) as never);
    open("dashboard", 2);
    const app = mountApp();
    await flushPromises();
    const widget = app.get('section[aria-label="Issues"]');
    await widget.get("button").trigger("pointerdown");
    await button(widget, "Close").trigger("click");
    await flushPromises();
    const asking = app.get('section[aria-label="Issues"] [role="alertdialog"]');
    expect(asking.text()).toContain("Close the issue?");
    expect(closed).toBe(false);
    await button(asking, "Confirm").trigger("click");
    await flushPromises();
    expect(Tools.CallForDashboard).toHaveBeenCalledWith(2, "lane", "close", '{"id":7}');
    expect(app.get('section[aria-label="Issues"]').text()).toContain("Done");
    vi.mocked(Tools.CallForDashboard).mockReset();
    app.unmount();
  });

  it("offers the sign-in a write needs above the dashboard, and says why another failed in its widget", async () => {
    tables.dashboards = [{ id: 2, name: "Lane", updated_at: "2026-10-12 10:00:00" }];
    vi.mocked(Documents.CompiledDashboard).mockResolvedValueOnce(
      compiled(
        `const lane = useService("lane")
        return () => bound([["lane", "post"], ["lane", "archive"]], "Lane", () =>
          h(harbor.Widget, { title: "Lane", action: "Post", onAction: () => lane.post({}) }, () =>
            h("button", { class: "archive", onClick: () => lane.archive({}) }, "Archive")))`,
        [
          ["lane", "post", { read: false }],
          ["lane", "archive", { read: false }],
        ],
      ),
    );
    vi.mocked(Tools.CallForDashboard).mockImplementation((async (_id: number, _service: string, tool: string) => {
      throw tool === "post" ? signInTo("lane", "Lane") : new Error("Lane said no");
    }) as never);
    open("dashboard", 2);
    const app = mountApp();
    await flushPromises();
    await button(app.get('section[aria-label="Lane"]'), "Post").trigger("click");
    await flushPromises();
    expect(app.get('[aria-label="Sign-ins needed"]').text()).toContain("Sign in to Lane");
    await app.get("button.archive").trigger("click");
    await flushPromises();
    expect(app.get('section[aria-label="Lane"] [role="alert"]').text()).toBe("Lane said no");
    vi.mocked(Tools.CallForDashboard).mockReset();
    app.unmount();
  });

  it("says what is wrong when it cannot be shown", async () => {
    tables.dashboards = [{ id: 2, name: "Inbox", updated_at: "2026-10-12 10:00:00" }];
    vi.mocked(Documents.CompiledDashboard).mockRejectedValueOnce(new Error("line 1: Element is missing end tag."));
    open("dashboard", 2);
    const app = mountApp();
    await flushPromises();
    expect(app.get('[role="alert"]').text()).toBe("line 1: Element is missing end tag.");
  });

  it("shows the problems that stop it from being saved", async () => {
    vi.mocked(Documents.SaveDashboard).mockRejectedValueOnce(new Error("line 1: Element is missing end tag."));
    open("dashboard", 2, true);
    const app = mountApp();
    await flushPromises();
    await app.get("main textarea").setValue("<template><p></template>");
    await app.get("main form").trigger("submit");
    await flushPromises();
    expect(Documents.SaveDashboard).toHaveBeenCalledWith(2, "Inbox", "<template><p></template>");
    expect(app.get('[role="alert"]').text()).toBe("line 1: Element is missing end tag.");
    expect(place.value).toMatchObject({ editing: true });
  });

  it("saves its name with its file", async () => {
    open("dashboard", 0);
    const app = mountApp();
    await flushPromises();
    await app.get('input[aria-label="Dashboard name"]').setValue("Mail");
    await app.get("main form").trigger("submit");
    await flushPromises();
    expect(Documents.SaveDashboard).toHaveBeenCalledWith(0, "Mail", "<template><p>new</p></template>");
    expect(place.value).toEqual({ kind: "dashboard", id: 8, editing: false });
  });
});

// A message as Harbor stores it.
let seq = 0;
const message = (role: string, ...parts: Record<string, unknown>[]) => ({ seq: ++seq, role, parts: JSON.stringify(parts) });

describe("a chat", () => {
  it("starts when you send the first message", async () => {
    const app = mountApp();
    await flushPromises();
    expect(app.find(".setup").exists()).toBe(false);
    const agent = app.get('aside[aria-label="Agent panel"]');
    // The window opened to Inbox, and the agent is told so.
    await agent.get('textarea[aria-label="Message"]').setValue("Make this weekly");
    await agent.get("form").trigger("submit");
    await flushPromises();
    expect(Chats.NewChat).toHaveBeenCalled();
    expect(Chats.Send).toHaveBeenCalledWith("chat-1", "Make this weekly", 'the dashboard "Inbox"');
    expect(chat.value).toBe("chat-1");
  });

  it("shows messages, steps, the reply so far, and asks before a change", async () => {
    tables.chats = [{ title: "Fix it", working: 0, draft: "", error: "" }];
    tables.messages = [
      message("user", { text: "Fix the dashboard" }),
      message("assistant", { toolCall: { id: "c1", name: "read_dashboard", input: { name: "Inbox" } } }),
      message("tool", { toolResult: { id: "c1", output: { source: "x" } } }),
      message("assistant", { text: "I will delete it." }, { toolCall: { id: "c2", name: "delete_dashboard", input: { name: "Inbox" } }, ask: true }),
    ];
    openChat("chat-1");
    const app = mountApp();
    await flushPromises();
    expect(app.get('[aria-label="You"]').text()).toBe("Fix the dashboard");
    expect(app.get('[aria-label="Agent"]').text()).toBe("I will delete it.");
    const steps = app.findAll(".step");
    expect(app.findAll(".steps-summary").map((s) => s.text())).toEqual(["Ran 1 step", "Needs your permission"]);
    expect(steps.map((s) => [s.get(".what").text(), s.find(".state").exists() ? s.get(".state").text() : ""])).toEqual([
      ["Read the dashboard Inbox", ""],
      ["Delete the dashboard Inbox", "needs your permission"],
    ]);
    expect(steps[1].text()).toContain("Let the agent delete the dashboard Inbox?");
    // The name is in the step's words, so only what it gave back shows.
    expect(steps[0].findAll(".part").map((p) => p.text())).toEqual(["sourcex"]);
    await button(steps[1], "Allow").trigger("click");
    expect(Chats.Answer).toHaveBeenCalledWith("chat-1", "c2", true);
  });

  it("shows why a turn stopped", async () => {
    tables.chats = [{ title: "Hi", working: 0, draft: "", error: "OpenAI said 401 Unauthorized: Incorrect API key provided." }];
    openChat("chat-1");
    const app = mountApp();
    await flushPromises();
    expect(app.get('[role="alert"]').text()).toBe("OpenAI said 401 Unauthorized: Incorrect API key provided.");
  });

  it("says when there is no provider, and goes to Providers in Settings", async () => {
    tables.settings = [];
    openChat();
    const app = mountApp();
    await flushPromises();
    expect(app.get(".setup").text()).toContain("Harbor's agent has no provider yet.");
    await app.get(".setup button").trigger("click");
    expect(settings.value).toBe("providers");
  });
});

describe("the side panels", () => {
  beforeEach(() => {
    window.innerWidth = 1600;
  });
  afterEach(() => {
    saveSettings([]);
  });

  it("collapse and open from the keyboard, and keep their width", async () => {
    saveSettings([{ key: "sidebar_panel", value: '{"width":300,"collapsed":false}' }]);
    const app = mountApp();
    const sidebar = app.get('[role="separator"][aria-label="Resize the sidebar"]');
    expect(sidebar.attributes("aria-valuenow")).toBe("300");
    await sidebar.trigger("keydown", { key: "ArrowRight" });
    expect(Preferences.SetPanel).toHaveBeenCalledWith("sidebar", 316, false);
    await app.get('[role="separator"][aria-label="Resize the agent"]').trigger("keydown", { key: "Enter" });
    expect(Preferences.SetPanel).toHaveBeenCalledWith("agent", 384, true);
  });

  it("collapse to a strip, with a button that opens them", async () => {
    saveSettings([
      { key: "sidebar_panel", value: '{"width":300,"collapsed":true}' },
      { key: "agent_panel", value: '{"width":500,"collapsed":true}' },
    ]);
    const app = mountApp();
    expect(app.get('aside[aria-label="Agent panel"] form').isVisible()).toBe(false);
    await app.get('[aria-label="Show the sidebar"]').trigger("click");
    expect(Preferences.SetPanel).toHaveBeenCalledWith("sidebar", 300, false);
    // Opening a chat opens the agent.
    openChat();
    expect(Preferences.SetPanel).toHaveBeenCalledWith("agent", 500, false);
  });
});

describe("providers", () => {
  const provider = (app: ReturnType<typeof mountApp>, title: string) =>
    app.findAll('section[aria-label="Providers"] li').find((li) => li.get(".title").text() === title)!;

  it("opens a provider's settings in place of the list, and saves them", async () => {
    tables.settings = [];
    tables.providerSettings = [];
    const app = mountApp();
    await button(app, "Settings").trigger("click");
    await flushPromises();
    expect(app.get('[role="dialog"] nav [aria-current="page"]').text()).toBe("Providers");
    expect(app.get(".status").text()).toBe("Harbor's agent has no provider yet.");
    await button(provider(app, "OpenAI"), "Set up").trigger("click");
    const form = app.get('form[aria-label="OpenAI"]');
    expect(app.find('section[aria-label="Providers"]').exists()).toBe(false);
    const [key, model] = form.findAll("input");
    expect(key.attributes("type")).toBe("password");
    expect(model.attributes("placeholder")).toBe("gpt-5");
    expect(form.get('button[type="submit"]').text()).toBe("Save and use");
    await key.setValue("sk-2");
    await form.trigger("submit");
    await flushPromises();
    expect(Providers.Save).toHaveBeenCalledWith("openai", { apiKey: "sk-2", model: "" });
    expect(app.find('form[aria-label="OpenAI"]').exists()).toBe(false);
    expect(provider(app, "OpenAI").exists()).toBe(true);
  });

  it("fills in what is saved, marks the one in use, and goes back", async () => {
    openSettings("providers");
    const app = mountApp();
    await flushPromises();
    expect(provider(app, "OpenAI").get(".in-use").text()).toBe("In use");
    expect(app.find(".status").exists()).toBe(false);
    await provider(app, "OpenAI").get('[aria-label="Edit OpenAI"]').trigger("click");
    const form = app.get('form[aria-label="OpenAI"]');
    expect((form.get("input").element as HTMLInputElement).value).toBe("sk-1");
    expect(form.get('button[type="submit"]').text()).toBe("Save");
    await app.get('[aria-label="Back"]').trigger("click");
    expect(app.find('form[aria-label="OpenAI"]').exists()).toBe(false);
  });
});

describe("the developer bar", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    devView.value = "harbor";
  });

  it("is not in a prod build, and nor is Tools", async () => {
    const app = mountApp();
    // Dev's components are loaded on their own, as a dev build loads them.
    await vi.dynamicImportSettled();
    await flushPromises();
    expect(app.find('nav[aria-label="Developer"]').exists()).toBe(false);
    expect(app.findAll("button").filter((b) => b.text() === "Tools")).toEqual([]);
  });

  it("opens Tools in dev, and goes back to Harbor as it was", async () => {
    vi.stubGlobal("DESKTOP_MODE", "dev");
    openSettings();
    const app = mountApp();
    // Dev's components are loaded on their own, as a dev build loads them.
    await vi.dynamicImportSettled();
    await flushPromises();
    const bar = app.get('nav[aria-label="Developer"]');
    expect(bar.findAll("button").map((b) => b.text())).toEqual(["Harbor", "Tools", "Benchmarks", "Components"]);
    await bar.findAll("button")[1].trigger("click");
    await vi.dynamicImportSettled();
    await flushPromises();
    expect(app.findAll("h1").filter((h) => h.isVisible()).map((h) => h.text())).toEqual(["Tools"]);
    expect(app.find('[role="dialog"]').exists()).toBe(false);
    expect(app.findAll("main li code").map((c) => c.text())).toEqual(["list_dashboards", "read_dashboard"]);
    await bar.findAll("button")[0].trigger("click");
    await flushPromises();
    // Tools is gone, and Harbor shows again. jsdom keeps the style it worked
    // out while Harbor was hidden, so this reads what v-show set.
    expect(app.get('[role="dialog"] section h2').text()).toBe("Providers");
    expect((app.get('[aria-label="Harbor"]').element.parentElement as HTMLElement).style.display).toBe("");
  });
});

describe("the agent's tools", () => {
  beforeEach(() => {
    vi.stubGlobal("DESKTOP_MODE", "dev");
    devView.value = "tools";
  });
  afterEach(() => {
    vi.unstubAllGlobals();
    devView.value = "harbor";
  });

  it("runs a tool, and says whether the agent would ask first", async () => {
    const app = mountApp();
    // Dev's components are loaded on their own, as a dev build loads them.
    await vi.dynamicImportSettled();
    await flushPromises();
    await app.get('[aria-label="Run read_dashboard"]').trigger("click");
    await flushPromises();
    const form = app.get('form[aria-label="Run read_dashboard"]');
    const input = form.get("textarea");
    expect(JSON.parse((input.element as HTMLTextAreaElement).value)).toEqual({ name: "" });
    expect(form.get(".asks").text()).toBe("The agent makes this call without asking.");
    await input.setValue('{"name":"Secret"}');
    await flushPromises();
    expect(form.get(".asks").text()).toBe("The agent asks you before this call.");
    await input.setValue('{"name":"Nope"}');
    await form.trigger("submit");
    await flushPromises();
    expect(AgentTools.Call).toHaveBeenCalledWith("read_dashboard", '{"name":"Nope"}');
    expect(form.get("pre").text()).toBe(JSON.stringify({ error: "there is no dashboard named Nope" }, null, 2));
    expect(form.get(".took").text()).toMatch(/^Took \d+ ms\.$/);
  });

  it("does not send input that is not JSON", async () => {
    const app = mountApp();
    // Dev's components are loaded on their own, as a dev build loads them.
    await vi.dynamicImportSettled();
    await flushPromises();
    await app.get('[aria-label="Run list_dashboards"]').trigger("click");
    const form = app.get('form[aria-label="Run list_dashboards"]');
    await form.get("textarea").setValue("{nope");
    await form.trigger("submit");
    expect(form.get('[role="alert"]').text()).toBe("The input is not JSON.");
    expect(form.find(".asks").exists()).toBe(false);
    expect(AgentTools.Call).not.toHaveBeenCalled();
  });
});

describe("settings", () => {
  const section = (app: ReturnType<typeof mountApp>, name: string) =>
    app.findAll('[role="dialog"] nav button').find((b) => b.text() === name)!;

  it("opens over the page from the sidebar, and closes", async () => {
    open("dashboard", 2);
    const app = mountApp();
    await button(app, "Settings").trigger("click");
    await flushPromises();
    expect(app.get('[role="dialog"]').text()).toContain("Settings");
    expect(app.findAll('[role="dialog"] nav button').map((b) => b.text())).toEqual([
      "Providers",
      "Appearance",
      "Logins & passkeys",
      "Connections",
    ]);
    await app.get('[role="dialog"] [aria-label="Close"]').trigger("click");
    await flushPromises();
    expect(settings.value).toBe(null);
    expect(place.value).toEqual({ kind: "dashboard", id: 2, editing: false });
  });

  it("says how to use Harbor from other agents, and replaces the token", async () => {
    openSettings("connections");
    const app = mountApp();
    await flushPromises();
    const pane = app.get('[aria-label="Connections"]');
    expect(pane.get(".address").text()).toBe("http://127.0.0.1:7331/mcp");
    // The token shows only when you ask.
    expect(pane.get(".command").text()).not.toContain("secret");
    await pane.get('[aria-label="Show"]').trigger("click");
    expect(pane.get(".token").text()).toBe("secret");
    expect(pane.get(".command").text()).toBe(
      'claude mcp add --transport http harbor http://127.0.0.1:7331/mcp --header "Authorization: Bearer secret"',
    );
    await pane.get('[aria-label="New token"]').trigger("click");
    expect(MCP.NewToken).not.toHaveBeenCalled();
    await button(pane, "Replace").trigger("click");
    await flushPromises();
    expect(pane.get(".token").text()).toBe("fresh");
  });

  it("chooses a theme and a mode", async () => {
    openSettings();
    const app = mountApp();
    await section(app, "Appearance").trigger("click");
    const appearance = app.get('[aria-label="Appearance"]');
    await button(appearance, "Nord").trigger("click");
    expect(Preferences.UseTheme).toHaveBeenCalledWith("Nord");
    await button(appearance, "Dark").trigger("click");
    expect(Preferences.SetMode).toHaveBeenCalledWith("dark");
  });

  it("adds a theme from the one in use, renames it, and imports CSS", async () => {
    openSettings();
    const app = mountApp();
    await section(app, "Appearance").trigger("click");
    const appearance = app.get('[aria-label="Appearance"]');

    // Add a theme: the editor opens with a copy of the theme in use, and no name.
    await appearance.get('[aria-label="Add a theme"]').trigger("click");
    let editor = app.get('form[aria-label="Add a theme"]');
    expect(app.find('[aria-label="Theme colours"]').exists()).toBe(false);
    expect((editor.get('input[aria-label="Theme name"]').element as HTMLInputElement).value).toBe("");
    expect((editor.get('input[aria-label="primary (Light)"]').element as HTMLInputElement).value).toBe("#18181b");
    // A derived variable that is not set shows the colour it will be given,
    // even when it is calculated from another one that is also not set.
    const input = editor.get('input[aria-label="input (Light)"]');
    expect((input.element as HTMLInputElement).value).toBe("");
    expect(input.attributes("placeholder")).toBe("#e4e4e7");
    const popover = editor.get('input[aria-label="popover (Light)"]');
    expect((popover.element as HTMLInputElement).value).toBe("");
    expect(popover.attributes("placeholder")).toBe("#ffffff");

    // The window wears what you type, in the mode the window is in.
    await editor.get('input[aria-label="primary (Light)"]').setValue("#123456");
    await flushPromises();
    expect(document.documentElement.style.getPropertyValue("--primary")).toBe("#123456");

    await editor.get('input[aria-label="Theme name"]').setValue("Mine");
    await editor.trigger("submit");
    await flushPromises();
    expect(Preferences.SaveTheme).toHaveBeenCalledWith("", "Mine", expect.any(String));
    const saved = JSON.parse(vi.mocked(Preferences.SaveTheme).mock.calls[0][2]) as {
      light: Record<string, string>;
      dark: Record<string, string>;
    };
    expect(saved.light.primary).toBe("#123456");
    expect(saved.dark.primary).toBe("#fafafa");
    expect(saved.light.input).toBeUndefined(); // what you did not set is not sent
    // The editor closed, and its theme is the one in use.
    expect(app.find('[aria-label="Theme colours"]').exists()).toBe(true);
    expect(app.get('[aria-label="Theme colours"]').text()).toContain("Mine");
    expect(document.documentElement.style.getPropertyValue("--primary")).toBe("#123456");

    // Edit: rename it.
    await app.get('[aria-label="Theme colours"]').get('[aria-label="Edit"]').trigger("click");
    editor = app.get('form[aria-label="Edit a theme"]');
    expect((editor.get('input[aria-label="Theme name"]').element as HTMLInputElement).value).toBe("Mine");
    await editor.get('input[aria-label="Theme name"]').setValue("Ours");
    await editor.trigger("submit");
    await flushPromises();
    expect(Preferences.SaveTheme).toHaveBeenLastCalledWith("Mine", "Ours", expect.any(String));
    expect(tables.themes.map((t) => t.name)).toEqual(["Ours"]);

    // Import CSS sets the fields it has and clears the others.
    await app.get('[aria-label="Theme colours"]').get('[aria-label="Edit"]').trigger("click");
    editor = app.get('form[aria-label="Edit a theme"]');
    await button(editor, "Import CSS").trigger("click");
    const area = document.querySelector('textarea[aria-label="Theme CSS"]') as HTMLTextAreaElement;
    area.value = ":root { --primary: red; --radius: 0.5rem; } .dark { --primary: pink; }";
    area.dispatchEvent(new Event("input"));
    await flushPromises();
    [...document.querySelectorAll("button")].find((b) => b.textContent?.trim() === "Import")!.click();
    await flushPromises();
    expect((editor.get('input[aria-label="primary (Light)"]').element as HTMLInputElement).value).toBe("red");
    expect((editor.get('input[aria-label="primary (Dark)"]').element as HTMLInputElement).value).toBe("pink");
    expect((editor.get('input[aria-label="background (Light)"]').element as HTMLInputElement).value).toBe("");

    // A required variable that is empty stops the save.
    await editor.trigger("submit");
    expect(editor.get('[role="alert"]').text()).toBe("Set background in Light.");
    expect(Preferences.SaveTheme).toHaveBeenCalledTimes(2);
    await button(editor, "Cancel").trigger("click");
    expect(app.find('[aria-label="Theme colours"]').exists()).toBe(true);
    // The window stops wearing the draft.
    expect(document.documentElement.style.getPropertyValue("--primary")).toBe("#123456");
  });

  it("puts the window in the theme and mode you chose", async () => {
    tables.settings = [
      { key: "theme", value: "Mine" },
      { key: "mode", value: "dark" },
    ];
    tables.themes = [{ name: "Mine", tokens: '{"light":{"primary":"red"},"dark":{"primary":"pink","border":"grey"}}' }];
    mountApp();
    const root = document.documentElement;
    expect(root.classList.contains("dark")).toBe(true);
    expect(root.style.getPropertyValue("--primary")).toBe("pink");
    // What a theme leaves out comes from what it has.
    expect(root.style.getPropertyValue("--input")).toBe("grey");
  });

  it("lists, adds, edits, and deletes saved logins", async () => {
    tables.logins = [{ origin: "https://tidewater.localhost:8443", username: "alex", password: "pw", totp_secret: "ABC" }];
    openSettings("logins");
    const app = mountApp();
    const saved = () => app.get('[aria-label="Saved logins"]');
    expect(saved().get(".title").text()).toBe("tidewater.localhost:8443");
    expect(saved().get(".detail").text()).toBe("alex");
    expect(saved().find('[aria-label="With authenticator"]').exists()).toBe(true);
    // Only a long list has a filter.
    expect(app.find('[aria-label="Filter logins"]').exists()).toBe(false);

    await saved().get('[aria-label="Edit the login for https://tidewater.localhost:8443"]').trigger("click");
    let form = app.get('form[aria-label="Save a login"]');
    expect(form.get("input").attributes("disabled")).toBeDefined();
    await form.findAll("input")[2].setValue("new");
    await form.trigger("submit");
    await flushPromises();
    expect(Tools.SaveLogin).toHaveBeenCalledWith("https://tidewater.localhost:8443", {
      username: "alex",
      password: "new",
      totpSecret: "ABC",
    });

    await app.get('[aria-label="Add a login"]').trigger("click");
    form = app.get('form[aria-label="Save a login"]');
    expect((form.get("input").element as HTMLInputElement).value).toBe("");
    await app.get('[aria-label="Back"]').trigger("click");

    await saved().get('[aria-label="Delete the login for https://tidewater.localhost:8443"]').trigger("click");
    expect(Tools.DeleteLogin).not.toHaveBeenCalled();
    await button(saved(), "Yes, delete").trigger("click");
    expect(Tools.DeleteLogin).toHaveBeenCalledWith("https://tidewater.localhost:8443");
  });

  it("filters a long list of logins", async () => {
    tables.logins = Array.from({ length: 9 }, (_, i) => ({
      origin: `https://site${i}.localhost`,
      username: "alex",
      password: "pw",
      totp_secret: "",
    }));
    openSettings("logins");
    const app = mountApp();
    await app.get('[aria-label="Filter logins"]').setValue("site3");
    expect(app.findAll('[aria-label="Saved logins"] .title').map((t) => t.text())).toEqual(["site3.localhost"]);
  });

  it("lets Harbor use a passkey without asking, and deletes one", async () => {
    tables.passkeys = [{ id: "k1", rp_id: "shoebox.localhost", user_name: "alex", auto: 0 }];
    openSettings("logins");
    const app = mountApp();
    const section = app.get('[aria-label="Passkeys"]');
    await section.get('input[type="checkbox"]').setValue(true);
    expect(Tools.UsePasskeyWithoutAsking).toHaveBeenCalledWith("k1", true);
    await section.get('[aria-label="Delete the passkey for alex on shoebox.localhost"]').trigger("click");
    await button(section, "Yes, delete").trigger("click");
    expect(Tools.DeletePasskey).toHaveBeenCalledWith("k1");
  });
});
