// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Mirrors sdks/python/src/isola/_client.py:AsyncIsola.

import { DEFAULT_REQUEST_TIMEOUT_MS, type FetchLike, HttpClient } from "./internal/http";
import { normalizeUrl } from "./internal/url";
import { RootfsSnapshots } from "./rootfs-snapshot";
import { Sandboxes } from "./sandbox";

/** Options for the {@link Isola} client constructor. */
export interface IsolaOptions {
  /**
   * Base URL of the Isola API gateway. If not provided, reads from
   * the `ISOLA_URL` environment variable.
   */
  url?: string;
  /**
   * Per-request HTTP timeout, in milliseconds. Default 30_000.
   * Pass `null` to disable. Validated at construction; non-positive
   * or non-finite values throw `TypeError`.
   *
   * `requestTimeoutMs` is constructor-only. Per-call cancellation
   * uses the `signal` option on individual methods.
   */
  requestTimeoutMs?: number | null;
  /**
   * Custom `fetch` implementation, for testing, proxies, or custom
   * transports.
   */
  fetch?: FetchLike;
}

/** Per-call options accepted by every SDK method. */
export interface RequestOptions {
  /** AbortSignal for per-call cancellation. */
  signal?: AbortSignal;
}

function resolveTimeout(value: number | null | undefined): number | null {
  if (value === undefined) return DEFAULT_REQUEST_TIMEOUT_MS;
  if (value === null) return null;
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0) {
    throw new TypeError("requestTimeoutMs must be a positive finite number, null, or undefined");
  }
  return value;
}

function resolveUrl(url: string | undefined): string {
  let candidate = url;
  if (candidate === undefined) {
    candidate = typeof process !== "undefined" && process.env ? process.env.ISOLA_URL : undefined;
  }
  if (candidate === undefined || candidate === null || candidate === "") {
    throw new Error("url must be provided either as argument or via the ISOLA_URL environment variable");
  }
  return normalizeUrl(candidate);
}

/**
 * Client for the Isola API.
 *
 * The client is async-disposable: use `await using` to close the
 * underlying HTTP connection automatically when the scope exits.
 *
 * @example
 * ```ts
 * import { Isola } from "@isola.run/sdk";
 *
 * await using client = new Isola();
 * await using sandbox = await client.sandboxes.create({
 *   image: "alpine:3.21",
 * });
 * const result = await sandbox.commands.run(["echo", "hello"]);
 * console.log(result.stdout);
 * ```
 *
 * @throws {Error} If no URL is provided and `ISOLA_URL` is unset.
 * @throws {TypeError} If `requestTimeoutMs` is invalid.
 */
export class Isola {
  /** @internal */
  readonly _api: HttpClient;
  readonly sandboxes: Sandboxes;
  readonly rootfsSnapshots: RootfsSnapshots;
  private _closed = false;

  constructor(options: IsolaOptions = {}) {
    const url = resolveUrl(options.url);
    const requestTimeoutMs = resolveTimeout(options.requestTimeoutMs);
    this._api = new HttpClient({
      url,
      requestTimeoutMs,
      ...(options.fetch ? { fetch: options.fetch } : {}),
    });
    this.sandboxes = new Sandboxes(this._api);
    this.rootfsSnapshots = new RootfsSnapshots(this._api);
  }

  /** @internal exposed for tests. */
  get url(): string {
    return this._api.url;
  }

  /** @internal exposed for tests. */
  get isClosed(): boolean {
    return this._closed;
  }

  /**
   * Close the client.
   *
   * Called automatically when using the client with `await using`.
   * The native fetch dispatcher does not need explicit shutdown; this
   * flips an internal closed flag and exists so callers always have a
   * symmetric `close()` method.
   */
  async close(): Promise<void> {
    this._closed = true;
    // The native fetch dispatcher we use does not need explicit shutdown;
    // close() exists to flip a closed flag for tests.
  }

  async [Symbol.asyncDispose](): Promise<void> {
    await this.close();
  }
}
