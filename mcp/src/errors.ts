export type ErrorKind = "auth" | "rate_limit" | "server" | "network" | "validation";

export interface ClassifiedError {
  kind: ErrorKind;
  message: string;
  statusCode?: number;
}

export function classifyHttpError(status: number, body: string, baseUrl: string): ClassifiedError {
  if (status === 401) {
    return {
      kind: "auth",
      message:
        "The LLM Rates API rejected this API key (missing, invalid, or revoked). " +
        "Call the `authenticate` tool to get a new one, or set LLMRATES_API_KEY in your MCP config.",
      statusCode: status,
    };
  }
  if (status === 403) {
    // The API has no tier gating, so a 403 is not a plan problem. Report it as
    // what it is rather than inventing an upgrade path that does not exist.
    return {
      kind: "auth",
      message: `The API refused this request (403)${detailSuffix(body)}`,
      statusCode: status,
    };
  }
  if (status === 429) {
    return {
      kind: "rate_limit",
      message: "Rate limit exceeded. Wait a moment before retrying.",
      statusCode: status,
    };
  }
  if (status >= 500) {
    return {
      kind: "server",
      message: `LLM Rates API server error (${status}). Try again shortly.`,
      statusCode: status,
    };
  }
  return {
    kind: "server",
    message: `Unexpected API response (${status})${detailSuffix(body)}`,
    statusCode: status,
  };
}

/**
 * Returned when no API key is configured at all.
 *
 * The wording is deliberate: the remedy is a tool call the agent can make on its
 * own, not a config edit the user has to perform by hand.
 */
export function missingApiKeyError(): ClassifiedError {
  return {
    kind: "auth",
    message:
      "No LLM Rates API key is configured. Call the `authenticate` tool: it returns a short code " +
      "and a URL for the user to approve, then stores the key locally so every tool works.",
  };
}

export function networkError(baseUrl: string): ClassifiedError {
  return {
    kind: "network",
    message: `Could not reach the LLM Rates API at ${baseUrl}. Check your network.`,
  };
}

export function validationError(message: string): ClassifiedError {
  return { kind: "validation", message };
}

export function formatMcpError(err: ClassifiedError): string {
  return err.message;
}

/** Pulls the RFC 7807 `detail` field out of an error body, when present. */
function detailSuffix(body: string): string {
  try {
    const parsed = JSON.parse(body) as { detail?: unknown };
    if (typeof parsed.detail === "string" && parsed.detail !== "") return `: ${parsed.detail}`;
  } catch {
    // Non-JSON error body; nothing to add.
  }
  return ".";
}
