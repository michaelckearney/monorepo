# Services and dashboards

The first piece of the rebuild: storing services and dashboards, and the UI
to list, read, make, edit, and delete them.

## Services

A service connects Harbor to one website. It is one TypeScript file, and it
is one MCP server: its tools are MCP tools.

```ts
import { defineService } from "harbor"

// Helpers are ordinary functions, shared by every tool.
async function page(path: string) {
  const html = await (await fetch(path)).text()
  return new DOMParser().parseFromString(html, "text/html")
}

export default defineService({
  name: "gitea",                       // lower-case letters, digits, hyphens
  title: "Gitea",
  description: "Read issues from Gitea.", // optional
  url: "https://gitea.example.com",

  login: {                             // optional
    url: "/user/login",                // a path on url
    check: async ({ cookies }) => cookies.some((c) => c.name === "i_like_gitea"),
    run: async () => { /* optional: sign in with fill(field, what) */ },
  },

  tools: {
    list_issues: {                     // lower-case letters, digits, underscores
      title: "List issues",            // optional
      description: "List a repository's issues, newest first.",
      annotations: { readOnlyHint: true },               // optional; MCP's hints
      inputSchema: { type: "object", properties: {} },   // optional
      outputSchema: { type: "object", properties: {} },  // optional
      login: { url, check, run },       // optional: to confirm it's you first
      frame: { site, page },            // optional: run in a frame from another site
      async run(input, { cookies }) {   // the context object is optional to use
        return { issues: [] }
      },
    },
  },
})
```

Rules:

- A tool's fields are MCP's tool definition, plus `run`.
- `run` runs inside the site's page, in Harbor's browser, signed in as you.
  It returns an object; throwing an error gives an MCP `isError` result.
- `inputSchema` and `outputSchema` are JSON Schema with `type: "object"`.
- A tool without `readOnlyHint` changes data, and is destructive unless
  `destructiveHint` is false. That is MCP's rule.
- `login.check` returns true or false. If it cannot tell yet, it throws, and
  Harbor checks again.
- `login.check` and `run` get a context object last, with the site's
  `cookies`. See `browser-engine.md`.
- `login.run`, a tool's `login`, `frame`, and files: see
  `browser-engine.md`.
- The only imports are `defineService` and `fill` from `"harbor"`.
- Code outside a function must not use the page: Harbor runs the file to read
  it, with no page and no network.
- Unknown fields are an error.

### Saving a service

Harbor reads the file before it saves it:

1. esbuild removes the types and bundles it. Any import other than
   `"harbor"` is an error.
2. goja runs it, with a one-second limit.
3. Harbor checks the exported object against the rules above and makes the
   **manifest**: everything except the code, as JSON.

A file with problems is not saved. The editor shows every problem, each with
the field it is about.

## Dashboards

A dashboard is one Vue file. It can import only from `"vue"` and `"harbor"`.
Its name is set in the UI, not in the file.

`"harbor"` is a kit (`frontend/src/kit`, made by `harbor()` in
`dashboards.ts`), so that a dashboard is short, and looks like Harbor and
like every other dashboard, in whatever theme. It is for dashboards only:
the app's own UI is built by hand from the framework's shared components
(`framework/ui`), and the two share only the tokens in its `style.css`.

The script reads the services' tools and shapes what they return into what
each widget takes, with Vue's own `ref` and `computed`; the template lays
out widgets on the `Grid`. Everything on a dashboard is a widget, and every
control is in the widget it changes: a dashboard has no controls of its own.

- **`useService("tasklane")`** reaches a service, and its tools are its
  methods: `tasklane.list_issues({ assignee: "me" })`. The service's name
  must be a string in quotes, and a tool is only ever used as
  `service.tool`, so the compiler (`backend/dashboard`) reads from the file
  which tools the dashboard uses, before it runs; saving checks each
  against its service's manifest, and notes the services
  (`dashboard_services`). `format` and `fileURL` are the rest of what
  `"harbor"` has besides its components.
- **A read** (a tool its manifest marks `readOnlyHint`) returns its output
  at once: what Harbor has, or, until it has loaded, or once it failed or
  needs a sign-in, a stand-in that throws that when anything in it is used.
  Reads go in a `computed` or the template, so Vue tracks them: one runs
  again when a ref its input reads changes, when the page refreshes, and
  every `every` seconds if it asks to. Reads alike (the same service, tool,
  and input) are one request however many widgets ask, and stay for the
  page's life. A read not wanted yet, such as a search before anything is
  typed, is simply not called. (`kit/runtime.ts`)
- The dashboard's `computed` is Vue's with two differences. One whose
  getter threw throws again each time it is read, until what it read
  changes: Vue keeps the value it had, so a second widget would read that.
  And once it has had a value, it keeps it while a read it makes is
  loading, so a widget whose filter changed shows what it had, and its
  controls, with a spinner, until the new data is there.
- **A write** (any other tool) returns a promise. It runs only from
  something the user does in a widget: its events, or a `v-model` whose
  `computed` has a `set`. A destructive tool waits for the user to confirm
  it in that widget. Once it has run, every read runs again: what a write
  changes is not known.
- The compiler puts each widget in the template inside a **Bound**, with
  the tools that widget's props and events reach. The widget is the Bound's
  slot, so its props are worked out as the Bound renders, and what a read
  throws there stays in that widget: the Bound shows the widget's frame
  loading, failed, or asking for a sign-in, and tries again once the read
  settles or the page refreshes. It also tells the frame which services
  the widget's data is from, when one of its reads is loading again or a
  write is running, and why one failed. An error in a widget stays in it.
- **Widgets** (`kit/widgets/`) are the catalog: `InboxWidget` for
  messages, `AgendaWidget` for things at a time, `TasksWidget`,
  `BoardWidget`, `LedgerWidget`, and so on. Each only shows: it takes its
  data as typed props, in the shapes `shapes.ts` gives, and says what the
  user does with events: `@move` on a board, `@act` on tasks, `@open` on a
  list. Its other props set how it looks (`done`, `limit`, `format`). Each
  is the **`Widget`** frame with its content inside, and the frame's props
  fall through to it: the title, the size, and what the user can do in a
  row under the title, each reporting only what was chosen (`search` with
  `v-model:query`, `filters` with `v-model:filter`, `views` with
  `v-model:view`, `toggle` with `v-model:toggled`, `action` with
  `@action`). What a choice does is the dashboard's: a read whose input
  reads it, or a `computed` that filters by it.
- **Primitives** (`kit/primitives/`) have no data and no state: text,
  values, times, badges, a chart, the `Item` row every list uses, so a
  widget of your own can be the `Widget` frame with them inside.
  **Controls** (`kit/controls/`) are what the frame draws.
- **Layout** (`kit/layout/`) is the `Grid`.

The kit is the quick way, not the only one. A dashboard is Vue, and can
build anything the kit does not have, with its own markup and `<style
scoped>`, which can use the theme's variables. The kit's components are
styled with Tailwind when the app is built; a dashboard's own Tailwind
classes do nothing, since it is compiled when it is shown, after Tailwind
has run. `backend/mcp.md` is how the agent is told to write one.

### The kit's files are its catalog

Nothing lists the kit by hand. A component's name is its file's name, and
its kind is its folder. `harbor()` exports every `.vue` file in the kit's
folders, found with `import.meta.glob`, so adding a file adds it to
`"harbor"`.

Each file starts with a comment that describes it: a `description`,
`keywords` (the words someone would use when they want it: a search that
misses is fixed by adding one), and one or more `example`s, each a template
that uses it. Its props are its own `defineProps`, with their comments.

The backend embeds the kit's files (`frontend/src/kit/kit.go`) and reads
them when it starts:

- `search_kit` scores each component by the query's words: a match in its
  name counts most, then its keywords, then its description, and widgets
  come first. It searches what the files say of themselves, not their
  code. `read_kit` gives a component's description, props, and examples.
- Saving a dashboard that imports something `"harbor"` does not have fails,
  with the names closest to it.
- The Kit page, in dev mode, shows every component and its examples,
  compiled as dashboards are.

Tests check that every component describes itself and that every example
compiles (`kit_test.go`) and mounts (`kitexamples.test.ts`), and that real
queries find the right widget first.

A dashboard's calls never wait for you to sign in. If you are signed out,
and Harbor cannot sign in on its own, a call fails at once. A piece shows
that in its card; a `call` outside the kit throws, and the page above the
dashboard offers the sign-in. Once you sign in, the dashboard loads again.

When a dashboard runs, Harbor notes which services it called
(`dashboard_services`), and the sidebar marks a dashboard whose service
needs you. Saving a dashboard forgets them, until it runs again.

When you save a dashboard, Harbor compiles it, and a file that does not
compile, or imports anything else, is not saved. Showing a dashboard compiles
it again, so what runs always matches the app's Vue; it takes about 30 ms.

Vue's compiler is JavaScript, so it runs in Go, in goja (`backend/dashboard`):

1. Vue's compiler makes the script, the template, and the styles into one
   TypeScript module, and the scoped CSS.
2. esbuild removes the types, and makes the module CommonJS: it gets `vue`
   and `harbor` with `require`.
3. The frontend runs the module and shows its component, with the CSS.

The compiler is `backend/dashboard/compiler/compiler.js`: a bundle of
`compiler/entry.js` and the frontend's `@vue/compiler-sfc`, made by a test.
When the frontend's Vue changes, that test fails until you make the bundle
again: `HARBOR_UPDATE_COMPILER=1 go test -run TestCompiler ./backend/dashboard`.

An error while a dashboard runs is shown in place of the dashboard.

## Database

```sql
CREATE TABLE services (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,  -- from the manifest
    source     TEXT NOT NULL,         -- exactly as written
    manifest   TEXT,                  -- JSON, made on save, or at start
    manifest_error TEXT,              -- why the last read at start failed
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE dashboards (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,  -- set in the UI
    source     TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

Everything that lists or describes a service reads the manifest. Nothing
runs the file again. There is no version history.

The manifest is Harbor's copy of what the file says, so nothing else writes
it. When Harbor starts, it reads the file of every service with no manifest:
one a migration inserted, such as the dev fixtures, or one whose manifest a
migration cleared because Harbor reads files differently now. A file that
will not read keeps no manifest, and `manifest_error` says why. The service
is listed with the error and has no tools until its file is fixed and saved.

## UI

The window is three columns: the sidebar, the page, and the agent's panel.
In dev mode, a developer bar runs across the top: Harbor, then Tools (the
agent's tools, for working on Harbor). Tools takes the whole window below
the bar, and Harbor goes back to the three columns as you left them. A prod
build has no bar, and leaves the code for both out.

- The sidebar lists the dashboards, then the chats; each list has a +
  button for a new one. Below them are Services and Settings.
- The window opens to the dashboard you last opened (`last_dashboard` in
  settings), or your first. With none, it offers to make one.
- Every page's top bar has a pill with the number of services, and how many
  need you to sign in, as Harbor last found (`sign_ins_needed`, written by
  every call, check, and sign-in). It opens a list of the services, with a
  button for each sign-in; All services is the Services page, with the
  button for a new service.
- The agent's panel shows one chat, which a chat in the sidebar opens. Each
  message tells the agent what you are looking at: the dashboard or service
  on the page.
- The sidebar and the agent's panel are resized by dragging their inner
  edge, and collapse to a strip, with the panel's icon and a button that
  opens it, when dragged to half their minimum. Double-clicking the edge
  goes back to the default width; ⌘B and ⌘J collapse and open them
  (`sidebar_panel` and `agent_panel` in settings, each a width and whether
  it is collapsed).
- Settings has the theme and the mode: light, dark, or the system's. A theme
  is shadcn's CSS variables, in light and dark (`framework/ui/themes.ts`).
  Five are built in; you add one by pasting its CSS, as tweakcn.com makes
  it (`themes`). What a theme leaves out comes from what it has, such as the
  sidebar's colours from the page's.
- The pages are built from shadcn-vue's components, in the framework's
  shared frontend, `framework/ui/components`, which Slipway uses too.
- Each service shows its site's favicon, or a plug until it has one. See
  Icons, below.
- A service's page shows its title, name, URL, description, where to sign
  in, and its tools. Each tool shows whether it is read-only, changes data,
  or is destructive.
- Edit opens one code editor with the whole file. Save (or ⌘S) saves it;
  Cancel goes back.
- A new service or dashboard starts from a starter file.
- Delete asks again before it deletes.
- A link leads out of Harbor, so it opens in your browser, never in the
  window: a link on a page, in a dashboard, or in the agent's reply, and a
  dashboard's `window.open` (`frontend/src/links.ts`). What acts in the app,
  such as signing in, is a button, not a link.

## Icons

`icons.go` finds the favicon of each origin the services' URLs are on, and
keeps it in `service_icons`; the frontend reads the table (`icons.ts`).

- Finding one (`backend/favicon`) is plain HTTP, not Harbor's Chromium: a
  GET of the origin, following redirects, then the `<link>` tags in its
  `<head>`. Which count and which wins follow Chromium's desktop favicon
  code: `icon`, not `mask-icon`; the size nearest 16 and 32 px; the last
  of a tie; `apple-touch-icon` only when there is no `icon`; and
  `/favicon.ico` last. A `media` of `prefers-color-scheme: dark` gives a
  second icon, shown when Harbor is dark. Bytes count only if they are an
  image, so a sign-in page served as `/favicon.ico` is not one.
- Like Chromium, it sends `localhost` and `*.localhost` to 127.0.0.1. It
  trusts the certificates the system trusts, so the fixtures' icons need
  their CA in the system's trusted certificates.
- A sweep runs 3 seconds after Harbor starts, every 5 minutes, and after a
  service is saved. An icon is refreshed after 14 days, with a conditional
  request. A site that does not answer is tried again after 30 minutes,
  doubling to a day, and keeps any icon it had. A site that answers with no
  icon is asked again after 14 days. An origin no service uses is forgotten
  after 30 days.

## Not in this piece

Type-checking. Running tools and the browser are in `browser-engine.md`;
the MCP server and the agent are in `agent.md`.
