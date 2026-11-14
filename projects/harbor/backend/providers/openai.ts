import { defineProvider } from "harbor"
import { responses, type Request } from "./shared/responses"

export default defineProvider({
  name: "openai",
  title: "OpenAI",
  description: "OpenAI's models, through the Responses API.",
  settings: {
    apiKey: {
      title: "API key",
      description: "Make one at https://platform.openai.com/api-keys.",
      secret: true,
    },
    model: { title: "Model", default: "gpt-5" },
  },

  async *generate(request: Request, { settings }) {
    yield* responses(request, {
      baseUrl: "https://api.openai.com/v1",
      name: "OpenAI",
      apiKey: settings.apiKey,
      model: settings.model,
      images: true,
    })
  },
})
