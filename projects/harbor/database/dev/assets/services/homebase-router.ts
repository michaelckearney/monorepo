// The router's admin page uses HTTP Basic: the browser asks for the
// username and password, and sends them with every request after. Its API
// also wants a token from a cookie that the admin page sets, sent only to
// /admin/api. These tools run on that page, so the browser has both.
import { defineService } from "harbor"

const origin = "https://router.home.localhost:8443"

async function request(path: string) {
  const response = await fetch("/admin/api" + path, { credentials: "include" })
  if (response.status === 401) {
    throw new Error("signed out of the Homebase router; sign in again")
  }
  const body = await response.json()
  if (!response.ok) {
    throw new Error(body.message ?? `${path} answered HTTP ${response.status}`)
  }
  return body
}

export default defineService({
  name: "homebase-router",
  title: "Homebase router",
  description: "The home router: its internet connection, and the devices connected to it.",
  url: origin + "/admin/",

  login: {
    url: "/admin/",
    check: async () => {
      // Without the password, a browser that cannot ask for it shows an
      // error page in place of the router's.
      if (location.origin !== origin) {
        return false
      }
      const response = await fetch("/admin/api/status", { credentials: "include" })
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
    get_status: {
      description: "Read the router's model, firmware, internet connection, uptime, and how many devices are online.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["model", "firmware", "wan_ip", "wan_status", "uptime_hours", "clients_online"],
        properties: {
          model: { type: "string" },
          firmware: { type: "string" },
          wan_ip: { type: "string", description: "The home's public address." },
          wan_status: { type: "string" },
          uptime_hours: { type: "number" },
          clients_online: { type: "integer" },
        },
      },
      run: async () => {
        const s = await request("/status")
        return {
          model: s.model,
          firmware: s.firmware,
          wan_ip: s.wan.ip,
          wan_status: s.wan.status,
          uptime_hours: Math.round(s.uptimeSeconds / 360) / 10,
          clients_online: s.clientsOnline,
        }
      },
    },

    list_clients: {
      description: "List the devices the router knows, and which are connected now.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["clients"],
        properties: {
          clients: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["name", "kind", "ip", "mac", "online", "band"],
              properties: {
                name: { type: "string" },
                kind: { type: "string" },
                ip: { type: "string" },
                mac: { type: "string" },
                online: { type: "boolean" },
                band: { type: "string" },
              },
            },
          },
        },
      },
      run: async () => request("/clients"),
    },
  },
})
