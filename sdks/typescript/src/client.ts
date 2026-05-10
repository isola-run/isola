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

export interface IsolaOptions {
  /** Base URL of the Isola API gateway. Falls back to ISOLA_URL env var. */
  url?: string;
  /**
   * Per-request HTTP timeout in milliseconds. Default 30_000.
   * Pass `null` to disable. Validated at construction; non-positive or
   * non-finite values throw TypeError.
   *
   * Note: requestTimeoutMs is constructor-only. Per-call cancellation uses
   * the `signal` option on individual methods.
   */
  requestTimeoutMs?: number | null;
  /** Custom fetch implementation (testing, proxies, custom transports). */
  fetch?: FetchLike;
}

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

  /** @internal — exposed for tests, mirrors Python client._api.url. */
  get url(): string {
    return this._api.url;
  }

  /** @internal — exposed for tests. */
  get isClosed(): boolean {
    return this._closed;
  }

  async close(): Promise<void> {
    this._closed = true;
    // The native fetch dispatcher we use does not need explicit shutdown;
    // close() exists for API parity and to flip a closed flag for tests.
  }

  async [Symbol.asyncDispose](): Promise<void> {
    await this.close();
  }
}
