// Tidewater Bank has no API: every page is rendered on the server, so these
// tools fetch the pages and read their tables. The session is an HttpOnly
// cookie that ends after 5 minutes without a request, and a page asked for
// without one redirects to /login, so that redirect is how a tool knows the
// session ended.
//
// Amounts are numbers of dollars, and dates are YYYY-MM-DD; the pages show
// "-$1,950.00" and "Mar 1, 2026".
import { defineService, fill } from "harbor"

// page fetches a page and parses it, or says to sign in when the bank sent
// the request to /login instead.
async function page(path: string) {
  const response = await fetch(path, { credentials: "include" })
  if (new URL(response.url).pathname.startsWith("/login")) {
    throw new Error("signed out of Tidewater Bank; sign in again")
  }
  if (!response.ok) {
    throw new Error(`${path} answered HTTP ${response.status}`)
  }
  return new DOMParser().parseFromString(await response.text(), "text/html")
}

function text(node: Element | null | undefined) {
  return node?.textContent?.replace(/\s+/g, " ").trim() ?? ""
}

function dollars(shown: string) {
  const n = Number(shown.replace(/[$,\s]/g, ""))
  if (!Number.isFinite(n)) {
    throw new Error(`cannot read the amount ${shown}`)
  }
  return n
}

const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

// isoDate turns "Mar 1, 2026" into "2026-03-01".
function isoDate(shown: string) {
  const m = /^(\w{3}) (\d{1,2}), (\d{4})$/.exec(shown)
  if (!m || !months.includes(m[1])) {
    throw new Error(`cannot read the date ${shown}`)
  }
  return `${m[3]}-${String(months.indexOf(m[1]) + 1).padStart(2, "0")}-${m[2].padStart(2, "0")}`
}

const transaction = {
  type: "object",
  additionalProperties: false,
  required: ["date", "description", "category", "amount", "balance"],
  properties: {
    date: { type: "string" },
    description: { type: "string" },
    category: { type: "string" },
    amount: { type: "number", description: "Dollars; negative for money out." },
    balance: { type: "number", description: "The account's balance after this transaction." },
  },
}

export default defineService({
  name: "tidewater",
  title: "Tidewater Bank",
  description: "Accounts, transactions, and statements at Tidewater Bank, and transfers between your accounts.",
  url: "https://tidewater.localhost:8443/accounts",

  login: {
    url: "/login",
    check: async () => {
      // Not following the bank's redirect to /login: loading that page
      // starts the sign-in over, and the form on the page expires.
      const response = await fetch("/accounts", { credentials: "include", redirect: "manual" })
      if (response.type === "opaqueredirect") {
        return false
      }
      if (!response.ok) {
        throw new Error("cannot tell yet whether you are signed in")
      }
      return true
    },
    // Signing in takes a page each for the username, the password, and a
    // code from an authenticator app, then one that asks whether this is a
    // new device. Harbor calls this on each page with your saved login.
    run: async () => {
      for (const what of ["username", "password", "code"] as const) {
        const field = document.querySelector<HTMLInputElement>(`form input[name="${what}"]:not([type="hidden"])`)
        if (field) {
          await fill(field, what)
          field.form!.requestSubmit()
          return
        }
      }
      document.querySelector<HTMLFormElement>('form[action="/login/new-device"]')?.requestSubmit()
    },
  },

  tools: {
    list_accounts: {
      description: "List your accounts and their balances. A credit card's balance is negative when you owe on it.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["accounts"],
        properties: {
          accounts: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "name", "number", "balance"],
              properties: {
                id: { type: "string", description: "Use it with list_transactions and transfer." },
                name: { type: "string" },
                number: { type: "string", description: "The last four digits, as the bank shows them." },
                balance: { type: "number" },
              },
            },
          },
        },
      },
      run: async () => {
        const doc = await page("/accounts")
        const accounts = Array.from(doc.querySelectorAll("table tr"))
          .filter((row) => row.querySelector("td"))
          .map((row) => {
            const cells = row.querySelectorAll("td")
            const link = cells[0].querySelector("a")
            return {
              id: link?.getAttribute("href")?.split("/").pop() ?? "",
              name: text(link),
              number: text(cells[1]),
              balance: dollars(text(cells[2])),
            }
          })
        return { accounts }
      },
    },

    list_transactions: {
      description: "List an account's transactions, newest first.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["account"],
        properties: {
          account: { type: "string", description: "Account id from list_accounts, such as chk-4821." },
          limit: { type: "integer", minimum: 1, maximum: 500, description: "How many to return. Defaults to 20." },
          since: { type: "string", description: "Only transactions on or after this date, YYYY-MM-DD." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["account", "transactions"],
        properties: {
          account: { type: "string" },
          transactions: { type: "array", items: transaction },
        },
      },
      run: async (input) => {
        const limit = input.limit ?? 20
        const out: { date: string; description: string; category: string; amount: number; balance: number }[] = []
        // The bank shows 20 transactions a page, newest first.
        for (let n = 1; out.length < limit; n++) {
          const response = await fetch(`/accounts/${encodeURIComponent(input.account)}?page=${n}`, { credentials: "include" })
          if (response.status === 404) {
            throw new Error(`there is no account ${input.account}; list_accounts has the ids`)
          }
          if (new URL(response.url).pathname.startsWith("/login")) {
            throw new Error("signed out of Tidewater Bank; sign in again")
          }
          const doc = new DOMParser().parseFromString(await response.text(), "text/html")
          for (const row of Array.from(doc.querySelectorAll("table tr")).filter((r) => r.querySelector("td"))) {
            const cells = row.querySelectorAll("td")
            const t = {
              date: isoDate(text(cells[0])),
              description: text(cells[1]),
              category: text(cells[2]),
              amount: dollars(text(cells[3])),
              balance: dollars(text(cells[4])),
            }
            if (input.since && t.date < input.since) {
              return { account: input.account, transactions: out }
            }
            if (out.length < limit) {
              out.push(t)
            }
          }
          // "Page 3 of 9"; past the last page the bank shows the last page again.
          const [, at, of] = /Page (\d+) of (\d+)/.exec(text(doc.querySelector(".pager"))) ?? []
          if (!at || Number(at) >= Number(of)) {
            break
          }
        }
        return { account: input.account, transactions: out }
      },
    },

    list_statements: {
      description: "List the monthly statements for every account, newest first.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["statements"],
        properties: {
          statements: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "account", "month", "closing_balance"],
              properties: {
                id: { type: "string", description: "Use it with get_statement." },
                account: { type: "string" },
                month: { type: "string", description: "YYYY-MM." },
                closing_balance: { type: "number" },
              },
            },
          },
        },
      },
      run: async () => {
        const doc = await page("/statements")
        const statements = Array.from(doc.querySelectorAll("table tr"))
          .filter((row) => row.querySelector("td"))
          .map((row) => {
            const cells = row.querySelectorAll("td")
            // The CSV link is /statements/chk-4821-2026-08.csv.
            const id = (cells[3].querySelector('a[href$=".csv"]')?.getAttribute("href") ?? "")
              .replace("/statements/", "")
              .replace(/\.csv$/, "")
            return {
              id,
              account: text(cells[0]),
              month: id.slice(-7),
              closing_balance: dollars(text(cells[2])),
            }
          })
        return { statements }
      },
    },

    get_statement: {
      description: "Read one monthly statement: its transactions, with the balance after each.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id"],
        properties: {
          id: { type: "string", description: "Statement id from list_statements, such as chk-4821-2026-08." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id", "transactions"],
        properties: {
          id: { type: "string" },
          transactions: { type: "array", items: transaction },
        },
      },
      run: async (input) => {
        // The CSV has the same rows as the PDF, and is far easier to read.
        const response = await fetch(`/statements/${encodeURIComponent(input.id)}.csv`, { credentials: "include" })
        if (new URL(response.url).pathname.startsWith("/login")) {
          throw new Error("signed out of Tidewater Bank; sign in again")
        }
        if (response.status === 404) {
          throw new Error(`there is no statement ${input.id}; list_statements has the ids`)
        }
        const [, ...lines] = (await response.text()).trim().split("\n")
        const transactions = lines.map((line) => {
          // Date,"Description",Category,Amount,Balance; only the description is quoted.
          const m = /^([^,]*),"((?:[^"]|"")*)",([^,]*),([^,]*),([^,]*)$/.exec(line)
          if (!m) {
            throw new Error(`cannot read the statement line ${line}`)
          }
          return {
            date: m[1],
            description: m[2].replace(/""/g, '"'),
            category: m[3],
            amount: Number(m[4]),
            balance: Number(m[5]),
          }
        })
        return { id: input.id, transactions }
      },
    },

    transfer: {
      description:
        "Move money between two of your accounts. The bank asks for your password again before a transfer, " +
        "if you signed in more than 5 minutes ago.",
      // The bank sends /transfer to this page until you have typed your
      // password in the last 5 minutes.
      login: {
        url: "/confirm-password?next=/transfer",
        check: async () => {
          const response = await fetch("/transfer", { credentials: "include", redirect: "manual" })
          return response.type !== "opaqueredirect"
        },
        run: async () => {
          const field = document.querySelector<HTMLInputElement>('form input[name="password"]')
          if (field) {
            await fill(field, "password")
            field.form!.requestSubmit()
          }
        },
      },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["from", "to", "amount"],
        properties: {
          from: { type: "string", description: "Account id to move money from, such as chk-4821." },
          to: { type: "string", description: "Account id to move money to, such as sav-7730." },
          amount: { type: "number", exclusiveMinimum: 0, description: "Dollars, such as 125.50." },
          memo: { type: "string", maxLength: 30 },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["message"],
        properties: { message: { type: "string", description: "What the bank said." } },
      },
      run: async (input) => {
        const form = await fetch("/transfer", { credentials: "include" })
        const path = new URL(form.url).pathname
        if (path.startsWith("/login")) {
          throw new Error("signed out of Tidewater Bank; sign in again")
        }
        if (path === "/confirm-password") {
          // The bank asked between login.check and now; Harbor confirms,
          // then runs the transfer again.
          throw new Error("Tidewater Bank wants your password again before a transfer")
        }
        const doc = new DOMParser().parseFromString(await form.text(), "text/html")
        const token = doc.querySelector<HTMLInputElement>('input[name="authenticity_token"]')?.value
        if (!token) {
          throw new Error("the transfer page has no form token")
        }
        const body = new URLSearchParams({
          authenticity_token: token,
          from: input.from,
          to: input.to,
          amount: input.amount.toFixed(2),
          memo: input.memo ?? "",
        })
        const response = await fetch("/transfer", { method: "POST", credentials: "include", body })
        const after = new DOMParser().parseFromString(await response.text(), "text/html")
        // A transfer that worked goes back to the accounts with a notice; one
        // that did not shows the form again with the reason.
        const problem = text(after.querySelector(".error"))
        if (problem) {
          throw new Error(problem)
        }
        const notice = text(after.querySelector(".notice"))
        if (!response.ok || !notice) {
          throw new Error(`the transfer was not confirmed (HTTP ${response.status})`)
        }
        return { message: notice }
      },
    },
  },
})
