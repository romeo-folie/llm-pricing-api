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

  private debugKey(): string {
    return this.apiKey.substring(0, 8) + "...";
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

      return JSON.parse(body) as T;
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
