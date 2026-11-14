// OpenAI's Responses API, streamed, for the providers that use it: OpenAI,
// and another server with the same API, such as vLLM. Tool results go back
// as JSON: { output } when a tool worked, { error } when it did not, with any
// images the tool returned beside it.

interface Part {
  text?: string
  toolCall?: { id: string; name: string; input: Record<string, unknown> }
  toolResult?: { id: string; output?: unknown; error?: string; images?: { mimeType: string; data: string }[] }
}
interface Message {
  role: "user" | "assistant" | "tool"
  parts: Part[]
}
interface Tool {
  name: string
  description: string
  inputSchema: Record<string, unknown>
}

function input(messages: Message[], images: boolean) {
  const items: Record<string, unknown>[] = []
  for (const message of messages) {
    // Text in one message is sent as one message, ahead of its tool calls.
    const text = message.parts.map((p) => p.text ?? "").filter((t) => t.trim() !== "")
    if (text.length > 0) {
      items.push({ role: message.role === "assistant" ? "assistant" : "user", content: text.join("\n\n") })
    }
    for (const { toolCall, toolResult } of message.parts) {
      if (toolCall) {
        items.push({ type: "function_call", call_id: toolCall.id, name: toolCall.name, arguments: JSON.stringify(toolCall.input) })
      }
      if (toolResult) {
        const shown = toolResult.images?.length ?? 0
        const result = JSON.stringify({
          ...(toolResult.error === undefined ? { output: toolResult.output } : { error: toolResult.error }),
          // A server that cannot take images refuses the whole request.
          ...(!images && shown > 0 && { note: `The tool returned ${shown} image(s), left out: this model cannot see images.` }),
        })
        // A picture goes with the result as an image the model can see.
        const output = images && shown > 0
          ? [
              { type: "input_text", text: result },
              ...toolResult.images.map((i) => ({ type: "input_image", image_url: `data:${i.mimeType};base64,${i.data}` })),
            ]
          : result
        items.push({ type: "function_call_output", call_id: toolResult.id, output })
      }
    }
  }
  return items
}

// Server-sent events, as they stream in: each event's data, parsed.
async function* events(body: { getReader(): { read(): Promise<{ done: boolean; value?: Uint8Array }> } }) {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""
  for (;;) {
    const { done, value } = await reader.read()
    buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
    const blocks = buffer.split(/\r?\n\r?\n/)
    buffer = done ? "" : blocks.pop()!
    for (const block of blocks) {
      const data = block
        .split(/\r?\n/)
        .filter((line) => line.startsWith("data:"))
        .map((line) => line.slice(5).trimStart())
        .join("\n")
      if (data !== "" && data !== "[DONE]") {
        yield JSON.parse(data)
      }
    }
    if (done) {
      return
    }
  }
}

export interface Request {
  instructions: string
  messages: Message[]
  tools: Tool[]
}

export interface Server {
  /** Where the API is, such as https://api.openai.com/v1. */
  baseUrl: string
  /** What errors call the server. */
  name: string
  apiKey?: string
  model: string
  /** false for a model that cannot take images. */
  images: boolean
}

export async function* responses(request: Request, server: Server) {
  const response = await fetch(`${server.baseUrl.replace(/\/+$/, "")}/responses`, {
    method: "POST",
    headers: {
      ...(server.apiKey && { Authorization: `Bearer ${server.apiKey}` }),
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      model: server.model,
      instructions: request.instructions,
      input: input(request.messages, server.images),
      tools: request.tools.map((t) => ({
        type: "function",
        name: t.name,
        description: t.description,
        parameters: t.inputSchema,
        // Not strict, for now: MCP, whose tools these are, has no strict
        // mode, so it would be a layer on top for each provider. And on
        // vLLM it did not hold: a strict report tool still came back from
        // Qwen with lists as strings. Go checks what a call sends instead.
        strict: false,
      })),
      stream: true,
    }),
  })
  if (!response.ok) {
    const text = await response.text()
    let message = text
    try {
      message = JSON.parse(text).error.message
    } catch {
      // Not OpenAI's error format: show it as it is.
    }
    throw new Error(`${server.name} said ${response.status} ${response.statusText}: ${message}`)
  }

  for await (const event of events(response.body)) {
    switch (event.type) {
      case "response.output_text.delta":
      case "response.refusal.delta":
        yield { text: event.delta }
        break
      case "response.output_item.done":
        if (event.item.type === "function_call") {
          const { call_id, name, arguments: args } = event.item
          yield { toolCall: { id: call_id, name, input: args ? JSON.parse(args) : {} } }
        }
        break
      case "response.completed":
        yield { done: true, usage: event.response.usage }
        return
      case "response.incomplete":
        throw new Error(`${server.name} stopped the reply early: ${event.response.incomplete_details?.reason ?? "no reason given"}`)
      case "response.failed":
        throw new Error(`${server.name} could not reply: ${event.response.error?.message ?? "no reason given"}`)
      case "error":
        throw new Error(`${server.name} could not reply: ${event.message}`)
    }
  }
  throw new Error(`${server.name}'s reply ended before it was complete`)
}
