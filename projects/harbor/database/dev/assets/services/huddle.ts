// Huddle keeps its token in IndexedDB, as Firebase-style apps do: no cookie
// at all. The API wants it as a bearer token. Signing in happens in a popup
// that hands the token back to the page, which saves it.
//
// Search is rate-limited, 5 searches in 10 seconds; past that, the API says
// how long to wait.
import { defineService } from "harbor"

// token reads the token from Huddle's database, as Huddle's page does.
function token(): Promise<string> {
  return new Promise((resolve, reject) => {
    const open = indexedDB.open("huddle", 1)
    // On a page that never signed in, there is no database yet.
    open.onupgradeneeded = () => open.result.createObjectStore("auth")
    open.onerror = () => reject(open.error)
    open.onsuccess = () => {
      const db = open.result
      const get = db.transaction("auth", "readonly").objectStore("auth").get("session")
      get.onerror = () => reject(get.error)
      get.onsuccess = () => {
        db.close()
        resolve(get.result?.token ?? "")
      }
    }
  })
}

async function request(path: string, init: RequestInit = {}) {
  const t = await token()
  if (!t) {
    throw new Error("signed out of Huddle; sign in again")
  }
  const response = await fetch("/api" + path, {
    ...init,
    headers: { "Content-Type": "application/json", Authorization: "Bearer " + t },
  })
  const body = await response.json()
  if (response.status === 401) {
    throw new Error(`signed out of Huddle: ${body.message}`)
  }
  if (!response.ok) {
    throw new Error(body.message ?? `${path} answered HTTP ${response.status}`)
  }
  return body
}

interface Channel {
  id: string
  name: string
  topic: string
  unreadMentions: number
}

// channel takes a channel's id, name, or #name, and returns its id.
async function channel(which: string) {
  const { channels } = await request("/channels")
  const name = which.replace(/^#/, "")
  const found = (channels as Channel[]).find((c) => c.id === which || c.name === name)
  if (!found) {
    throw new Error(`there is no channel ${which}; list_channels has them`)
  }
  return found
}

interface Message {
  id: string
  channel: string
  text: string
  sent: string
  mentionsMe: boolean
  read: boolean
  author: { username: string; name: string }
}

function message(m: Message, channels: Map<string, string>) {
  return {
    id: m.id,
    channel: channels.get(m.channel) ?? m.channel,
    author: m.author.name,
    text: m.text,
    sent: m.sent,
    mentions_me: m.mentionsMe,
    read: m.read,
  }
}

async function channelNames() {
  const { channels } = await request("/channels")
  return new Map((channels as Channel[]).map((c) => [c.id, c.name]))
}

const messages = {
  type: "array",
  items: {
    type: "object",
    additionalProperties: false,
    required: ["id", "channel", "author", "text", "sent", "mentions_me", "read"],
    properties: {
      id: { type: "string" },
      channel: { type: "string", description: "The channel's name." },
      author: { type: "string" },
      text: { type: "string" },
      sent: { type: "string" },
      mentions_me: { type: "boolean" },
      read: { type: "boolean" },
    },
  },
}

export default defineService({
  name: "huddle",
  title: "Huddle",
  description: "Channels, messages, and mentions in Huddle, Northwind's team chat.",
  url: "https://huddle.localhost:8443/",

  login: {
    url: "/",
    check: async () => {
      const t = await token()
      if (!t) {
        return false
      }
      const response = await fetch("/api/me", { headers: { Authorization: "Bearer " + t } })
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
    list_channels: {
      description: "List the channels, with how many unread messages in each mention you.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["channels"],
        properties: {
          channels: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "name", "topic", "unread_mentions"],
              properties: {
                id: { type: "string" },
                name: { type: "string" },
                topic: { type: "string" },
                unread_mentions: { type: "integer" },
              },
            },
          },
        },
      },
      run: async () => {
        const { channels } = await request("/channels")
        return {
          channels: (channels as Channel[]).map((c) => ({ id: c.id, name: c.name, topic: c.topic, unread_mentions: c.unreadMentions })),
        }
      },
    },

    read_channel: {
      description: "Read a channel's latest messages, oldest first.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["channel"],
        properties: {
          channel: { type: "string", description: "The channel's name, such as general." },
          limit: { type: "integer", minimum: 1, maximum: 100, description: "How many messages. Defaults to 30." },
          before: { type: "string", description: "Only messages before this message id, for older ones." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["messages", "has_more"],
        properties: {
          messages,
          has_more: { type: "boolean", description: "Whether there are older messages; pass the first id as before." },
        },
      },
      run: async (input) => {
        const c = await channel(input.channel)
        const query = new URLSearchParams({ limit: String(input.limit ?? 30) })
        if (input.before) query.set("before", input.before)
        const body = await request(`/channels/${c.id}/messages?${query}`)
        const names = new Map([[c.id, c.name]])
        return { messages: body.messages.map((m: Message) => message(m, names)), has_more: body.hasMore }
      },
    },

    list_mentions: {
      description: "List messages that mention you, newest first.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: { unread: { type: "boolean", description: "Only ones you have not read." } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["mentions"],
        properties: { mentions: messages },
      },
      run: async (input) => {
        const body = await request(input.unread ? "/mentions?unread=1" : "/mentions")
        const names = await channelNames()
        return { mentions: body.mentions.map((m: Message) => message(m, names)) }
      },
    },

    search_messages: {
      description: "Search every channel's messages for text, newest first. Huddle allows 5 searches in 10 seconds.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["query"],
        properties: { query: { type: "string", minLength: 1 } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["results"],
        properties: { results: messages },
      },
      run: async (input) => {
        // A 429 comes back as an error with Huddle's own words: how long to wait.
        const body = await request(`/search?q=${encodeURIComponent(input.query)}`)
        const names = await channelNames()
        return { results: body.results.map((m: Message) => message(m, names)) }
      },
    },

    post_message: {
      description: "Post a message to a channel, as you.",
      annotations: { destructiveHint: false },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["channel", "text"],
        properties: {
          channel: { type: "string", description: "The channel's name, such as general." },
          text: { type: "string", minLength: 1 },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id", "channel", "sent"],
        properties: { id: { type: "string" }, channel: { type: "string" }, sent: { type: "string" } },
      },
      run: async (input) => {
        const c = await channel(input.channel)
        const m = await request(`/channels/${c.id}/messages`, { method: "POST", body: JSON.stringify({ text: input.text }) })
        return { id: m.id, channel: c.name, sent: m.sent }
      },
    },

    mark_mentions_read: {
      description: "Mark messages that mention you as read: the ones given, or every unread one.",
      annotations: { destructiveHint: false, idempotentHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        properties: {
          ids: { type: "array", items: { type: "string" }, description: "Message ids. Defaults to every unread mention." },
        },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["marked"],
        properties: { marked: { type: "integer", description: "How many were unread." } },
      },
      run: async (input) => {
        const ids = input.ids ?? (await request("/mentions?unread=1")).mentions.map((m: Message) => m.id)
        const body = await request("/mentions/read", { method: "POST", body: JSON.stringify({ ids }) })
        return { marked: body.marked }
      },
    },
  },
})
