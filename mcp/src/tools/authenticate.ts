import type { CallToolResult } from "@modelcontextprotocol/sdk/types.js";
import { ApiClient } from "../api-client.js";
import {
  clearPendingGrant,
  readPendingGrant,
  writeApiKey,
  writePendingGrant,
  type PendingGrant,
} from "../config-store.js";
import { DeviceFlowError, pollGrant, sleep, startGrant } from "../device-flow.js";

/**
 * `authenticate` obtains an API key for the user without either party handling
 * a secret by hand.
 *
 * It is intentionally two-phase, because a single blocking call cannot work:
 * the URL has to reach the user *before* they can approve, but a tool result is
 * not delivered until the call returns. So:
 *
 *   call 1 (no pending grant) → starts a grant and returns the URL + code now
 *   user                      → opens the URL and approves in a browser
 *   call 2 (pending grant)    → polls, collects the key, stores it
 *
 * Call 2 also waits briefly, so if the user approves during the first call the
 * flow still completes in two calls rather than needing a third.
 */

const DEFAULT_WAIT_SECONDS = 90;
const MAX_WAIT_SECONDS = 300;

export const definition = {
  name: "authenticate",
  description:
    "Get an LLM Rates API key for the user, or replace a missing/revoked one. " +
    "Returns a short code and a URL; show both to the user and ask them to open the URL and approve. " +
    "Then call this tool again to collect the key, which is stored locally so every other tool works. " +
    "Call this when a tool reports that no API key is configured, or when the API rejects the current key.",
  inputSchema: {
    type: "object" as const,
    properties: {
      client_name: {
        type: "string",
        description:
          "How to identify this client on the user's approval screen (for example \"Claude Code\"). " +
          "Shown verbatim to the user, so keep it short and recognisable. Defaults to LLMRATES_CLIENT_NAME or \"MCP client\".",
      },
      wait_seconds: {
        type: "number",
        description:
          "How long to wait for approval before returning the prompt again. Defaults to 90, maximum 300.",
      },
    },
    required: [],
  },
};

export async function handler(
  args: Record<string, unknown>,
  client: ApiClient
): Promise<CallToolResult> {
  const baseUrl = client.getBaseUrl();
  const clientName =
    (typeof args.client_name === "string" && args.client_name.trim()) ||
    process.env.LLMRATES_CLIENT_NAME?.trim() ||
    "MCP client";
  const waitSeconds = clamp(
    typeof args.wait_seconds === "number" && Number.isFinite(args.wait_seconds)
      ? args.wait_seconds
      : DEFAULT_WAIT_SECONDS,
    1,
    MAX_WAIT_SECONDS
  );

  const existing = await readPendingGrant();
  const usable = existing && existing.baseUrl === baseUrl && !isExpired(existing);

  if (!usable) {
    if (existing) {
      // Stale state is worse than none: it would make a later call poll a code
      // the API has already forgotten.
      await clearPendingGrant();
    }
    return startPhase(baseUrl, clientName);
  }

  return awaitCompletePhase(baseUrl, existing, waitSeconds, client);
}

// ── Phase 1: start a grant and hand the user the prompt ───────────────────────

async function startPhase(baseUrl: string, clientName: string): Promise<CallToolResult> {
  let started;
  try {
    started = await startGrant({
      baseUrl,
      clientName,
      platform: `${process.platform}-${process.arch}`,
    });
  } catch (err) {
    const message =
      err instanceof DeviceFlowError
        ? err.message
        : `Could not start authorization: ${err instanceof Error ? err.message : String(err)}`;
    return { content: [{ type: "text", text: message }], isError: true };
  }

  const grant: PendingGrant = {
    deviceCode: started.deviceCode,
    userCode: started.userCode,
    verificationUri: started.verificationUri,
    verificationUriComplete: started.verificationUriComplete,
    expiresAt: started.expiresAt,
    baseUrl,
  };
  await writePendingGrant(grant);

  // Mirror the prompt to stderr as well: a host that surfaces server logs shows
  // it immediately, which beats waiting for the tool result in some clients.
  console.error(`[llmrates] Authorize at ${started.verificationUriComplete}`);
  console.error(`[llmrates] Or open ${started.verificationUri} and enter code ${started.userCode}`);

  return {
    content: [{ type: "text", text: promptText(grant) }],
  };
}

// ── Phase 2: wait for the decision and collect the key ────────────────────────

async function awaitCompletePhase(
  baseUrl: string,
  grant: PendingGrant,
  waitSeconds: number,
  client: ApiClient
): Promise<CallToolResult> {
  const deadline = Math.min(Date.now() + waitSeconds * 1000, Date.parse(grant.expiresAt));
  let intervalMs = 2000;
  let firstPoll = true;

  while (Date.now() < deadline) {
    // Poll once immediately: by the time this phase runs the user has usually
    // already approved, and making them wait a full interval for news they
    // already know is the kind of delay that reads as a hang.
    if (!firstPoll) await sleep(intervalMs);
    firstPoll = false;

    const outcome = await pollGrant({ baseUrl, deviceCode: grant.deviceCode });

    switch (outcome.status) {
      case "issued": {
        // Install it in-process first, so the key is usable for the rest of this
        // session even if persisting it fails.
        client.setApiKey(outcome.apiKey);

        let storedPath: string;
        try {
          storedPath = await writeApiKey(outcome.apiKey);
        } catch (err) {
          // The grant is already consumed and the plaintext cannot be fetched
          // again, so this is not retryable with the same grant. Say that
          // plainly and name the fix, rather than letting the exception escape
          // as an opaque failure the user cannot act on. The key itself is never
          // included in the message.
          await clearPendingGrant();
          return {
            content: [
              {
                type: "text",
                text:
                  `The key was issued but could not be saved: ${err instanceof Error ? err.message : String(err)}\n\n` +
                  "It is active and usable for this session only. Fix the directory, then run " +
                  "`authenticate` again to store a key permanently.",
              },
            ],
            isError: true,
          };
        }

        await clearPendingGrant();
        return {
          content: [{ type: "text", text: successText(storedPath, outcome.email, outcome.label) }],
        };
      }

      case "pending":
        continue;

      case "slow_down":
        intervalMs = Math.min(30000, Math.max(intervalMs, (outcome.intervalSeconds ?? 10) * 1000));
        continue;

      case "denied":
        await clearPendingGrant();
        return {
          content: [
            {
              type: "text",
              text:
                "The user denied the authorization request, so no key was issued and nothing was " +
                "shared. Do not retry unless the user asks you to.",
            },
          ],
          isError: true,
        };

      case "expired":
        await clearPendingGrant();
        return {
          content: [
            {
              type: "text",
              text:
                "The authorization code expired before it was approved. Call `authenticate` again " +
                "to get a fresh code, and approve it promptly.",
            },
          ],
          isError: true,
        };

      case "redeemed":
        // Another client already collected this grant. Not an error worth
        // alarming the user about; just start over.
        await clearPendingGrant();
        return {
          content: [
            {
              type: "text",
              text:
                "This authorization was already used. Call `authenticate` again to start a new one.",
            },
          ],
          isError: true,
        };

      case "unknown":
        await clearPendingGrant();
        return {
          content: [
            {
              type: "text",
              text:
                "The server no longer recognises this authorization, so it most likely expired. " +
                "Call `authenticate` again to start a new one.",
            },
          ],
          isError: true,
        };

      case "key_limit":
        await clearPendingGrant();
        return {
          content: [
            {
              type: "text",
              text:
                `${outcome.message} The user can review and revoke keys at ` +
                `${webBaseUrl(baseUrl)}/signup/free, then run \`authenticate\` again.`,
            },
          ],
          isError: true,
        };

      case "error":
        if (!outcome.retryable) {
          await clearPendingGrant();
          return { content: [{ type: "text", text: outcome.message }], isError: true };
        }
        // Transient: keep polling until the deadline.
        continue;
    }
  }

  // Still pending: hand the prompt back rather than failing, so the agent can
  // relay it and try again once the user has acted.
  return {
    content: [
      {
        type: "text",
        text:
          `${promptText(grant)}\n\n` +
          "The request is still waiting for approval. Show the user the URL and code above, then " +
          "call `authenticate` again once they say they have approved it.",
      },
    ],
  };
}

// ── Copy ──────────────────────────────────────────────────────────────────────

function promptText(grant: PendingGrant): string {
  const expiresInMinutes = Math.max(1, Math.round((Date.parse(grant.expiresAt) - Date.now()) / 60000));
  return [
    "An API key needs to be approved by the user. Ask them to do this now:",
    "",
    `  1. Open ${grant.verificationUriComplete}`,
    `     (or ${grant.verificationUri} and enter code ${grant.userCode})`,
    `  2. Check the code on that page matches: ${grant.userCode}`,
    "  3. Choose Approve",
    "",
    `The code expires in about ${expiresInMinutes} minute(s). The key is delivered straight to this ` +
      "client and never has to be copied or pasted. Tell the user not to approve a code they did " +
      "not just see here.",
  ].join("\n");
}

function successText(path: string, email?: string, label?: string): string {
  const lines = [
    "Authenticated. The API key has been stored and every tool will now work.",
    "",
    `  Stored at: ${path}`,
    email ? `  Account:   ${email}` : null,
    label ? `  Authorized for: ${label}` : null,
    "",
  ].filter((line): line is string => line !== null);

  if (process.env.LLMRATES_API_KEY?.trim()) {
    lines.push(
      "Note: LLMRATES_API_KEY is set in this environment and takes precedence over the stored key. " +
        "Unset it if you want the newly issued key to be used."
    );
  }

  return lines.join("\n");
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function isExpired(grant: PendingGrant): boolean {
  const expiresAt = Date.parse(grant.expiresAt);
  return !Number.isFinite(expiresAt) || expiresAt <= Date.now();
}

/**
 * Maps an API base URL to the human-facing site.
 *
 * The API lives at api.llmrates.live and the app at llmrates.live. Kept as a
 * helper so a message cannot silently point users at a route that does not
 * exist.
 */
function webBaseUrl(apiBaseUrl: string): string {
  return apiBaseUrl.replace("//api.", "//").replace(/\/$/, "");
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
