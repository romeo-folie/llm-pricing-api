import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { ApiClient, ApiError } from "../api-client.js";
import { formatMcpError, validationError } from "../errors.js";

export const definition = {
  name: "subscribe_to_changes",
  description:
    "Register a webhook URL to receive real-time notifications when LLM prices change. Requires Pro tier.",
  inputSchema: {
    type: "object" as const,
    properties: {
      url: {
        type: "string",
        description: "HTTPS webhook URL to receive events",
      },
      events: {
        type: "array",
        items: {
          type: "string",
          enum: ["price_change", "model_added", "model_deprecated"],
        },
        description:
          'Event types to subscribe to (default: all). Valid values: "price_change", "model_added", "model_deprecated"',
      },
    },
    required: ["url"],
  },
};

export async function handler(
  args: Record<string, unknown>,
  client: ApiClient
): Promise<CallToolResult> {
  const url = args.url as string;
  const events = args.events as string[] | undefined;

  if (!url.startsWith("https://")) {
    const err = validationError("Webhook URL must use HTTPS");
    return {
      content: [{ type: "text", text: formatMcpError(err) }],
      isError: true,
    };
  }

  try {
    const result = await client.post("/v1/webhooks", { url, events });
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
