// Clearwater's portal shows one person's record at a time: yours, or a
// child's. The session remembers whose, and the portal switches it with
// POST /api/v2/patient. Tools that take a person switch to them, then switch
// back, so the portal you have open is left as it was.
//
// The API answers only requests with its client header, and starts every
// reply with ")]}'" so another site cannot load it as a script; the text is
// cut before parsing. A session lasts 30 minutes, used or not.
import { defineService } from "harbor"

const prefix = ")]}'\n"

async function request(path: string, init: RequestInit = {}) {
  const response = await fetch("/api/v2" + path, {
    ...init,
    credentials: "include",
    headers: { "X-Clearwater-Client": "portal-web/3.2", "Content-Type": "application/json" },
  })
  const raw = await response.text()
  const body = JSON.parse(raw.startsWith(prefix) ? raw.slice(prefix.length) : raw)
  if (response.status === 401) {
    throw new Error("signed out of Clearwater Health; sign in again")
  }
  if (!response.ok) {
    throw new Error(body.message ?? `${path} answered HTTP ${response.status}`)
  }
  return body
}

// as runs fn with the portal showing person's record. Without a person, it
// is whoever the portal shows now.
async function as<T>(person: string | undefined, fn: () => Promise<T>): Promise<T> {
  const me = await request("/me")
  const was = me.patient.id
  if (!person || person === was) {
    return fn()
  }
  if (!me.patients.some((p: { id: string }) => p.id === person)) {
    throw new Error(`there is no person ${person}; list_people has the ids`)
  }
  await request("/patient", { method: "POST", body: JSON.stringify({ patient: person }) })
  try {
    return await fn()
  } finally {
    await request("/patient", { method: "POST", body: JSON.stringify({ patient: was }) })
  }
}

const person = {
  type: "string",
  description: "Whose record, by id from list_people, such as p-sam. Defaults to the one the portal shows.",
}

export default defineService({
  name: "clearwater",
  title: "Clearwater Health",
  description: "Appointments, test results, and messages from Clearwater Health, for you and your children.",
  url: "https://clearwater.localhost:8443/app/",

  login: {
    url: "/",
    check: async () => {
      const response = await fetch("/api/v2/me", {
        credentials: "include",
        headers: { "X-Clearwater-Client": "portal-web/3.2" },
      })
      // 403 is a sign-in not finished yet: no person chosen, or the terms
      // not accepted.
      if (response.status === 401 || response.status === 403) {
        return false
      }
      if (!response.ok) {
        throw new Error("cannot tell yet whether you are signed in")
      }
      return true
    },
  },

  tools: {
    list_people: {
      description: "List the people whose records you can see, and whose the portal shows now.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["showing", "people"],
        properties: {
          showing: { type: "string", description: "The id of the person the portal shows." },
          people: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "name", "relationship", "born"],
              properties: {
                id: { type: "string" },
                name: { type: "string" },
                relationship: { type: "string", description: "You, or Child." },
                born: { type: "string" },
              },
            },
          },
        },
      },
      run: async () => {
        const me = await request("/me")
        return {
          showing: me.patient.id,
          people: me.patients.map((p: { id: string; name: string; relationship: string; born: string }) => ({
            id: p.id,
            name: p.name,
            relationship: p.relationship,
            born: p.born,
          })),
        }
      },
    },

    list_appointments: {
      description: "List a person's appointments, past and upcoming, oldest first.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: {
          person,
          upcoming: { type: "boolean", description: "Only appointments that have not happened yet." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["appointments"],
        properties: {
          appointments: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "start", "minutes", "clinician", "department", "location", "reason", "status", "telehealth"],
              properties: {
                id: { type: "string" },
                start: { type: "string", description: "Date and time with its offset." },
                minutes: { type: "integer" },
                clinician: { type: "string" },
                department: { type: "string" },
                location: { type: "string" },
                reason: { type: "string" },
                status: { type: "string", description: "scheduled or completed." },
                telehealth: { type: "boolean" },
              },
            },
          },
        },
      },
      run: async (input) => {
        const { appointments } = await as(input.person, () => request("/appointments"))
        const now = Date.now()
        return {
          appointments: appointments
            .filter((a: { start: string }) => !input.upcoming || Date.parse(a.start) > now)
            .sort((a: { start: string }, b: { start: string }) => Date.parse(a.start) - Date.parse(b.start))
            .map(({ patient, ...a }: { patient: string }) => a),
        }
      },
    },

    list_results: {
      description: "List a person's test results, newest first, without their values.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: { person } },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["results"],
        properties: {
          results: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "name", "collected", "status", "ordered_by", "flagged"],
              properties: {
                id: { type: "string", description: "Use it with get_result." },
                name: { type: "string" },
                collected: { type: "string" },
                status: { type: "string" },
                ordered_by: { type: "string" },
                flagged: { type: "integer", description: "How many values are outside the normal range." },
              },
            },
          },
        },
      },
      run: async (input) => {
        const { results } = await as(input.person, () => request("/results"))
        return {
          results: results
            .map((r: { id: string; name: string; collected: string; status: string; orderedBy: string; components: { flag: string }[] }) => ({
              id: r.id,
              name: r.name,
              collected: r.collected,
              status: r.status,
              ordered_by: r.orderedBy,
              flagged: r.components.filter((c) => c.flag).length,
            }))
            .sort((a: { collected: string }, b: { collected: string }) => b.collected.localeCompare(a.collected)),
        }
      },
    },

    get_result: {
      description: "Read one test result: each value, its normal range, and whether it is high or low.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id"],
        properties: { id: { type: "string", description: "Result id from list_results, such as R-7710." }, person },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id", "name", "collected", "status", "ordered_by", "values"],
        properties: {
          id: { type: "string" },
          name: { type: "string" },
          collected: { type: "string" },
          status: { type: "string" },
          ordered_by: { type: "string" },
          values: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["name", "value", "unit", "range", "flag"],
              properties: {
                name: { type: "string" },
                value: { type: "string" },
                unit: { type: "string" },
                range: { type: "string" },
                flag: { type: "string", description: "high, low, or empty when normal." },
              },
            },
          },
        },
      },
      run: async (input) => {
        const r = await as(input.person, () => request(`/results/${encodeURIComponent(input.id)}`))
        return { id: r.id, name: r.name, collected: r.collected, status: r.status, ordered_by: r.orderedBy, values: r.components }
      },
    },

    list_messages: {
      description: "List the messages from a person's care team, newest first.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: { person, unread: { type: "boolean", description: "Only unread messages." } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["messages"],
        properties: {
          messages: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "from", "subject", "sent", "read", "body"],
              properties: {
                id: { type: "string" },
                from: { type: "string" },
                subject: { type: "string" },
                sent: { type: "string" },
                read: { type: "boolean" },
                body: { type: "string" },
              },
            },
          },
        },
      },
      run: async (input) => {
        const { messages } = await as(input.person, () => request("/messages"))
        return {
          messages: messages
            .filter((m: { read: boolean }) => !input.unread || !m.read)
            .map(({ patient, ...m }: { patient: string }) => m),
        }
      },
    },

    mark_message_read: {
      description: "Mark a message as read.",
      annotations: { destructiveHint: false, idempotentHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id"],
        properties: { id: { type: "string", description: "Message id from list_messages, such as M-301." }, person },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id", "read"],
        properties: { id: { type: "string" }, read: { type: "boolean" } },
      },
      run: async (input) => {
        const m = await as(input.person, () => request(`/messages/${encodeURIComponent(input.id)}/read`, { method: "POST" }))
        return { id: m.id, read: m.read }
      },
    },
  },
})
