import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { ApiClient, ApiError } from "../api-client.js";
import { formatMcpError, validationError } from "../errors.js";

export const definition = {
  name: "compare_models",
  description:
    "Compare up to 5 LLM models side-by-side with current pricing and trust metadata.",
  inputSchema: {
    type: "object" as const,
    properties: {
      models: {
        type: "array",
        items: { type: "string" },
        description: "Model IDs to compare (2–5 models)",
        minItems: 2,
        maxItems: 5,
      },
    },
    required: ["models"],
  },
};

export async function handler(
  args: Record<string, unknown>,
  client: ApiClient
): Promise<CallToolResult> {
  const models = args.models as string[];

  if (!Array.isArray(models) || models.length < 2 || models.length > 5) {
    const err = validationError("compare_models requires between 2 and 5 model IDs");
    return {
      content: [{ type: "text", text: formatMcpError(err) }],
      isError: true,
    };
  }

  try {
    const result = await client.get("/v1/compare", {
      params: { models: models.join(",") },
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
