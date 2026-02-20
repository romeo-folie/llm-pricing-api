import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { ApiClient, ApiError } from "../api-client.js";
import { formatMcpError } from "../errors.js";

export const definition = {
  name: "get_recent_changes",
  description:
    "Get recent LLM pricing changes across all providers or filtered by provider and time range.",
  inputSchema: {
    type: "object" as const,
    properties: {
      since: {
        type: "string",
        description: "ISO 8601 datetime — only return changes after this time",
      },
      provider: {
        type: "string",
        description: 'Filter by provider name (e.g., "openai", "anthropic")',
      },
    },
    required: [],
  },
};

export async function handler(
  args: Record<string, unknown>,
  client: ApiClient
): Promise<CallToolResult> {
  const since = args.since as string | undefined;
  const provider = args.provider as string | undefined;

  try {
    const result = await client.get("/v1/changes", {
      params: { since, provider },
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
