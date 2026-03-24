import { APIClient } from "./client.js";
import { Sandboxes } from "./sandbox.js";

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

export interface IsolaOptions {
  /**
   * Base URL of the Isola API gateway.
   *
   * Falls back to the `ISOLA_BASE_URL` environment variable when omitted.
   */
  baseURL?: string;

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
// Isola (top-level client)
// ---------------------------------------------------------------------------

/**
 * Entry point for the Isola SDK.
 *
 * ```ts
 * const isola = new Isola({ baseURL: "http://localhost:8080" });
 * const sandbox = await isola.sandboxes.create({ image: "ubuntu:24.04" });
 * ```
 */
export class Isola {
  readonly sandboxes: Sandboxes;

  constructor(options?: IsolaOptions) {
    const baseURL =
      options?.baseURL ??
      (typeof process !== "undefined" ? process.env.ISOLA_BASE_URL : undefined);

    if (!baseURL) {
      throw new Error(
        "baseURL must be provided or set via the ISOLA_BASE_URL environment variable",
      );
    }

    const api = new APIClient({
      baseURL,
      timeout: options?.timeout,
      maxRetries: options?.maxRetries,
      defaultHeaders: options?.defaultHeaders,
      fetch: options?.fetch,
    });
    this.sandboxes = new Sandboxes(api);
  }

  /** Release any resources held by the client. */
  close(): void {
    // Reserved for future connection pool cleanup.
  }

  /**
   * Support `using client = ...` (TC39 Explicit Resource Management).
   * Calls {@link close} on disposal.
   */
  [Symbol.dispose](): void {
    this.close();
  }
}
