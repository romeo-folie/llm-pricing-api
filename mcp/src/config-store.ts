/**
 * Local credential and pending-grant storage for the MCP server.
 *
 * Everything lives in one config directory so the trust boundary is a single
 * path the operator can inspect:
 *
 *   <configDir>/credentials.json    the API key (0600)
 *   <configDir>/pending-grant.json  an in-flight device grant (0600)
 *
 * Directory resolution, highest priority first:
 *   1. LLMRATES_CONFIG_DIR   explicit override (tests, CI, containers)
 *   2. XDG_CONFIG_HOME/llmrates
 *   3. ~/.config/llmrates
 *
 * Secrets are never written anywhere else: not to the project tree, not to
 * logs, not to stdout.
 */

import { promises as fs, constants as fsConstants } from "fs";
import { randomUUID } from "crypto";
import os from "os";
import path from "path";

export type CredentialSource = "env" | "file" | "none";

export interface ResolvedCredential {
  apiKey: string | null;
  source: CredentialSource;
  /** Populated only when source is "file". */
  path: string | null;
}

/** An in-flight device grant, persisted so a second tool call can resume it. */
export interface PendingGrant {
  deviceCode: string;
  userCode: string;
  verificationUri: string;
  verificationUriComplete: string;
  /** ISO 8601 expiry, as reported by the API. */
  expiresAt: string;
  baseUrl: string;
}

const CREDENTIALS_FILE = "credentials.json";
const PENDING_GRANT_FILE = "pending-grant.json";

/**
 * Directory holding the credentials file and the in-flight grant.
 *
 * Defaults to `~/.llmrates` per the decision recorded on issue #191. Only the
 * explicit `LLMRATES_CONFIG_DIR` override changes it, so the location is
 * predictable rather than depending on ambient XDG settings.
 */
export function configDir(): string {
  const explicit = process.env.LLMRATES_CONFIG_DIR?.trim();
  if (explicit) return explicit;

  return path.join(os.homedir(), ".llmrates");
}

export function credentialsPath(): string {
  return path.join(configDir(), CREDENTIALS_FILE);
}

export function pendingGrantPath(): string {
  return path.join(configDir(), PENDING_GRANT_FILE);
}

// ── Credentials ───────────────────────────────────────────────────────────────

/**
 * Resolve the API key, preferring the environment.
 *
 * The env var wins so an operator can override a stored key without deleting
 * it, and so CI can inject one without touching the filesystem.
 */
export async function resolveApiKey(): Promise<ResolvedCredential> {
  const envKey = process.env.LLMRATES_API_KEY?.trim();
  if (envKey) return { apiKey: envKey, source: "env", path: null };

  const stored = await readJson<{ api_key?: unknown }>(credentialsPath());
  const fileKey = typeof stored?.api_key === "string" ? stored.api_key.trim() : "";
  if (fileKey) return { apiKey: fileKey, source: "file", path: credentialsPath() };

  return { apiKey: null, source: "none", path: null };
}

/** Persist the API key at 0600, creating the config directory at 0700. */
export async function writeApiKey(apiKey: string): Promise<string> {
  const target = credentialsPath();
  await writeSecretJson(target, {
    api_key: apiKey,
    saved_at: new Date().toISOString(),
  });
  return target;
}

// ── Pending grant ─────────────────────────────────────────────────────────────

export async function readPendingGrant(): Promise<PendingGrant | null> {
  const raw = await readJson<Partial<PendingGrant>>(pendingGrantPath());
  if (
    !raw ||
    typeof raw.deviceCode !== "string" ||
    typeof raw.userCode !== "string" ||
    typeof raw.expiresAt !== "string" ||
    typeof raw.baseUrl !== "string"
  ) {
    return null;
  }
  return {
    deviceCode: raw.deviceCode,
    userCode: raw.userCode,
    verificationUri: typeof raw.verificationUri === "string" ? raw.verificationUri : "",
    verificationUriComplete:
      typeof raw.verificationUriComplete === "string" ? raw.verificationUriComplete : "",
    expiresAt: raw.expiresAt,
    baseUrl: raw.baseUrl,
  };
}

export async function writePendingGrant(grant: PendingGrant): Promise<void> {
  await writeSecretJson(pendingGrantPath(), grant);
}

/** Remove the pending grant. Missing file is not an error. */
export async function clearPendingGrant(): Promise<void> {
  try {
    await fs.unlink(pendingGrantPath());
  } catch {
    // Already gone.
  }
}

// ── Internals ─────────────────────────────────────────────────────────────────

async function readJson<T>(file: string): Promise<T | null> {
  try {
    const raw = await fs.readFile(file, "utf8");
    return JSON.parse(raw) as T;
  } catch {
    // Missing, unreadable, or malformed all mean "nothing usable here"; the
    // caller falls back rather than failing, since the remedy in every case is
    // to authenticate again.
    return null;
  }
}

/**
 * Refuses to write a secret into a directory other users can reach.
 *
 * The file itself is 0600 either way, but a group- or world-accessible directory
 * lets another local user replace the file (or a path component) so this process
 * reads its "credentials" from an attacker-controlled file.
 *
 * This refuses rather than silently chmod-ing: the directory may be the
 * operator's deliberate choice, and quietly tightening someone's filesystem is
 * the kind of surprise that is worse than a clear error. The message names the
 * fix so it is actionable.
 */
async function assertDirectoryIsPrivate(dir: string): Promise<void> {
  const info = await fs.stat(dir);
  const mode = info.mode & 0o777;
  if ((mode & 0o077) !== 0) {
    throw new Error(
      `refusing to write credentials: ${dir} is mode ${mode.toString(8)}, which is ` +
        `accessible to other users. Restrict it first, for example: chmod 700 ${dir}`
    );
  }
}

/**
 * Write JSON to a secret file atomically.
 *
 * Three properties matter here:
 *
 *   - The mode is set at open time, so the secret is never briefly readable by
 *     other users. The write goes to a temp file in the same directory and is
 *     then renamed over the target, because rename is atomic on POSIX. A crash
 *     mid-write therefore leaves either the old file or the new one, never a
 *     truncated file that would read as "no key" and silently re-trigger the
 *     device flow.
 *   - The temp name is random and opened O_EXCL, so the write cannot be
 *     redirected through a pre-planted symlink or truncate an existing file.
 *     O_NOFOLLOW is ORed in where the platform defines it.
 *   - The containing directory must not be group- or world-accessible; see
 *     assertDirectoryIsPrivate.
 */
async function writeSecretJson(target: string, value: unknown): Promise<void> {
  const dir = path.dirname(target);
  await fs.mkdir(dir, { recursive: true, mode: 0o700 });
  await assertDirectoryIsPrivate(dir);

  const tmp = path.join(dir, `.${path.basename(target)}.${randomUUID()}.tmp`);
  const flags =
    fsConstants.O_CREAT | fsConstants.O_EXCL | fsConstants.O_WRONLY | (fsConstants.O_NOFOLLOW ?? 0);

  const handle = await fs.open(tmp, flags, 0o600);
  try {
    await handle.writeFile(`${JSON.stringify(value, null, 2)}\n`, "utf8");
    // Explicit chmod guards against a permissive umask on an exotic platform.
    await handle.chmod(0o600);
    await handle.sync();
  } finally {
    await handle.close();
  }

  try {
    await fs.rename(tmp, target);
    await fsyncDir(dir);
  } catch (err) {
    await fs.unlink(tmp).catch(() => undefined);
    throw err;
  }
}

/**
 * fsync a directory so a rename survives a crash.
 *
 * Best-effort by design: opening a directory for reading is unsupported on some
 * platforms (notably Windows). The rename is still atomic there; only its
 * durability is weaker, which is not worth failing the write over.
 */
async function fsyncDir(dir: string): Promise<void> {
  try {
    const handle = await fs.open(dir, fsConstants.O_RDONLY);
    try {
      await handle.sync();
    } finally {
      await handle.close();
    }
  } catch {
    // Unsupported on this platform; the rename already happened.
  }
}
