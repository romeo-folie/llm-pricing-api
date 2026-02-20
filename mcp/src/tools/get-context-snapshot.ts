import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { ApiClient, ApiError } from "../api-client.js";
import { formatMcpError } from "../errors.js";

export const definition = {
  name: "get_context_snapshot",
  description:
    "Get a ~2k-token pricing snapshot optimised for agent system prompts. Use format=markdown for a human-readable table. Requires Developer tier.",
  inputSchema: {
    type: "object" as const,
    properties: {
      format: {
        type: "string",
        description: '"json" (default) or "markdown"',
        enum: ["json", "markdown"],
      },
    },
    required: [],
  },
};

export async function handler(
  args: Record<string, unknown>,
  client: ApiClient
): Promise<CallToolResult> {
  const format = args.format as string | undefined;

  try {
    const result = await client.get("/v1/context", {
      params: { format },
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
