import { classifyHttpError, networkError, ClassifiedError } from "./errors.js";

export interface RequestOptions {
  params?: Record<string, string | number | boolean | undefined>;
}

export class ApiError extends Error {
  constructor(
    public readonly classified: ClassifiedError,
    public readonly statusCode?: number
  ) {
    super(classified.message);
    this.name = "ApiError";
  }
}

export class ApiClient {
  private readonly baseUrl: string;
  private readonly apiKey: string;

  constructor(baseUrl: string, apiKey: string) {
    this.baseUrl = baseUrl.replace(/\/$/, ""); // strip trailing slash
    this.apiKey = apiKey;
  }

  private buildUrl(path: string, params?: Record<string, string | number | boolean | undefined>): string {
    const url = new URL(path, this.baseUrl + "/");
    if (params) {
      for (const [k, v] of Object.entries(params)) {
        if (v !== undefined) url.searchParams.set(k, String(v));
      }
    }
    return url.toString();
  }

  async get<T = unknown>(path: string, options?: RequestOptions): Promise<T> {
    const url = this.buildUrl(path, options?.params);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);

    try {
      const response = await fetch(url, {
        method: "GET",
        headers: {
          Authorization: `Bearer ${this.apiKey}`,
          "Content-Type": "application/json",
          Accept: "application/json",
        },
        signal: controller.signal,
      });

      const body = await response.text();

      if (!response.ok) {
        const err = classifyHttpError(response.status, body, this.baseUrl);
        throw new ApiError(err, response.status);
      }

      try {
        return JSON.parse(body) as T;
      } catch {
        throw new ApiError(
          { kind: "server", message: `Unexpected non-JSON response from ${this.baseUrl}` },
          response.status
        );
      }
    } catch (err) {
      if (err instanceof ApiError) throw err;
      if (err instanceof Error && err.name === "AbortError") {
        throw new ApiError(networkError(this.baseUrl));
      }
      throw new ApiError(networkError(this.baseUrl));
    } finally {
      clearTimeout(timeout);
    }
  }

  /** Fetch a plain-text / non-JSON response body. Used for endpoints that
   *  return formats other than JSON (e.g. /v1/context?format=markdown). */
  async getText(path: string, options?: RequestOptions): Promise<string> {
    const url = this.buildUrl(path, options?.params);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);

    try {
      const response = await fetch(url, {
        method: "GET",
        headers: {
          Authorization: `Bearer ${this.apiKey}`,
          Accept: "text/plain, text/markdown",
        },
        signal: controller.signal,
      });

      const body = await response.text();

      if (!response.ok) {
        const err = classifyHttpError(response.status, body, this.baseUrl);
        throw new ApiError(err, response.status);
      }

      return body;
    } catch (err) {
      if (err instanceof ApiError) throw err;
      if (err instanceof Error && err.name === "AbortError") {
        throw new ApiError(networkError(this.baseUrl));
      }
      throw new ApiError(networkError(this.baseUrl));
    } finally {
      clearTimeout(timeout);
    }
  }

  async post<T = unknown>(path: string, body?: unknown): Promise<T> {
    const url = this.buildUrl(path);
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 5000);

    try {
      const response = await fetch(url, {
        method: "POST",
        headers: {
          Authorization: `Bearer ${this.apiKey}`,
          "Content-Type": "application/json",
          Accept: "application/json",
        },
        body: body ? JSON.stringify(body) : undefined,
        signal: controller.signal,
      });

      const responseBody = await response.text();

      if (!response.ok) {
        const err = classifyHttpError(response.status, responseBody, this.baseUrl);
        throw new ApiError(err, response.status);
      }

      return JSON.parse(responseBody) as T;
    } catch (err) {
      if (err instanceof ApiError) throw err;
      if (err instanceof Error && err.name === "AbortError") {
        throw new ApiError(networkError(this.baseUrl));
      }
      throw new ApiError(networkError(this.baseUrl));
    } finally {
      clearTimeout(timeout);
    }
  }
}
