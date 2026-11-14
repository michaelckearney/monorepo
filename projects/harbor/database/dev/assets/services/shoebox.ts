// Shoebox's session is a JWT in an HttpOnly cookie, which the API reads.
// Photos are not served by the API: it hands out a link to
// files.shoebox.localhost, signed and good for 5 minutes, and the bytes come
// from there.
//
// Uploads and share links must also be signed, as Shoebox's page signs
// them: an HMAC of the method, path, and time, with a key the session hands
// out at /api/session.
import { defineService } from "harbor"

async function request(path: string, init: RequestInit = {}) {
  const response = await fetch("/api" + path, { ...init, credentials: "include" })
  const body = await response.json()
  if (response.status === 401 && body.error === "unauthenticated") {
    throw new Error("signed out of Shoebox; sign in again")
  }
  if (!response.ok) {
    throw new Error(body.message ?? `${path} answered HTTP ${response.status}`)
  }
  return body
}

// signed returns the headers that sign a request to path.
async function signed(method: string, path: string) {
  const { signingKey } = await request("/session")
  // Whole seconds: Shoebox's page sends the time without milliseconds.
  const date = new Date().toISOString().replace(/\.\d+Z$/, "Z")
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(signingKey), { name: "HMAC", hash: "SHA-256" }, false, ["sign"])
  const mac = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(`${method}\n${path}\n${date}`))
  const hex = Array.from(new Uint8Array(mac), (b) => b.toString(16).padStart(2, "0")).join("")
  return { "X-Shoebox-Date": date, "X-Shoebox-Signature": hex }
}

function base64(bytes: Uint8Array) {
  let binary = ""
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}

const photo = {
  type: "object",
  additionalProperties: false,
  required: ["id", "name", "taken", "width", "height", "content_type", "size"],
  properties: {
    id: { type: "string", description: "Use it with download_photo and share_photo." },
    name: { type: "string" },
    taken: { type: "string" },
    width: { type: "integer" },
    height: { type: "integer" },
    content_type: { type: "string" },
    size: { type: "integer", description: "Bytes." },
  },
}

export default defineService({
  name: "shoebox",
  title: "Shoebox",
  description: "Albums and photos in Shoebox: download, upload, and share them.",
  url: "https://shoebox.localhost:8443/",

  login: {
    url: "/login",
    check: async () => {
      const response = await fetch("/api/session", { credentials: "include" })
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
    list_albums: {
      description: "List the albums, with how many photos each has.",
      annotations: { readOnlyHint: true },
      inputSchema: { type: "object", additionalProperties: false, properties: {} },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["albums"],
        properties: {
          albums: {
            type: "array",
            items: {
              type: "object",
              additionalProperties: false,
              required: ["id", "title", "photo_count", "cover_photo"],
              properties: {
                id: { type: "string", description: "Use it with get_album and upload_photo." },
                title: { type: "string" },
                photo_count: { type: "integer" },
                cover_photo: { type: "string", description: "A photo id, or empty for an empty album." },
              },
            },
          },
        },
      },
      run: async () => {
        const { albums } = await request("/albums")
        return {
          albums: albums.map((a: { id: string; title: string; count: number; coverPhoto: string }) => ({
            id: a.id,
            title: a.title,
            photo_count: a.count,
            cover_photo: a.coverPhoto,
          })),
        }
      },
    },

    get_album: {
      description: "List the photos in an album.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["album"],
        properties: { album: { type: "string", description: "Album id from list_albums, such as al-coast." } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["id", "title", "photos"],
        properties: { id: { type: "string" }, title: { type: "string" }, photos: { type: "array", items: photo } },
      },
      run: async (input) => {
        const body = await request(`/albums/${encodeURIComponent(input.album)}`)
        return {
          id: body.album.id,
          title: body.album.title,
          photos: body.photos.map(
            (p: { id: string; name: string; taken: string; width: number; height: number; contentType: string; size: number }) => ({
              id: p.id,
              name: p.name,
              taken: p.taken,
              width: p.width,
              height: p.height,
              content_type: p.contentType,
              size: p.size,
            }),
          ),
        }
      },
    },

    download_photo: {
      description: "Download a photo. It comes back as a data: URL, which an <img> can show.",
      annotations: { readOnlyHint: true },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["photo"],
        properties: { photo: { type: "string", description: "Photo id, such as ph-001." } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["name", "content_type", "data_url"],
        properties: { name: { type: "string" }, content_type: { type: "string" }, data_url: { type: "string" } },
      },
      run: async (input) => {
        const link = await request(`/photos/${encodeURIComponent(input.photo)}/download`)
        // The link needs no cookie, and the file host allows any page to read it.
        const file = await fetch(link.url)
        if (!file.ok) {
          throw new Error(`the photo's link answered HTTP ${file.status}: ${await file.text()}`)
        }
        const bytes = new Uint8Array(await file.arrayBuffer())
        return { name: link.name, content_type: link.contentType, data_url: `data:${link.contentType};base64,${base64(bytes)}` }
      },
    },

    upload_photo: {
      description: "Add a photo to an album.",
      annotations: { destructiveHint: false },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["album", "file_name", "content_base64"],
        properties: {
          album: { type: "string", description: "Album id, such as al-misc." },
          file_name: { type: "string", description: "Such as garden.jpg." },
          content_base64: { type: "string", description: "The image file, in base64." },
        },
      },
      outputSchema: photo,
      run: async (input) => {
        const path = `/api/albums/${encodeURIComponent(input.album)}/photos`
        const bytes = Uint8Array.from(atob(input.content_base64), (c) => c.charCodeAt(0))
        const form = new FormData()
        form.append("file", new Blob([bytes]), input.file_name)
        const p = await request(path.slice(4), { method: "POST", headers: await signed("POST", path), body: form })
        return { id: p.id, name: p.name, taken: p.taken, width: p.width, height: p.height, content_type: p.contentType, size: p.size }
      },
    },

    share_photo: {
      description: "Make a link that shows a photo to anyone who has it.",
      annotations: { destructiveHint: false },
      inputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["photo"],
        properties: { photo: { type: "string", description: "Photo id, such as ph-001." } },
      },
      outputSchema: {
        type: "object",
        additionalProperties: false,
        required: ["url"],
        properties: { url: { type: "string" } },
      },
      run: async (input) => {
        const path = `/api/photos/${encodeURIComponent(input.photo)}/share`
        const body = await request(path.slice(4), { method: "POST", headers: await signed("POST", path) })
        return { url: body.url }
      },
    },
  },
})
