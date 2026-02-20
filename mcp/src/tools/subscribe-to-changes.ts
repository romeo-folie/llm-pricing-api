import { Resolver } from "dns/promises";
import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { ApiClient, ApiError } from "../api-client.js";
import { formatMcpError, validationError } from "../errors.js";

/** Returns true for RFC 1918, loopback, and link-local addresses (IPv4 and IPv6). Fails closed on resolution errors. */
async function isPrivateOrLoopback(hostname: string): Promise<boolean> {
  const isPrivateV4 = (ip: string): boolean => {
    const parts = ip.split(".").map(Number);
    return (
      parts[0] === 127 || // 127.0.0.0/8 — full loopback range
      parts[0] === 10 || // 10.0.0.0/8
      (parts[0] === 172 && parts[1] >= 16 && parts[1] <= 31) || // 172.16.0.0/12
      (parts[0] === 192 && parts[1] === 168) || // 192.168.0.0/16
      ip.startsWith("169.254.") // 169.254.0.0/16 link-local
    );
  };
  const isPrivateV6 = (ip: string): boolean => {
    const lower = ip.toLowerCase();
    return (
      lower === "::1" || // IPv6 loopback
      lower.startsWith("fc") || // fc00::/7 ULA
      lower.startsWith("fd") || // fd00::/8 ULA
      lower.startsWith("fe80:") // fe80::/10 link-local
    );
  };
  try {
    const resolver = new Resolver();
    const [v4Result, v6Result] = await Promise.allSettled([
      resolver.resolve4(hostname),
      resolver.resolve6(hostname),
    ]);
    const v4addrs = v4Result.status === "fulfilled" ? v4Result.value : [];
    const v6addrs = v6Result.status === "fulfilled" ? v6Result.value : [];
    // Fail closed: unresolvable on both protocols is rejected
    if (v4addrs.length === 0 && v6addrs.length === 0) return true;
    return v4addrs.some(isPrivateV4) || v6addrs.some(isPrivateV6);
  } catch {
    return true; // fail closed
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
