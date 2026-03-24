import {
  APIError,
  APIConnectionError,
  type IsolaError,
  isTransient,
} from "./errors.js";
import type { StreamAPI } from "./streaming.js";
import { VERSION } from "./version.js";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const DEFAULT_TIMEOUT_MS = 30_000;
const DEFAULT_MAX_RETRIES = 5;
const INITIAL_RETRY_DELAY_S = 0.5;
const MAX_RETRY_DELAY_S = 8.0;

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type HttpMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

// ---------------------------------------------------------------------------
// Request options
// ---------------------------------------------------------------------------

export interface RequestOptions {
  /** JSON-serializable body (mutually exclusive with `content`). */
  body?: unknown;
  /** Raw body for binary uploads (mutually exclusive with `body`). */
  content?: Uint8Array | Blob | ReadableStream<Uint8Array> | string;
  /** URL query parameters. */
  params?: Record<string, string>;
  /** Additional request headers. */
  headers?: Record<string, string>;
  /** Per-request timeout override in milliseconds. */
  timeout?: number;
}

// ---------------------------------------------------------------------------
// Client options
// ---------------------------------------------------------------------------

export interface APIClientOptions {
  /** Base URL of the API (required). */
  baseURL: string;
  /** Default request timeout in milliseconds (default: 30000). */
  timeout?: number;
  /** Maximum number of retries on transient errors (default: 5). */
  maxRetries?: number;
  /** Headers sent with every request. Per-request headers take precedence. */
  defaultHeaders?: Record<string, string>;
  /**
   * Custom `fetch` implementation. Defaults to the global `fetch`.
   *
   * Useful for testing or for environments where the global is unavailable.
   */
  fetch?: typeof globalThis.fetch;
}

// ---------------------------------------------------------------------------
// APIClient
// ---------------------------------------------------------------------------

/**
 * Low-level HTTP client used internally by every resource class.
 *
 * All public methods include automatic retry on transient errors
 * (connection failures, 408/429/502/503/504) with exponential backoff
 * and jitter, up to {@link DEFAULT_MAX_RETRIES} times by default.
 *
 * @internal
 */
export class APIClient implements StreamAPI {
  private readonly baseURL: string;
  private readonly timeout: number;
  private readonly maxRetries: number;
  private readonly defaultHeaders: Record<string, string>;
  private readonly fetchFn: typeof globalThis.fetch;

  constructor(options: APIClientOptions) {
    this.baseURL = options.baseURL.replace(/\/+$/, "");
    if (!this.baseURL) {
      throw new Error("baseURL must be a non-empty string");
    }
    this.timeout = options.timeout ?? DEFAULT_TIMEOUT_MS;
    this.maxRetries = options.maxRetries ?? DEFAULT_MAX_RETRIES;
    this.defaultHeaders = options.defaultHeaders ?? {};
    this.fetchFn = options.fetch ?? globalThis.fetch;
  }

  // -----------------------------------------------------------------------
  // Public (with retry)
  // -----------------------------------------------------------------------

  /** Send a request and parse the JSON response. */
  async request<T>(
    method: HttpMethod,
    path: string,
    options?: RequestOptions,
  ): Promise<T> {
    return this.withRetry(async (retryCount) => {
      const res = await this.rawFetch(method, path, options, retryCount);
      try {
        return (await res.json()) as T;
      } catch (error) {
        throw APIConnectionError.fromError(error, method, path);
      }
    });
  }

  /** Send a request that returns no body (204 / fire-and-forget). */
  async requestNoContent(
    method: HttpMethod,
    path: string,
    options?: RequestOptions,
  ): Promise<void> {
    await this.withRetry(async (retryCount) => {
      const res = await this.rawFetch(method, path, options, retryCount);
      // Consume the body so the connection can be reused.
      await res.body?.cancel();
    });
  }

  /** Send a request and return the response as raw bytes. */
  async requestBytes(
    method: HttpMethod,
    path: string,
    options?: RequestOptions,
  ): Promise<Uint8Array> {
    return this.withRetry(async (retryCount) => {
      const res = await this.rawFetch(method, path, options, retryCount);
      try {
        return new Uint8Array(await res.arrayBuffer());
      } catch (error) {
        throw APIConnectionError.fromError(error, method, path);
      }
    });
  }

  /**
   * Open an SSE stream connection (no retry — {@link StreamReader} manages
   * its own reconnection).
   */
  async openStream(
    path: string,
    options?: { headers?: Record<string, string> },
  ): Promise<Response> {
    return this.rawFetch("GET", path, {
      headers: {
        Accept: "text/event-stream",
        ...options?.headers,
      },
      // No timeout for long-lived streams.
      timeout: 0,
    });
  }

  // -----------------------------------------------------------------------
  // Internals
  // -----------------------------------------------------------------------

  private async rawFetch(
    method: HttpMethod,
    path: string,
    options?: RequestOptions,
    retryCount?: number,
  ): Promise<Response> {
    let url = this.baseURL + path;
    if (options?.params && Object.keys(options.params).length > 0) {
      url += "?" + new URLSearchParams(options.params).toString();
    }

    const headers: Record<string, string> = {
      "User-Agent": `isola-typescript/${VERSION}`,
      ...this.defaultHeaders,
      ...options?.headers,
    };

    if (retryCount !== undefined && retryCount > 0) {
      headers["X-Retry-Count"] = String(retryCount);
    }

    let body: Uint8Array | Blob | ReadableStream<Uint8Array> | string | undefined;
    if (options?.content !== undefined) {
      body = options.content;
    } else if (options?.body !== undefined) {
      headers["Content-Type"] = "application/json";
      body = JSON.stringify(options.body);
    }

    const timeout = options?.timeout ?? this.timeout;
    const signal = timeout > 0 ? AbortSignal.timeout(timeout) : undefined;

    let response: Response;
    try {
      response = await this.fetchFn.call(undefined, url, {
        method,
        headers,
        body,
        signal,
      });
    } catch (error) {
      throw APIConnectionError.fromError(error, method, path);
    }

    if (!response.ok) {
      const responseBody = await response.text();
      throw APIError.fromResponse(
        response.status,
        responseBody,
        response.headers,
        method,
        path,
      );
    }

    return response;
  }

  /**
   * Exponential backoff with jitter, matching Anthropic/OpenAI SDK patterns.
   *
   * Delay: min(initialDelay * 2^attempt, maxDelay) * jitter
   * where jitter is uniform in [0.75, 1.0].
   *
   * Respects `Retry-After` / `Retry-After-Ms` headers when present.
   */
  private retryDelay(attempt: number, responseHeaders?: Headers): number {
    if (responseHeaders) {
      const retryAfterMs = responseHeaders.get("retry-after-ms");
      if (retryAfterMs) {
        const ms = parseFloat(retryAfterMs);
        if (!Number.isNaN(ms)) return ms;
      }

      const retryAfter = responseHeaders.get("retry-after");
      if (retryAfter) {
        const seconds = parseFloat(retryAfter);
        if (!Number.isNaN(seconds)) return seconds * 1_000;
        const date = Date.parse(retryAfter);
        if (!Number.isNaN(date)) return Math.max(0, date - Date.now());
      }
    }

    const sleepSeconds = Math.min(
      INITIAL_RETRY_DELAY_S * Math.pow(2, attempt),
      MAX_RETRY_DELAY_S,
    );
    const jitter = 1 - Math.random() * 0.25;
    return sleepSeconds * jitter * 1_000;
  }

  private async withRetry<T>(
    fn: (retryCount: number) => Promise<T>,
  ): Promise<T> {
    let lastError: IsolaError | undefined;
    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      try {
        return await fn(attempt);
      } catch (error) {
        if (
          error instanceof APIError ||
          error instanceof APIConnectionError
        ) {
          if (isTransient(error) && attempt < this.maxRetries) {
            lastError = error;
            const headers =
              error instanceof APIError ? error.headers : undefined;
            const delay = this.retryDelay(attempt, headers);
            await new Promise((r) => setTimeout(r, delay));
            continue;
          }
        }
        throw error;
      }
    }
    // Unreachable in practice — the last attempt throws.
    throw lastError!;
  }
}
