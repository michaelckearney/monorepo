// Homebase sits behind a login portal on login.home.localhost: the server
// in front asks the portal about every request, and sends a browser that is
// not signed in there to sign in. A fetch cannot follow that redirect to
// another site here, so the tools ask not to, and treat it as signed out.
//
// Homebase also ties its API to the browser that opened its home page, by a
// cookie that page sets. These tools run on that page, so they have it.
import { defineService } from "harbor"

const origin = "https://home.localhost:8443"

async function request(path: string) {
  const response = await fetch("/api" + path, { credentials: "include", redirect: "manual" })
  if (response.type === "opaqueredirect" || response.status === 401) {
    throw new Error("signed out of Homebase; sign in again")
  }
  const body = await response.json()
  if (!response.ok) {
    throw new Error(body.message ?? `${path} answered HTTP ${response.status}`)
  }
  return body
}

export default defineService({
  name: "homebase",
  title: "Homebase",
  description: "Devices on the network, disk space, and sensor readings from Homebase, the home server.",
  url: origin + "/",

  login: {
    url: "/",
    check: async () => {
      // Signed out, the tab ends up on the login portal.
      if (location.origin !== origin) {
        return false
      }
      const response = await fetch("/api/me", { credentials: "include", redirect: "manual" })
      if (response.type === "opaqueredirect" || response.status === 401) {
        return false
      }
      if (!response.ok) {
        throw new Error("cannot tell yet whether you are signed in")
      }
      return true
    },
  },

  tools: {
    list_devices: {
      description: "List the devices Homebase knows on the home network, and which are online.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: { online: { type: "boolean", description: "Only devices online now." } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["devices"],
        properties: {
          devices: {
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
                band: { type: "string", description: "The Wi-Fi band, or wired." },
              },
            },
          },
        },
      },
      run: async (input) => {
        const { devices } = await request("/devices")
        return { devices: devices.filter((d: { online: boolean }) => !input.online || d.online) }
      },
    },

    get_storage: {
      description: "Read each disk volume's size, and how much is used and free.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["volumes"],
        properties: {
          volumes: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["name", "size_gb", "used_gb", "free_gb", "used_percent"],
              properties: {
                name: { type: "string" },
                size_gb: { type: "number" },
                used_gb: { type: "number" },
                free_gb: { type: "number" },
                used_percent: { type: "number" },
              },
            },
          },
        },
      },
      run: async () => {
        const { volumes } = await request("/storage")
        return {
          volumes: volumes.map((v: { name: string; sizeGB: number; usedGB: number }) => ({
            name: v.name,
            size_gb: v.sizeGB,
            used_gb: v.usedGB,
            free_gb: v.sizeGB - v.usedGB,
            used_percent: Math.round((v.usedGB / v.sizeGB) * 1000) / 10,
          })),
        }
      },
    },

    get_sensors: {
      description:
        "Read every sensor now. Homebase's page follows them live; this reads them once, which is what a question needs.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["sensors"],
        properties: {
          sensors: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "name", "value", "unit", "time"],
              properties: {
                id: { type: "string" },
                name: { type: "string" },
                value: { type: "number" },
                unit: { type: "string" },
                time: { type: "string" },
              },
            },
          },
        },
      },
      run: async () => request("/sensors"),
    },
  },
})
