// Brightline's pages are on brightline.localhost, but its data comes from a
// JSON API on api.brightline.localhost. The session cookie is set on the
// parent domain, so the API gets it too, as long as the request includes
// credentials; the API allows that for Brightline's own pages, where these
// tools run.
//
// Amounts are numbers of dollars; the API sends cents.
import { defineService } from "harbor"

const api = "https://api.brightline.localhost:8443/v1"

async function request(path: string, init: RequestInit = {}) {
  const response = await fetch(api + path, {
    ...init,
    credentials: "include",
    headers: { "Content-Type": "application/json", ...init.headers },
  })
  const body = await response.json()
  if (response.status === 401) {
    throw new Error("signed out of Brightline Energy; sign in again")
  }
  if (!response.ok) {
    throw new Error(body.message ?? `${path} answered HTTP ${response.status}`)
  }
  return body
}

interface Bill {
  id: string
  period: string
  kwh: number
  amountCents: number
  issued: string
  due: string
  status: string
}

function bill(b: Bill) {
  return { id: b.id, period: b.period, kwh: b.kwh, amount: b.amountCents / 100, issued: b.issued, due: b.due, status: b.status }
}

const billSchema = {
  type: "object",
  additionalProperties: false,
  required: ["id", "period", "kwh", "amount", "issued", "due", "status"],
  properties: {
    id: { type: "string" },
    period: { type: "string", description: "The month billed, YYYY-MM." },
    kwh: { type: "number" },
    amount: { type: "number", description: "Dollars." },
    issued: { type: "string" },
    due: { type: "string" },
    status: { type: "string", description: "due or paid." },
  },
}

export default defineService({
  name: "brightline",
  title: "Brightline Energy",
  description: "Electricity bills, usage, and autopay at Brightline Energy.",
  url: "https://brightline.localhost:8443/account",

  login: {
    url: "/login",
    check: async () => {
      const response = await fetch(api + "/account", { credentials: "include" })
      if (response.status === 401) {
        return false
      }
      if (!response.ok) {
        throw new Error("cannot tell yet whether you are signed in")
      }
      return true
    },
  },

  tools: {
    get_account: {
      description: "Read the account: its number, service address, plan, and whether autopay is on.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["account_number", "name", "service_address", "plan", "autopay"],
        properties: {
          account_number: { type: "string" },
          name: { type: "string" },
          service_address: { type: "string" },
          plan: { type: "string" },
          autopay: { type: "boolean" },
        },
      },
      run: async () => {
        const a = await request("/account")
        return {
          account_number: a.accountNumber,
          name: a.name,
          service_address: a.serviceAddress,
          plan: a.plan,
          autopay: a.autopay,
        }
      },
    },

    list_bills: {
      description: "List the bills, newest first.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: {
          status: { type: "string", enum: ["due", "paid"], description: "Only bills with this status." },
          limit: { type: "integer", minimum: 1, description: "How many to return. Defaults to all." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["bills"],
        properties: { bills: { type: "array", items: billSchema } },
      },
      run: async (input) => {
        const { bills } = await request("/bills")
        const matching = (bills as Bill[]).filter((b) => !input.status || b.status === input.status).map(bill)
        return { bills: input.limit === undefined ? matching : matching.slice(0, input.limit) }
      },
    },

    get_bill: {
      description: "Read one bill.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id"],
        properties: { id: { type: "string", description: "Bill id from list_bills, such as B202609." } },
      },
      outputSchema: billSchema,
      run: async (input) => bill(await request(`/bills/${encodeURIComponent(input.id)}`)),
    },

    get_usage: {
      description: "Read electricity use in kWh, by month or by day.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: {
          by: { type: "string", enum: ["month", "day"], description: "Defaults to month." },
          from: { type: "string", description: "First month or day, YYYY-MM or YYYY-MM-DD." },
          to: { type: "string", description: "Last month or day, YYYY-MM or YYYY-MM-DD." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["by", "usage"],
        properties: {
          by: { type: "string" },
          usage: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["period", "kwh"],
              properties: {
                period: { type: "string", description: "YYYY-MM, or YYYY-MM-DD by day." },
                kwh: { type: "number" },
              },
            },
          },
        },
      },
      run: async (input) => {
        const query = new URLSearchParams({ by: input.by ?? "month" })
        if (input.from) query.set("from", input.from)
        if (input.to) query.set("to", input.to)
        const body = await request(`/usage?${query}`)
        return { by: body.by, usage: body.points }
      },
    },

    export_usage: {
      description:
        "Export daily use between two dates, as Brightline's export does. It takes a few seconds: the export " +
        "runs as a job, and this waits for it.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["from", "to"],
        properties: {
          from: { type: "string", description: "First day, YYYY-MM-DD." },
          to: { type: "string", description: "Last day, YYYY-MM-DD." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["from", "to", "days"],
        properties: {
          from: { type: "string" },
          to: { type: "string" },
          days: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["date", "kwh"],
              properties: { date: { type: "string" }, kwh: { type: "number" } },
            },
          },
        },
      },
      run: async (input) => {
        let job = await request("/exports", { method: "POST", body: JSON.stringify({ from: input.from, to: input.to }) })
        for (let tries = 0; job.status !== "ready"; tries++) {
          if (tries === 30) {
            throw new Error("the export was not ready after 30 seconds")
          }
          await new Promise((resolve) => setTimeout(resolve, 1000))
          job = await request(`/exports/${job.id}`)
        }
        const file = await fetch(job.fileUrl, { credentials: "include" })
        if (!file.ok) {
          throw new Error(`the export's file answered HTTP ${file.status}`)
        }
        // date,kwh with a header line.
        const [, ...lines] = (await file.text()).trim().split("\n")
        const days = lines.map((line) => {
          const [date, kwh] = line.split(",")
          return { date, kwh: Number(kwh) }
        })
        return { from: input.from, to: input.to, days }
      },
    },

    set_autopay: {
      description: "Turn autopay on or off: whether Brightline takes each bill's amount on its due date.",
      annotations: { destructiveHint: false, idempotentHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["enabled"],
        properties: { enabled: { type: "boolean" } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["autopay"],
        properties: { autopay: { type: "boolean" } },
      },
      run: async (input) => {
        const body = await request("/autopay", { method: "PUT", body: JSON.stringify({ enabled: input.enabled }) })
        return { autopay: body.autopay }
      },
    },
  },
})
