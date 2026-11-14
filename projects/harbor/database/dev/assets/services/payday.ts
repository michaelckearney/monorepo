// Payday's page keeps its access token in memory only, and gets a new one
// on every load from /api/auth/refresh, which reads an HttpOnly cookie sent
// only to that path. These tools do the same: the refresh cookie does not
// change when used, so a tool refreshing beside the page does no harm.
//
// Payday's API takes batches of calls, POST /rpc?batch=1, and answers in its
// own packed format: the JSON, gzipped, then base64.
//
// Changing bank details is left out: Payday asks for the password again
// first, and it is not something to hand to an agent.
//
// Amounts are numbers of dollars; the API sends cents.
import { defineService } from "harbor"

const origin = "https://payroll.northwind.localhost:8443"

let token = ""
let tokenUntil = 0

async function accessToken() {
  if (Date.now() < tokenUntil - 30_000) {
    return token
  }
  const response = await fetch("/api/auth/refresh", { method: "POST", credentials: "include" })
  const body = await response.json()
  if (response.status === 401) {
    throw new Error(`signed out of Payday: ${body.message}`)
  }
  if (!response.ok) {
    throw new Error(body.message ?? `refreshing answered HTTP ${response.status}`)
  }
  token = body.accessToken
  tokenUntil = Date.now() + body.expiresIn * 1000
  return token
}

async function unpack(response: Response) {
  const packed = Uint8Array.from(atob(await response.text()), (c) => c.charCodeAt(0))
  const stream = new Blob([packed]).stream().pipeThrough(new DecompressionStream("gzip"))
  return JSON.parse(await new Response(stream).text())
}

// rpc makes one call. Payday's own page batches its calls; one at a time is
// as good for a tool.
async function rpc(method: string, params: Record<string, unknown> = {}) {
  for (let attempt = 0; ; attempt++) {
    const response = await fetch("/rpc?batch=1", {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: "Bearer " + (await accessToken()) },
      body: JSON.stringify([{ id: 1, method, params }]),
    })
    if (response.status === 401 && attempt === 0) {
      tokenUntil = 0
      continue
    }
    const [reply] = await unpack(response)
    if (reply.error) {
      throw new Error(reply.error.message)
    }
    return reply.result
  }
}

interface TimeOff {
  id: string
  type: string
  start: string
  end: string
  hours: number
  status: string
  note: string
}

const timeOff = {
  type: "object",
  additionalProperties: false,
  required: ["id", "type", "start", "end", "hours", "status", "note"],
  properties: {
    id: { type: "string" },
    type: { type: "string", description: "vacation, sick, or personal." },
    start: { type: "string" },
    end: { type: "string" },
    hours: { type: "number", description: "Weekdays count 8 hours." },
    status: { type: "string", description: "pending, approved, or cancelled." },
    note: { type: "string" },
  },
}

export default defineService({
  name: "payday",
  title: "Payday",
  description: "Payslips, time off, and pay details at Payday, Northwind's HR and payroll.",
  url: origin + "/",

  login: {
    url: "/auth/login",
    check: async () => {
      // Signing in goes through Northwind ID, where the tab may still be.
      if (location.origin !== origin) {
        return false
      }
      const response = await fetch("/api/auth/refresh", { method: "POST", credentials: "include" })
      if (response.status === 401) {
        return false
      }
      if (!response.ok) {
        throw new Error("cannot tell yet whether you are signed in")
      }
      const body = await response.json()
      token = body.accessToken
      tokenUntil = Date.now() + body.expiresIn * 1000
      return true
    },
  },

  tools: {
    list_payslips: {
      description: "List payslips, newest first.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: {
          year: { type: "integer", description: "Only payslips paid in this year." },
          limit: { type: "integer", minimum: 1, description: "How many to return. Defaults to all." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["payslips"],
        properties: {
          payslips: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "pay_date", "period_start", "period_end", "gross", "net"],
              properties: {
                id: { type: "string", description: "Use it with get_payslip." },
                pay_date: { type: "string" },
                period_start: { type: "string" },
                period_end: { type: "string" },
                gross: { type: "number" },
                net: { type: "number", description: "What was paid in, after deductions." },
              },
            },
          },
        },
      },
      run: async (input) => {
        const list = await rpc("payslips.list", input.year ? { year: input.year } : {})
        const payslips = list
          .map((p: { id: string; payDate: string; periodStart: string; periodEnd: string; grossCents: number; netCents: number }) => ({
            id: p.id,
            pay_date: p.payDate,
            period_start: p.periodStart,
            period_end: p.periodEnd,
            gross: p.grossCents / 100,
            net: p.netCents / 100,
          }))
          .sort((a: { pay_date: string }, b: { pay_date: string }) => b.pay_date.localeCompare(a.pay_date))
        return { payslips: input.limit === undefined ? payslips : payslips.slice(0, input.limit) }
      },
    },

    get_payslip: {
      description: "Read one payslip, with each deduction.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id"],
        properties: { id: { type: "string", description: "Payslip id from list_payslips, such as PS-2026-09-18." } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id", "pay_date", "period_start", "period_end", "gross", "deductions", "net"],
        properties: {
          id: { type: "string" },
          pay_date: { type: "string" },
          period_start: { type: "string" },
          period_end: { type: "string" },
          gross: { type: "number" },
          deductions: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["name", "amount"],
              properties: { name: { type: "string" }, amount: { type: "number" } },
            },
          },
          net: { type: "number" },
        },
      },
      run: async (input) => {
        const p = await rpc("payslips.get", { id: input.id })
        return {
          id: p.id,
          pay_date: p.payDate,
          period_start: p.periodStart,
          period_end: p.periodEnd,
          gross: p.grossCents / 100,
          deductions: p.deductions.map((d: { name: string; amountCents: number }) => ({ name: d.name, amount: d.amountCents / 100 })),
          net: p.netCents / 100,
        }
      },
    },

    get_time_off: {
      description: "Read the hours of time off left, by type, and every request.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["balances", "requests"],
        properties: {
          balances: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["type", "label", "hours"],
              properties: { type: { type: "string" }, label: { type: "string" }, hours: { type: "number" } },
            },
          },
          requests: { type: "array", items: timeOff },
        },
      },
      run: async () => {
        const balances = await rpc("timeoff.balances")
        const requests: TimeOff[] = await rpc("timeoff.list")
        return { balances, requests }
      },
    },

    get_bank_details: {
      description: "Read where your pay goes: the bank, and the account's last four digits.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["bank_name", "routing_number", "account_last4", "account_type"],
        properties: {
          bank_name: { type: "string" },
          routing_number: { type: "string" },
          account_last4: { type: "string" },
          account_type: { type: "string" },
        },
      },
      run: async () => {
        const b = await rpc("bank.get")
        return { bank_name: b.bankName, routing_number: b.routingNumber, account_last4: b.accountLast4, account_type: b.accountType }
      },
    },

    request_time_off: {
      description: "Ask for time off. Weekdays count 8 hours each; your manager approves it in Payday.",
      annotations: { destructiveHint: false },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["type", "start", "end"],
        properties: {
          type: { type: "string", enum: ["vacation", "sick", "personal"] },
          start: { type: "string", description: "First day off, YYYY-MM-DD." },
          end: { type: "string", description: "Last day off, YYYY-MM-DD." },
          note: { type: "string" },
        },
      },
      outputSchema: timeOff,
      run: async (input) => rpc("timeoff.request", { ...input, note: input.note ?? "" }),
    },

    cancel_time_off: {
      description: "Cancel a time-off request that is still pending.",
      annotations: { destructiveHint: false },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id"],
        properties: { id: { type: "string", description: "Request id from get_time_off, such as TO-366." } },
      },
      outputSchema: timeOff,
      run: async (input) => rpc("timeoff.cancel", { id: input.id }),
    },
  },
})
