import { defineProvider } from "harbor"
import { responses, type Request } from "./shared/responses"

// A server with OpenAI's Responses API, such as vLLM or llama-swap in front
// of it. Errors name it by its host.
export default defineProvider({
  name: "openai-compatible",
  title: "OpenAI-compatible server",
  description: "A model on another server with OpenAI's Responses API, such as vLLM.",
  settings: {
    baseUrl: { title: "Base URL", default: "http://localhost:8000/v1" },
    model: { title: "Model", description: "The name the server knows it by, such as Qwen/Qwen3-8B." },
    apiKey: {
      title: "API key",
      description: "Only for a server that asks for one.",
      secret: true,
      optional: true,
    },
    images: {
      title: "Sees images",
      description: 'Set to "no" for a model that cannot take images. Screenshots are then left out, with a note saying so.',
      default: "yes",
    },
  },

  async *generate(request: Request, { settings }) {
    yield* responses(request, {
      baseUrl: settings.baseUrl,
      name: settings.baseUrl.replace(/^\w+:\/\//, "").split("/")[0],
      apiKey: settings.apiKey,
      model: settings.model,
      images: settings.images.trim().toLowerCase() !== "no",
    })
  },
})
