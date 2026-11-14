// Tasklane is a single-page app on a GraphQL API that runs only the queries
// its own app was built with, sent by their SHA-256 hash; the app's script
// carries the table of names and hashes, so these tools read it from there.

import { defineService } from "harbor"

const origin = "https://tasks.northwind.localhost:8443"

const store = {
  get access() {
    return localStorage.getItem("tasklane.accessToken") ?? ""
  },
  get refresh() {
    return localStorage.getItem("tasklane.refreshToken") ?? ""
  },
  get expiresAt() {
    return Number(localStorage.getItem("tasklane.expiresAt") ?? 0)
  },
  save(t: { accessToken: string; refreshToken: string; expiresIn: number }) {
    localStorage.setItem("tasklane.accessToken", t.accessToken)
    localStorage.setItem("tasklane.refreshToken", t.refreshToken)
    localStorage.setItem("tasklane.expiresAt", String(Date.now() + t.expiresIn * 1000))
  },
  clear() {
    for (const k of ["accessToken", "refreshToken", "expiresAt"]) localStorage.removeItem("tasklane." + k)
  },
}

class SignedOut extends Error {}

async function refresh() {
  if (!store.refresh) {
    throw new SignedOut("signed out of Tasklane; sign in again")
  }
  const response = await fetch("/api/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refreshToken: store.refresh }),
  })
  const body = await response.json()
  if (response.status === 401) {
    // As the app does: the tokens are no good now.
    store.clear()
    throw new SignedOut(`signed out of Tasklane: ${body.message}`)
  }
  if (!response.ok) {
    throw new Error(body.message ?? `refreshing answered HTTP ${response.status}`)
  }
  store.save(body)
}

let hashes: Record<string, string> | undefined

async function operations() {
  if (!hashes) {
    const script = await (await fetch("/app.js")).text()
    const m = /const OPERATIONS = (\{.*?\});/.exec(script)
    if (!m) {
      throw new Error("cannot find the operations in Tasklane's app.js")
    }
    hashes = JSON.parse(m[1]) as Record<string, string>
  }
  return hashes
}

// gql runs one of Tasklane's operations by name.
async function gql(name: string, variables: Record<string, unknown> = {}) {
  const hash = (await operations())[name]
  if (store.expiresAt - Date.now() < 30_000) {
    await refresh()
  }
  for (let attempt = 0; ; attempt++) {
    const response = await fetch("/graphql", {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: "Bearer " + store.access },
      body: JSON.stringify({ operationName: name, variables, extensions: { persistedQuery: { version: 1, sha256Hash: hash } } }),
    })
    const body = await response.json()
    if (response.status === 401 && attempt === 0) {
      await refresh()
      continue
    }
    if (body.errors) {
      throw new Error(body.errors.map((e: { message: string }) => e.message).join("; "))
    }
    return body.data
  }
}

// assignee turns "me" into the signed-in user's username.
async function assignee(who: string | undefined) {
  if (who !== "me") {
    return who
  }
  return (await gql("Viewer")).viewer.username as string
}

interface Issue {
  key: string
  title: string
  description?: string
  state: string
  priority: string
  labels: string[]
  created?: string
  updated: string
  project: { key: string; name?: string }
  assignee: { username: string; name: string } | null
}

function issue(i: Issue) {
  return {
    key: i.key,
    title: i.title,
    state: i.state,
    priority: i.priority,
    labels: i.labels,
    project: i.project.key,
    assignee: i.assignee,
    updated: i.updated,
    url: `${origin}/issues/${i.key}`,
  }
}

const states = ["TODO", "IN_PROGRESS", "IN_REVIEW", "DONE"]
const priorities = ["LOW", "MEDIUM", "HIGH", "URGENT"]

const person = {
  type: ["object", "null"],
  additionalProperties: false,
  required: ["username", "name"],
  properties: { username: { type: "string" }, name: { type: "string" } },
}

const issueProperties = {
  key: { type: "string" },
  title: { type: "string" },
  state: { type: "string", enum: states },
  priority: { type: "string", enum: priorities },
  labels: { type: "array", items: { type: "string" } },
  project: { type: "string" },
  assignee: person,
  updated: { type: "string" },
  url: { type: "string" },
}

export default defineService({
  name: "tasklane",
  title: "Tasklane",
  description: "Issues and projects in Tasklane, Northwind's issue tracker.",
  url: origin + "/",

  login: {
    url: "/",
    check: async ({ cookies }) => cookies.some((c) => c.name === "tasklane_session"),
  },

  tools: {
    list_projects: {
      description: "List the projects.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["projects"],
        properties: {
          projects: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["key", "name"],
              properties: { key: { type: "string" }, name: { type: "string" } },
            },
          },
        },
      },
      run: async () => ({ projects: (await gql("Projects")).projects }),
    },

    list_issues: {
      description: "List issues, most recently updated first.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: {
          project: { type: "string", description: "Project key from list_projects, such as WEB." },
          state: { type: "string", enum: states },
          assignee: { type: "string", description: 'A username, or "me".' },
          limit: { type: "integer", minimum: 1, description: "How many to return. Defaults to 50." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["total", "issues"],
        properties: {
          total: { type: "integer", description: "How many issues match, including any past the limit." },
          issues: {
            type: "array",
            items: { type: "object", additionalProperties: false, required: Object.keys(issueProperties), properties: issueProperties },
          },
        },
      },
      run: async (input) => {
        const limit = input.limit ?? 50
        const filters: Record<string, unknown> = {}
        if (input.project) filters.project = input.project
        if (input.state) filters.state = input.state
        if (input.assignee) filters.assignee = await assignee(input.assignee)
        const issues = []
        let after: string | null = null
        let total = 0
        do {
          const page: { issues: { totalCount: number; pageInfo: { hasNextPage: boolean; endCursor: string | null }; edges: { node: Issue }[] } } =
            await gql("Issues", { ...filters, first: Math.min(50, limit - issues.length), after })
          total = page.issues.totalCount
          issues.push(...page.issues.edges.map((e) => issue(e.node)))
          after = page.issues.pageInfo.hasNextPage ? page.issues.pageInfo.endCursor : null
        } while (after && issues.length < limit)
        return { total, issues }
      },
    },

    get_issue: {
      description: "Read one issue, with its description.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["key"],
        properties: { key: { type: "string", description: "Issue key, such as WEB-12." } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: [...Object.keys(issueProperties), "description", "created"],
        properties: { ...issueProperties, description: { type: "string" }, created: { type: "string" } },
      },
      run: async (input) => {
        const { issue: found } = await gql("Issue", { key: input.key })
        if (!found) {
          throw new Error(`there is no issue ${input.key}`)
        }
        return { ...issue(found), description: found.description, created: found.created }
      },
    },

    create_issue: {
      description: "Create an issue.",
      annotations: { destructiveHint: false },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["project", "title"],
        properties: {
          project: { type: "string", description: "Project key, such as WEB." },
          title: { type: "string", minLength: 1 },
          description: { type: "string" },
          priority: { type: "string", enum: priorities, description: "Defaults to MEDIUM." },
          assignee: { type: "string", description: 'A username, or "me".' },
          labels: { type: "array", items: { type: "string" } },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["key", "url"],
        properties: { key: { type: "string" }, url: { type: "string" } },
      },
      run: async (input) => {
        const { createIssue } = await gql("CreateIssue", { input: { ...input, assignee: await assignee(input.assignee) } })
        return { key: createIssue.key, url: `${origin}/issues/${createIssue.key}` }
      },
    },

    update_issue: {
      description: "Change an issue: its state, title, description, priority, or assignee. Only what you send changes.",
      annotations: { destructiveHint: false, idempotentHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["key"],
        properties: {
          key: { type: "string", description: "Issue key, such as WEB-12." },
          state: { type: "string", enum: states },
          title: { type: "string", minLength: 1 },
          description: { type: "string" },
          priority: { type: "string", enum: priorities },
          assignee: { type: "string", description: 'A username, or "me".' },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["key", "title", "state", "priority"],
        properties: { key: { type: "string" }, title: { type: "string" }, state: { type: "string" }, priority: { type: "string" } },
      },
      run: async (input) => {
        const { key, ...changes } = input
        if (changes.assignee) {
          changes.assignee = await assignee(changes.assignee)
        }
        const { updateIssue } = await gql("UpdateIssue", { key, input: changes })
        return { key: updateIssue.key, title: updateIssue.title, state: updateIssue.state, priority: updateIssue.priority }
      },
    },
  },
})
