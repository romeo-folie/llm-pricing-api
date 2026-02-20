import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { ApiClient, ApiError } from "../api-client.js";
import { formatMcpError } from "../errors.js";

export const definition = {
  name: "get_cheapest_model",
  description:
    "Find the cheapest LLM model matching your task requirements. Returns a ranked list with pricing and trust metadata. Requires Developer tier.",
  inputSchema: {
    type: "object" as const,
    properties: {
      task: {
        type: "string",
        description: 'Task type description (e.g., "code generation", "summarization")',
      },
      min_context: {
        type: "number",
        description: "Minimum context window in tokens",
      },
      max_input_price: {
        type: "number",
        description: "Maximum price per 1M input tokens in USD",
      },
      modality: {
        type: "string",
        description: 'Input modality: "text", "vision", or "audio"',
        enum: ["text", "vision", "audio"],
      },
    },
    required: ["task"],
  },
};

export async function handler(
  args: Record<string, unknown>,
  client: ApiClient
): Promise<CallToolResult> {
  const task = args.task as string;
  const min_context = args.min_context as number | undefined;
  const max_input_price = args.max_input_price as number | undefined;
  const modality = args.modality as string | undefined;

  try {
    const result = await client.get("/v1/recommend", {
      params: { task, min_context, max_input_price, modality },
    });
    return {
      content: [{ type: "text", text: JSON.stringify(result, null, 2) }],
    };
  } catch (err) {
    if (err instanceof ApiError) {
      return {
        content: [{ type: "text", text: formatMcpError(err.classified) }],
        isError: true,
      };
    }
    throw err;
  }
}
