export type ErrorKind = "auth" | "tier" | "rate_limit" | "server" | "network" | "validation";

export interface ClassifiedError {
  kind: ErrorKind;
  message: string;
  statusCode?: number;
}

export function classifyHttpError(status: number, body: string, baseUrl: string): ClassifiedError {
  if (status === 401) return { kind: "auth", message: "Authentication failed. Set LLMRATES_API_KEY in your MCP config.", statusCode: status };
  if (status === 403) {
    // Parse tier info from body if available
    let requiredTier = "Developer";
    let currentTier = "Free";
    try {
      const parsed = JSON.parse(body);
      if (parsed.required_tier) requiredTier = parsed.required_tier;
      if (parsed.current_tier) currentTier = parsed.current_tier;
    } catch {}
    return { kind: "tier", message: `This tool requires ${requiredTier} tier. Current tier: ${currentTier}.`, statusCode: status };
  }
  if (status === 429) return { kind: "rate_limit", message: "Rate limit exceeded. Upgrade your plan or wait before retrying.", statusCode: status };
  if (status >= 500) return { kind: "server", message: `LLM Rates API server error (${status}). Try again shortly.`, statusCode: status };
  return { kind: "server", message: `Unexpected API response (${status}).`, statusCode: status };
}

export function networkError(baseUrl: string): ClassifiedError {
  return { kind: "network", message: `Could not reach the LLM Rates API at ${baseUrl}. Check your network.` };
}

export function validationError(message: string): ClassifiedError {
  return { kind: "validation", message };
}

export function formatMcpError(err: ClassifiedError): string {
  return err.message;
}
