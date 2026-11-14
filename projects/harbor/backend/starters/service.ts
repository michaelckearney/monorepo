import { defineService } from "harbor"

// A service connects Harbor to one website. Each tool is an MCP tool: its
// run function runs inside the site, in Harbor's browser, signed in as you.
export default defineService({
  name: "example",
  title: "Example",
  description: "What this service does.",
  url: "https://example.com",

  tools: {
    get_title: {
      description: "Read the title of the home page.",
      annotations: { readOnlyHint: true },
      outputSchema: {
        type: "object",
        properties: { title: { type: "string" } },
      },
      async run() {
        const html = await (await fetch("/")).text()
        const doc = new DOMParser().parseFromString(html, "text/html")
        return { title: doc.title }
      },
    },
  },
})
