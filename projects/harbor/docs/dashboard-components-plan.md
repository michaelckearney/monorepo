# Plan: dashboard components

The dashboard kit grows from seven pieces into a set of widgets and
primitives that the agent finds by searching, and the six seeded
dashboards move onto it. This is the plan; `services-and-dashboards.md`
says how dashboards work today.

## The rule

**Anything that shows a service's data is a widget.** A widget owns that
data's states (loading, needs sign-in, failed, empty, stale), so one
signed-out service blanks only the widgets that need it, never the
dashboard. A widget shows nothing of its data until every service it is
from is signed in to: some of it would say something untrue, such as
"Nothing coming up" when only one calendar loaded. Everything else follows from
this:

- **`Widget`**: the frame, the only component that knows a Result's state.
- **Widgets**: the frame, one content block, and a data shape, for one kind
  of information (`InboxWidget` for messages, not `GmailWidget`). The
  agent's first choice.
- **Primitives**: no data, no state. They go *inside* a `Widget` when no
  widget fits. Every widget's content block is one of them (`InboxWidget`
  is `Widget` + `InboxList`), so a custom widget can reuse a widget's
  content.
- **Layout and controls**: belong to the dashboard, not to a widget:
  `Grid`, `Section`, `Toolbar`, and the controls in it.

**The kit is the fast path, not a fence.** A dashboard is a Vue file and
stays one: it can build anything the kit doesn't cover, with its own
markup and `<style scoped>`. The agent is told to use the theme's tokens
there (`var(--text-body)`, `var(--radius)`, the colours), so what it builds
still looks like Harbor, but nothing enforces it.

## The kit and the app

The kit is for dashboards only. The app's own UI is built by hand, with
`components/ui/`, and changes to it never reach a dashboard:

- **They share the tokens**: `style.css`'s colours, type scale, radii,
  and control heights, which `tokens.test.ts` checks in both.
- **The kit imports nothing from `components/ui/`.** Today `Card` uses the
  app's `Button` and `Skeleton`; the kit gets its own.

## The frame: `Widget`

A `Card` with a header (the title, where the data is from) and the states of
its results. What today's `Card` does with `from`, plus:

| | |
|---|---|
| `from` (one or many results) | as now: loading, sign-in, error in place of the content |
| **all or nothing** | given several results, any that needs a sign-in shows "Sign in to Clearwater Health to see this" in place of the content, naming every service still needed. A result that failed for another reason is a line under what did load. Replaces This week's separate "Not shown" cards |
| **refreshing** | while it loads again, it keeps showing its data, as now, and adds a small spinner by the source. Today nothing shows that a filter change is loading, which is why the Issue board writes its own "Loading…" |
| **stale** | "Updated 2h ago" by the source when the data is older than its `every`, or after a failed refresh |
| **empty** | `empty="No new messages."`; a content block reports that it has nothing, and the frame shows the line |
| `count` | a number beside the title ("In review 3"), instead of putting it in the title string |
| `#actions`, `#footer` slots | buttons by the title; a footer such as "Open Clearwater" or a total |

## Primitives

All take the theme's tokens and nothing else; none has props for colour or
size beyond the tone and size scales.

**Surfaces and layout**: `Card` (a bordered surface with an optional
header: no data, no state), `Stack` (`gap`: `tight`, `normal`, `loose`),
`Row` (`justify`, `align`, `wrap`), `Divider`.

**Text and values**:

| | |
|---|---|
| `Text` | `size` from the type scale up to `title`, `tone`, `weight`, `lines` (clamp) |
| `Value` | a formatted value: `format` as today (`money`, `percent`, ...); `signed` colours money in and out |
| `Time` | a date or datetime: `relative` ("in 2 days", "3h ago"), `day` ("Today", "Tomorrow", "Thu, Nov 5"), or `date`/`datetime`; a range when given `end` |
| `Badge` | as now: `tone` |
| `Status` | a dot and a word: `tone` |
| `Avatar` | initials from a name, or an image; a stable colour from the chart palette |
| `Image` | `src`, `alt`, `aspect`, rounded, lazy |
| `Meter` | 0 to 100, or `value` of `max`; turns warning, then bad, at thresholds |
| `Sparkline`, `Trend` | a series as a small line; a change as "▲ 4%" with a tone |
| `Button`, `Skeleton` | the kit's own |

**Content blocks**:

| | |
|---|---|
| `Item` | the one row every list-like widget uses: `leading` (avatar, icon, image, or time), `label`, `detail`, `trailing` (value, badge, or time), `tone`, `link`, `unread`. One row anatomy everywhere is most of the consistency |
| `ItemList` | items as `Item`s, with `group` headings |
| `KeyValue` | label and value pairs, one a line |
| `InboxList`, `AgendaList`, ... | each widget's content, exported (below) |

**Tone** is one vocabulary everywhere: `neutral`, `good`, `warning`, `bad`,
`info`. Any piece that shows a status takes `tone` as a value, a field, a
function, or a map: `:tone="{ missing: 'bad', late: 'warning' }"`.

## Layout and controls

| | |
|---|---|
| `Grid` | as now; `span` on a widget |
| `Section` | a heading over a group of widgets, a full row of the grid |
| `Toolbar` | a row above the grid for the dashboard's controls |
| `Select`, `Toggle`, `Segmented`, `Search` | the kit's controls, with `v-model`, for what the dashboard's calls read. The Issue board's hand-styled `<select>` becomes `<Select>` |

## Widgets

Every widget takes the same props in the same way, as `List` does now:
`items` (a result or a list), `field` (where the list is in the result), and
**field props**, each a field name (with `:format`), or a function of the
item. Plus `title`, `limit`, `empty`, `span`, and `from`. Below, each
widget's field props are its data shape.

| Widget | For | Field props | It shows |
|---|---|---|---|
| `MetricWidget` | one number: a balance, a bill, a pay | `value`, `format`, `note`, `previous` (for a `Trend`), `series` (for a `Sparkline`) | the figure, its change, a note |
| `StatsWidget` | a few numbers that belong together | `items`, `label`, `value`, `format` | the figures side by side |
| `InboxWidget` | email, portal messages, mentions, school messages | `sender`, `subject`, `preview`, `time`, `unread`, `link`, `avatar` | sender with avatar, subject, a line of preview, relative time; unread in bold with a dot; the unread count by the title |
| `AgendaWidget` | appointments, events, deadlines, time off: things at a time | `start`, `end`, `label`, `detail`, `who`, `where`, `tone` | grouped by day (Today, Tomorrow, then dates), the time in a column on the left, ranges as ranges |
| `TasksWidget` | assignments, to-dos, issues | `label`, `keyField`, `status`, `due`, `who`, `priority`, `link`, `tone` | status badge, due relative and turning warning or bad when close or late, key in mono |
| `BoardWidget` | the same things in columns by state | the `TasksWidget` props, plus `column` (a field) and `columns` (`[{ value, title }]`) | one widget, a column per state with its count |
| `LedgerWidget` | transactions, charges, bills, payslips | `date`, `description`, `amount`, `account`, `category`, `total` | amounts signed and coloured, category as a badge, an optional total in the footer |
| `StatusWidget` | many things, each with a state: orders, devices, sensors, results | `label`, `detail`, `status`, `tone`, `value`, `link` | a status dot and word per item, the value on the right |
| `MetersWidget` | how full things are: disks, quotas, budgets | `label`, `value` + `max`, or `percent`; `detail`, `unit` | a meter per item, warning and bad near full |
| `GalleryWidget` | photos, documents with previews | `src`, `alt`, `caption`, `link`, `columns` | a grid of square images |
| `DetailsWidget` | one thing's facts: an account, a router, a bill | `items` as pairs, or `value` and `fields` | a `KeyValue` |
| `ListWidget` | anything listy with no better widget | today's `List`: `label`, `detail`, `value`, `group`, `meter`, `link` | `ItemList` |
| `TableWidget` | rows with several columns | today's `Table` | |
| `ChartWidget` | values over time or by category | today's `Chart` | |

Today's names change: `Card` with `from` becomes `Widget` (and `Card` is
the plain surface), and `Stat`, `List`, `Table`, and `Chart` become
`MetricWidget`, `ListWidget`, `TableWidget`, and `ChartWidget`, so every
widget's name ends in `Widget`. Later, as dashboards ask for them:
`FeedWidget` (activity over time), `ProgressWidget` (goals), `MapWidget`.

Widgets stay read-only. A link opens the site. Actions on items (mark read,
pay, reply) change things on a site, so they come later as one feature, with
the agent's confirmation, not widget by widget.

## Finding them

### The files are the catalog

Nothing lists the kit by hand, and nothing is generated:

```
frontend/src/kit/
  widgets/     Widget.vue, InboxWidget.vue, AgendaWidget.vue, ...
  primitives/  Card, Text, Value, Time, Badge, Item, InboxList, AgendaList, ...
  layout/      Grid, Section, Toolbar
  controls/    Select, Toggle, Segmented, Search
  data.ts, tone.ts
```

- A component's **name** is its file's name; its **kind** (widget,
  primitive, layout, control) is its folder.
- The `"harbor"` module exports every `.vue` file under `kit/`, found with
  `import.meta.glob`, so adding a file adds the export. `kit/index.ts` and
  `harbor()`'s list of pieces go.
- The backend embeds `frontend/src/kit` (`go:embed`) and reads it at
  startup, for search. Search is in the backend because the MCP server
  also serves other agents, with or without Harbor's window.

### Each component describes itself

In its own file, in a comment at the top, beside its code:

```vue
<!--
description: Messages with a sender, subject, preview, and time; unread ones stand out.
keywords: inbox, email, mail, messages, portal, care team, doctor, mentions, notifications, dms, school messages
example:
  <InboxWidget title="Messages" :items="messages" sender="from" subject="subject" time="sent" />
-->
```

- `description`: one sentence, what it shows.
- `keywords`: the words someone would use when they want it, not
  technical tags. They are where synonyms live: a search that misses is
  fixed by adding a keyword.
- `example`: one or more, each a template using the component, with the
  data it needs. What the agent copies, and what the tests mount.
- Props are the component's own `defineProps`, with their comments: the
  types are the documentation, so they can't drift from it.
- It is a comment, not a custom block such as `<kit>`, because slipway
  generates the Vite config, which takes no plugin to drop custom blocks
  from the build. Vue ignores a comment outside the template; the backend
  and the tests read it.

### Searching

Search reads the metadata, not the code: code is full of words like
`computed` and `class`, and the agent searches by what it wants to show
("messages from the doctor's office").

- **`search_kit(query)`** scores each component by the query's words: a
  match in the name counts most, then keywords, then the description. It
  returns the best few, each with its name, kind, and description. For a
  few hundred components, simple scoring is enough; embeddings can come if
  searches start to miss.
- **`read_kit(name)`** returns the description, the `defineProps` block
  with its comments, and the examples.
- `mcp.md`'s dashboard section shrinks to: pick a widget for each thing,
  or build one from `Widget` and the primitives, or write your own Vue;
  search and read before you use.
- Saving a dashboard fails when it imports a name `"harbor"` doesn't have,
  with the closest name ("no `Inbox`; did you mean `InboxWidget`?").
- A **Kit page** (dev mode) shows every example, in the chosen theme,
  light and dark: the place to review the kit.

### Tests

- Every `.vue` under `kit/` has its comment, with a description, keywords,
  and at least one example; every example compiles as a dashboard
  (`kit_test.go`) and mounts without a warning (`kitexamples.test.ts`).
- A Go test runs real queries and checks what comes first: "my emails" →
  `InboxWidget`, "upcoming appointments" → `AgendaWidget`, "disk space" →
  `MetersWidget`, and so on, growing with the kit.

## The seeded dashboards

Each moves onto the widgets. The kit should cover all of them, so their
CSS likely goes, but that is where it lands, not a rule. What each gains:

**Family**

- *Schoolwork* → `TasksWidget` grouped by student. "Missing" is a red
  badge instead of a "Missing:" prefix; due dates are relative ("due Thu"),
  turning warning the day before.
- *Coming up at school* and *Appointments* → one `AgendaWidget`, "Coming
  up", by day, with who it's for as an avatar. If Clearwater is signed out,
  the school events still show, with Clearwater's sign-in on a line in the
  same widget.
- *Test results* → `StatusWidget`: the status as a badge, "2 out of range"
  in warning, instead of all of it in one detail string.
- **New**: *Messages from the care team* → `InboxWidget`, from
  Clearwater's `list_messages`, which the service has but no dashboard
  shows.
- *Photos* → `GalleryWidget`, titled with the album.

**Home**

- *Sensors* → `StatusWidget`, readings right-aligned with their units.
- *Disks* → `MetersWidget`: "412 GB free of 2 TB", warning near full.
- *Network* → `StatusWidget` of the clients (an online dot, the band),
  with the internet status and uptime as `KeyValue` above them, instead of
  packed into the title.
- *Electricity* → `ChartWidget`, full width.

**Issue board**

- The four separate cards → one `BoardWidget`, a column for each state with
  its count, priority as a badge, the key in mono.
- The filters → a `Toolbar` with `Select` (project) and `Toggle` (only
  mine), instead of a hand-styled `<select>`. Its "Loading…" text goes: the
  widget's own spinner shows that a filter change is loading.

**Money**

- *Accounts* → `ListWidget` of balances, the total in the footer.
- *Electricity bill* → `MetricWidget`, due date in the note, an "Autopay"
  badge.
- *Latest pay* → `MetricWidget` with a `Trend` against the payslip before.
- *Largest charges* → `LedgerWidget`: amounts coloured, category badges,
  the account as detail.

**This week**

- *The next seven days* → `AgendaWidget` given all five sources: Today and
  Tomorrow headings, times in a column, the source as a small caption. A
  signed-out source puts the sign-in in the widget, not a separate "Not
  shown" card, and the `loading ? 'Loading…'` workaround goes.
- *On the way* → `StatusWidget` of orders, the status as a tone.

**Today at work**

- *Your issues under way* → `TasksWidget`, the state as a badge.
- *Mentions* → `InboxWidget`: author's avatar, `#channel`, relative time,
  unread.
- *Time off left* → `StatsWidget`: each balance as a figure.
- *Time off coming up* → `AgendaWidget` of ranges, approved or pending as a
  tone.

**How they move.** `010_fixtures.sql` has run, so it and the asset files it
reads stay as they are. A new migration, `database/dev/013_kit_dashboards.sql`,
sets each seeded dashboard's `source` from new files in
`database/dev/assets/dashboards/kit/`, by name. A seeded dashboard edited
in the dev app is overwritten; other dashboards in the dev database that use
the old names (`Stat`, `List`, ...) break, and are fixed by hand or by the
agent.

## Order of work

1. **Frame and primitives.** The new folders and the glob-built `"harbor"`
   module; the kit's own `Button` and `Skeleton`, off `components/ui/`;
   `Widget` with partial, refreshing, stale, empty, count, and footer, on
   the plain `Card`; `tone.ts`; `Item`, `ItemList`, `KeyValue`, `Text`,
   `Value`, `Time`, `Status`, `Avatar`, `Image`, `Meter`, `Trend`,
   `Sparkline`; `Section`, `Toolbar`, and the controls. Each with its
   comment and examples from the start.
2. **Widgets.** The fourteen above, each with its content block, and the
   renames.
3. **Search.** The embedded kit,
   `search_kit` and `read_kit`, the import check on save, the query tests,
   `mcp.md` rewritten, the Kit page.
4. **Seeded dashboards.** The new files and `013_kit_dashboards.sql`.
   Screenshots of each dashboard before and after, signed in and with a
   service signed out (`harness record`); the dev e2e test for them still
   passes, and a new one checks This week's partial widget.

## Decided

- Family: *Coming up at school* and *Appointments* merge into one agenda.
- Family: it gets the care-team *Messages* widget.
