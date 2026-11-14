// Parcel's orders page embeds its first ten orders in the page, and loads
// the rest from /api/orders as you scroll, a page at a time by cursor. These
// tools use that API directly. Writes need the XSRF-TOKEN cookie copied
// into an X-XSRF-TOKEN header, as Parcel's own pages do; the cookie is
// readable by the page for that reason.
//
// Amounts are numbers of dollars; the API sends cents.
import { defineService } from "harbor"

async function request(path: string, init: RequestInit = {}) {
  const response = await fetch("/api" + path, { ...init, credentials: "include" })
  const body = await response.json()
  if (response.status === 401) {
    throw new Error("signed out of Parcel; sign in again")
  }
  if (!response.ok) {
    throw new Error(body.message ?? `${path} answered HTTP ${response.status}`)
  }
  return body
}

function xsrfToken() {
  const m = /(?:^|; )XSRF-TOKEN=([^;]*)/.exec(document.cookie)
  if (!m) {
    throw new Error("Parcel's XSRF-TOKEN cookie is missing; sign in again")
  }
  return decodeURIComponent(m[1])
}

export default defineService({
  name: "parcel",
  title: "Parcel",
  description: "Orders, deliveries, and returns at Parcel.",
  url: "https://parcel.localhost:8443/orders",

  login: {
    url: "/signin",
    check: async () => {
      const response = await fetch("/api/orders?limit=1", { credentials: "include" })
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
    list_orders: {
      description: "List your orders, newest first, without their items or tracking.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: {
          limit: { type: "integer", minimum: 1, description: "How many to return. Defaults to 10." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["orders"],
        properties: {
          orders: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "placed", "status", "total", "item_count", "summary"],
              properties: {
                id: { type: "string", description: "Use it with get_order." },
                placed: { type: "string" },
                status: { type: "string", description: "Such as delivered or cancelled." },
                total: { type: "number" },
                item_count: { type: "integer" },
                summary: { type: "string", description: "The first item, and how many more." },
              },
            },
          },
        },
      },
      run: async (input) => {
        const limit = input.limit ?? 10
        const orders: Record<string, unknown>[] = []
        let cursor = ""
        do {
          const query = new URLSearchParams({ limit: String(Math.min(50, limit - orders.length)) })
          if (cursor) query.set("cursor", cursor)
          const page = await request(`/orders?${query}`)
          for (const o of page.orders) {
            orders.push({
              id: o.id,
              placed: o.placed,
              status: o.status,
              total: o.totalCents / 100,
              item_count: o.itemCount,
              summary: o.firstItem + (o.itemCount > 1 ? ` and ${o.itemCount - 1} more` : ""),
            })
          }
          cursor = page.nextCursor
        } while (cursor && orders.length < limit)
        return { orders }
      },
    },

    get_order: {
      description: "Read one order: its items, costs, where it shipped, its tracking, and any returns.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id"],
        properties: { id: { type: "string", description: "Order id from list_orders, such as P1364274-046." } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id", "placed", "status", "items", "subtotal", "shipping", "tax", "total", "ship_to", "tracking", "returns"],
        properties: {
          id: { type: "string" },
          placed: { type: "string" },
          status: { type: "string" },
          items: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["sku", "name", "quantity", "price"],
              properties: {
                sku: { type: "string", description: "Use it with start_return." },
                name: { type: "string" },
                quantity: { type: "integer" },
                price: { type: "number", description: "For one." },
              },
            },
          },
          subtotal: { type: "number" },
          shipping: { type: "number" },
          tax: { type: "number" },
          total: { type: "number" },
          ship_to: { type: "string" },
          tracking: {
            type: ["object", "null"],
            additionalProperties: false,
            required: ["carrier", "number", "events"],
            properties: {
              carrier: { type: "string" },
              number: { type: "string" },
              events: {
                type: "array",
                description: "Oldest first.",
                items: {
                  type: "object",
                  additionalProperties: false,
                  required: ["time", "place", "status"],
                  properties: { time: { type: "string" }, place: { type: "string" }, status: { type: "string" } },
                },
              },
            },
          },
          returns: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "skus", "reason", "created", "label_url"],
              properties: {
                id: { type: "string" },
                skus: { type: "array", items: { type: "string" } },
                reason: { type: "string" },
                created: { type: "string" },
                label_url: { type: "string" },
              },
            },
          },
        },
      },
      run: async (input) => {
        const o = await request(`/orders/${encodeURIComponent(input.id)}`)
        return {
          id: o.id,
          placed: o.placed,
          status: o.status,
          items: o.items.map((i: { sku: string; name: string; quantity: number; priceCents: number }) => ({
            sku: i.sku,
            name: i.name,
            quantity: i.quantity,
            price: i.priceCents / 100,
          })),
          subtotal: o.subtotalCents / 100,
          shipping: o.shippingCents / 100,
          tax: o.taxCents / 100,
          total: o.totalCents / 100,
          ship_to: o.shipTo,
          tracking: o.tracking ?? null,
          returns: (o.returns ?? []).map((r: { id: string; skus: string[]; reason: string; created: string; labelUrl: string }) => ({
            id: r.id,
            skus: r.skus,
            reason: r.reason,
            created: r.created,
            label_url: r.labelUrl,
          })),
        }
      },
    },

    start_return: {
      description: "Start a return for items in a delivered order. Parcel makes a shipping label to print.",
      annotations: { destructiveHint: false },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["order", "skus", "reason"],
        properties: {
          order: { type: "string", description: "Order id, such as P1364274-046." },
          skus: { type: "array", minItems: 1, items: { type: "string" }, description: "The items to return, from get_order." },
          reason: {
            type: "string",
            enum: ["Changed my mind", "Arrived damaged", "Wrong item", "Doesn't fit"],
          },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id", "label_url"],
        properties: {
          id: { type: "string" },
          label_url: { type: "string", description: "A PDF to print, on Parcel's site." },
        },
      },
      run: async (input) => {
        const r = await request(`/orders/${encodeURIComponent(input.order)}/returns`, {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-XSRF-TOKEN": xsrfToken() },
          body: JSON.stringify({ skus: input.skus, reason: input.reason }),
        })
        return { id: r.id, label_url: r.labelUrl }
      },
    },
  },
})
