# The MCP server and the agent

Harbor's agent writes, tests, and uses services and dashboards. It does this
only through Harbor's own MCP server, so any other agent connected to that
server can do the same.

## The MCP server

One server (`backend/mcp.go`), with these tools:

| Tool | What it does | Asks first |
|---|---|---|
| `list_services`, `read_service` | the services, and a service's file | |
| `write_service` | saves a whole file; the name in it says which service | |
| `delete_service` | | yes |
| `call_service_tool` | runs a service's tool in your browser, signed in as you | unless the tool is read-only |
| `test_service_tool` | the same, in the agent's browser, to test a service; with no tool, only signs in | unless the tool is read-only |
| `list_dashboards`, `read_dashboard` | | |
| `write_dashboard` | saves a whole file under a name | |
| `delete_dashboard` | | yes |
| `read_file` | a file a tool returned: text as text, an image as an image | |
| `browser_*` | the agent's own browsing, after Playwright MCP (below) | |
| `browser_sign_in` | shows you the agent's browser at a site, to sign in and click Done; returns what the sign-in did | |
| `browser_clear_data` | removes what the agent's browser keeps for a site, or everything | |

"Asks first" is Harbor's agent asking you in the chat. Agents outside Harbor
decide that themselves, from the tools' MCP annotations. Saving a file does
not ask: you can change it back. Browsing does not ask, as in Playwright MCP,
though a click can change your account; the instructions tell the agent to
submit nothing but a sign-in while it explores.

A tool call signs in when it needs to: with your saved login if the service
has `login.run`, or by showing you the login page and waiting.

### Browsing

The `browser_*` tools have Playwright MCP's names and inputs, so a model
that knows those knows these: `browser_navigate`, `browser_navigate_back`,
`browser_snapshot`, `browser_take_screenshot`, `browser_click`,
`browser_type`, `browser_fill_form`, `browser_select_option`,
`browser_press_key`, `browser_wait_for`, `browser_handle_dialog`,
`browser_evaluate`, `browser_network_requests`, and
`browser_network_request`. They act in one tab of the agent's browser
(`backend/engine/browse.go`), never yours.

- An action returns the page's snapshot after it: the accessibility tree,
  one element a line, with the ref the next action names it by.
- An action, `browser_evaluate`, and `test_service_tool`'s sign-in also
  return `events`, what they did, in at most 20 lines
  (`backend/engine/events.go`): the pages they went to, redirects
  included; the site's requests, method, address, and status, without
  images, styles, scripts, fonts, or media; and the cookies and
  `localStorage` or `sessionStorage` keys added, changed, or removed, by
  name, never by value. Past 20 lines, requests are left out first. A
  benchmark trial spent 57 calls guessing where Tasklane kept its sign-in,
  which only the requests and storage of pages already left could show.
- `browser_network_requests` leaves out images, styles, scripts, fonts, and
  media unless asked. It lists the newest 100, each with the page that made
  it, the pages before the one open too. It is how the agent finds a site's
  own APIs.
- `browser_evaluate` gets the element, if a ref names one, and the context
  object a tool's `run` gets, `{ cookies }`. 30 s limit.
- Not built: frames in a snapshot, choosing a tab, drag, hover, uploads, and
  PDFs. Actions go to the newest tab the agent's tab opened.

Before it writes a service's login, the agent calls `browser_sign_in`: you
sign in to the site in its browser and click Done, and it gets what the
sign-in did, its pages, requests, and the cookies and storage keys it added,
which say how the site keeps its sign-in. Without it, the agent had to guess
a `login.check` before it had seen a sign-in, and only a right guess let
Harbor sign in: a wrong one was refused, or held the sign-in open, and one
model, left without a way in, typed a guessed password. In a benchmark, the
run signs in for you and clicks Done once the site is back with nothing left
to do (`backend/benchmark/signin.go`).

To build a service, the agent saves a first version with only its `url` and
`login`, then calls `test_service_tool` with no tool: Harbor signs in, or
shows you the login page, in the agent's browser. If the check is wrong,
the error says which way.

To try a tool by hand, run Harbor in dev mode (`harness run -mode=dev`) and
open Tools in the developer bar at the top of the window. It runs
the tool through this server, as Harbor's agent does, and shows the result
as the agent gets it: `{"output": ...}` or `{"error": ...}`. It also says
whether the agent would ask before that call. It does not ask you itself.

A write with problems is not saved, and the tool's error lists every problem,
so an agent can fix them all at once. A result says in words what happened,
since the model sees a tool's description and input schema but never its
output schema: a save returns `"saved": "service tasklane"`, whether it
`replaced` one, and the tools Harbor read from the file; a delete or a clear
returns a sentence; a missing tool's error lists the tools there are. A bare
`"created": false`, for a save that replaced a file, once read as a failure
43 times in one trial. The server's instructions
(`backend/mcp.md`) tell an agent how services and dashboards are written, and
how to work: ask which site, browse, find the site's APIs, try code with
`browser_evaluate`, save, then test.

The server is reached two ways:

- **Harbor's agent:** in memory.
- **Other agents:** streamable HTTP on `127.0.0.1`, with a token in the
  `Authorization: Bearer` header. The port and the token are kept in
  `mcp.json` in the data folder, so an agent set up once keeps working. If
  another program has the port, Harbor takes a new one. A new chat shows the
  `claude mcp add` command.

## The agent

The agent is Harbor's own loop (`backend/chat`). A turn repeats these steps:

1. Ask the model for the next message, with the instructions, the chat so
   far, and the MCP server's tools. The reply streams into `draft`.
2. Store the message. If it has tool calls, run each one through an
   in-memory MCP client, store its result, and go back to step 1.
3. Stop when the message has no tool calls.

- **Instructions:** a short introduction, then `backend/mcp.md`.
- **Permission:** a tool call that needs your permission is stored with
  `ask`, and the turn ends. Allow runs it; Don't allow stores a refusal as
  its result. Either way the turn goes on. A new message instead of an
  answer stores a result that says so, so every call has a result.
- **Stop** cancels the turn, the provider's requests, and its code.
- **What you are looking at:** your message is sent with what is on the
  page beside the chat, such as `the dashboard "Money"`, as a text part
  marked `context`. The model gets it after your message; the chat does not
  show it.

## Providers

A provider connects the agent to a model. It is one TypeScript file, like a
service; the built-in ones are in `backend/providers/`, embedded in Harbor.

```ts
import { defineProvider } from "harbor"

export default defineProvider({
  name: "openai",
  title: "OpenAI",
  settings: {
    apiKey: { title: "API key", secret: true },  // no default: must be filled in,
                                                 // unless optional: true
    model: { title: "Model", default: "gpt-5" },
  },
  async *generate({ instructions, messages, tools }, { settings }) {
    // yield { text }, { toolCall: { id, name, input } }, then { done: true, usage }
  },
})
```

- `messages` are the chat as stored (below), without what only the chat
  uses (`ask`, `refused`). `tools` are as MCP gives them: `name`,
  `description`, `inputSchema`.
- `generate` runs in goja, in a new runtime for each request. esbuild turns
  it into ES2017 first, which turns the async generator into code goja can
  run. It can use `fetch` (a method, headers, a string body; a response
  with `status`, `ok`, `headers.get`, `text()`, `json()`, and `body` to read
  as it streams), `TextDecoder`, and `console`. Nothing else leaves its
  runtime. Stop cancels its requests and interrupts its code.
- A built-in provider can import a module from `backend/providers/shared/`
  by a relative path, such as `./shared/responses`.
- **OpenAI** (`openai.ts`) and **OpenAI-compatible server**
  (`openai-compatible.ts`) both use the Responses API, streamed, in
  `shared/responses.ts`. Tool results go back as JSON: `{ output }`, or
  `{ error }`, with any images the tool returned, such as a screenshot,
  beside it. The model's reasoning is not sent back on later requests.
- Tools are sent with `strict: false`, for now. MCP, whose tools they are,
  has no strict mode, so it would be a layer on top for each provider; on
  vLLM a strict tool call still came back from Qwen with lists as strings;
  and holding output to a schema on vLLM lets the model loop on whitespace
  (`docs/benchmarks.md`). Go checks what a call sends instead.
- **OpenAI-compatible server** is a model on another server with that API,
  such as vLLM: its base URL (`http://localhost:8000/v1` by default), the
  model's name there, and an API key only if the server asks for one.
  **Sees images** set to `no` leaves images out of tool results, with a note
  saying how many, for a model served without vision: such a server refuses
  the whole request otherwise. Errors name the server by its host.

Providers, in Settings, shows each provider's settings. Saving one makes it the
provider the agent uses. Settings, the API key included, are kept in SQLite.
Before each turn, Harbor reads the provider in use and its settings again,
so a change takes effect on the next turn. With no provider, or a setting
missing, the chat says so and links to Providers in Settings.

## Storage

```sql
chats              -- a chat: its title, and, while a turn runs, working,
                   -- the reply so far (draft), and error
messages           -- each message, in order; parts is JSON:
                   --   {text}, {toolCall, ask}, {toolResult, refused}
settings           -- provider: the provider in use
provider_settings  -- what you filled in for each provider
```

A message does not change once it is stored. The frontend reads all of it
with live queries, so the chat updates as rows change.

The agent's replies, and the reply so far, are shown as Markdown, with
markdown-it (`framework/ui/components/Markdown.vue`). HTML in a reply is
shown as text, and a link that would run code is not a link, so a reply can
only format text. Its links open in your browser, as every link does (UI in
`docs/services-and-dashboards.md`).

## Not in this piece

Providers you add yourself, providers that run a command (such as a CLI),
and seeing a dashboard before it is saved (`preview_dashboard`).
