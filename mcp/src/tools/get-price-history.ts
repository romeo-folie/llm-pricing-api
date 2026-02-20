import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { ApiClient, ApiError } from "../api-client.js";
import { formatMcpError } from "../errors.js";

export const definition = {
  name: "get_price_history",
  description:
    "Retrieve the full pricing history for a specific model. Returns timestamped price records with source attribution. Requires Developer tier.",
  inputSchema: {
    type: "object" as const,
    properties: {
      model_id: {
        type: "string",
        description: "The model identifier",
      },
      from: {
        type: "string",
        description: 'Start date in ISO 8601 format (e.g., "2025-01-01T00:00:00Z")',
      },
      to: {
        type: "string",
        description: "End date in ISO 8601 format",
      },
    },
    required: ["model_id"],
  },
};

export async function handler(
  args: Record<string, unknown>,
  client: ApiClient
): Promise<CallToolResult> {
  const model_id = args.model_id as string;
  const from = args.from as string | undefined;
  const to = args.to as string | undefined;

  try {
    const result = await client.get(`/v1/models/${model_id}/history`, {
      params: { from, to },
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
