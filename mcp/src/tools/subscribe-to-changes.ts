import { Resolver } from "dns/promises";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { ApiClient, ApiError } from "../api-client.js";
import { formatMcpError, validationError } from "../errors.js";

/** Returns true for RFC 1918, loopback, and link-local addresses. Fails closed on resolution errors. */
async function isPrivateOrLoopback(hostname: string): Promise<boolean> {
  try {
    const resolver = new Resolver();
    const addresses = await resolver.resolve4(hostname);
    return addresses.some((ip) => {
      const parts = ip.split(".").map(Number);
      return (
        ip === "127.0.0.1" ||
        parts[0] === 10 ||
        (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) ||
        (parts[0] === 192 && parts[1] === 168) ||
        ip.startsWith("169.254.")
      );
    });
  } catch {
    return true; // fail closed: unresolvable or NXDOMAIN hosts are rejected
  }
}

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
  const url = args.url;
  if (typeof url !== "string" || url.trim() === "") {
    return {
      content: [{ type: "text", text: "url is required and must be a non-empty string" }],
      isError: true,
    };
  }

  if (!url.startsWith("https://")) {
    const err = validationError("Webhook URL must use HTTPS");
    return {
      content: [{ type: "text", text: formatMcpError(err) }],
      isError: true,
    };
  }

  let parsedUrl: URL;
  try {
    parsedUrl = new URL(url);
  } catch {
    return {
      content: [{ type: "text", text: "Invalid URL format" }],
      isError: true,
    };
  }

  if (await isPrivateOrLoopback(parsedUrl.hostname)) {
    const err = validationError("Webhook URL must not target a private, loopback, or link-local address");
    return {
      content: [{ type: "text", text: formatMcpError(err) }],
      isError: true,
    };
  }

  const events = args.events as string[] | undefined;

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
