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

// Mirrors sdks/python/src/isola/_client.py:_AsyncAPI logic for request/retry/decode.

import {
  APIConnectionError,
  APIError,
  connectionErrorFromError,
  errorFromHttp,
  isAbortError,
  isTransient,
} from "../errors";
import { buildUrl } from "./url";

export const DEFAULT_REQUEST_TIMEOUT_MS = 30_000; // mirrors Python _client.py read=30s
export const MAX_RETRIES = 5; // _client.py:32
export const RETRY_DELAY_MS = 1_000; // _client.py:33

const STREAM_CONNECT_TIMEOUT_MS = 5_000;

export type FetchLike = typeof fetch;

export interface HttpClientOptions {
  url: string;
  requestTimeoutMs: number | null;
  fetch?: FetchLike;
}

export interface RequestOpts {
  method: string;
  path: string;
  params?: Record<string, string | number | undefined>;
  jsonBody?: unknown;
  body?: BodyInit;
  bodyKind?: BodyKind; // for retry classification
  headers?: Record<string, string>;
  signal?: AbortSignal;
}

export type BodyKind = "replayable" | "stream" | "none";

interface AttemptSignal {
  userSignal: AbortSignal | undefined;
  timeoutSignal: AbortSignal | undefined;
  fetchSignal: AbortSignal | undefined;
}

function buildAttemptSignal(userSignal: AbortSignal | undefined, requestTimeoutMs: number | null): AttemptSignal {
  if (userSignal?.aborted) throw userSignal.reason;

  const timeoutSignal = requestTimeoutMs === null ? undefined : AbortSignal.timeout(requestTimeoutMs);

  const signals: AbortSignal[] = [];
  if (userSignal) signals.push(userSignal);
  if (timeoutSignal) signals.push(timeoutSignal);

  let fetchSignal: AbortSignal | undefined;
  if (signals.length === 1) {
    fetchSignal = signals[0];
  } else if (signals.length > 1) {
    fetchSignal = AbortSignal.any(signals);
  }

  return { userSignal, timeoutSignal, fetchSignal };
}

export function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(signal.reason);
      return;
    }
    const onAbort = (): void => {
      clearTimeout(timer);
      reject(signal?.reason);
    };
    const timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

// Replayable body kinds: string | Uint8Array | ArrayBuffer | Blob | URLSearchParams.
// Streams are non-replayable; FormData would be too if we supported it.
export function isReplayableBody(body: unknown): boolean {
  if (body == null) return true;
  if (typeof body === "string") return true;
  if (body instanceof Uint8Array) return true;
  if (body instanceof ArrayBuffer) return true;
  if (typeof Blob !== "undefined" && body instanceof Blob) return true;
  if (typeof URLSearchParams !== "undefined" && body instanceof URLSearchParams) return true;
  return false;
}

interface BodyAttempt {
  body: BodyInit | undefined;
  duplex?: "half";
  canRetry: boolean;
}

export class HttpClient {
  readonly url: string;
  readonly requestTimeoutMs: number | null;
  private readonly _fetch: FetchLike;

  constructor(opts: HttpClientOptions) {
    this.url = opts.url;
    this.requestTimeoutMs = opts.requestTimeoutMs;
    this._fetch = opts.fetch ?? globalThis.fetch.bind(globalThis);
  }

  private resolveBody(opts: RequestOpts): BodyAttempt {
    if (opts.jsonBody !== undefined) {
      return { body: JSON.stringify(opts.jsonBody), canRetry: true };
    }
    if (opts.body === undefined) {
      return { body: undefined, canRetry: true };
    }
    if (opts.bodyKind === "stream") {
      return { body: opts.body, duplex: "half", canRetry: false };
    }
    return { body: opts.body, canRetry: isReplayableBody(opts.body) };
  }

  async request(opts: RequestOpts): Promise<Response> {
    const url = buildUrl(this.url, opts.path, opts.params);
    const { body, duplex, canRetry } = this.resolveBody(opts);

    const headers = new Headers(opts.headers);
    if (opts.jsonBody !== undefined && !headers.has("content-type")) {
      headers.set("content-type", "application/json");
    }

    let lastError: unknown;
    for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
      const attemptSignal = buildAttemptSignal(opts.signal, this.requestTimeoutMs);

      const init: RequestInit & { duplex?: "half" } = {
        method: opts.method,
      };
      init.headers = headers;
      if (body !== undefined) init.body = body;
      if (duplex !== undefined) init.duplex = duplex;
      if (attemptSignal.fetchSignal) init.signal = attemptSignal.fetchSignal;

      let response: Response;
      try {
        response = await this._fetch(url, init);
      } catch (err) {
        lastError = err;
        if (attemptSignal.userSignal?.aborted) {
          throw attemptSignal.userSignal.reason ?? err;
        }
        const transportTimedOut = attemptSignal.timeoutSignal?.aborted === true || isAbortError(err);
        if (canRetry && attempt < MAX_RETRIES) {
          await sleep(RETRY_DELAY_MS, opts.signal);
          continue;
        }
        if (transportTimedOut) {
          throw connectionErrorFromError(err, { method: opts.method, path: opts.path });
        }
        // Non-Error JS runtime errors (RuntimeError-style) → APIConnectionError too.
        if (err instanceof APIError || err instanceof APIConnectionError) throw err;
        throw connectionErrorFromError(err, { method: opts.method, path: opts.path });
      }

      if (response.status >= 400) {
        const bodyBuf = new Uint8Array(await response.arrayBuffer());
        const apiErr = errorFromHttp({
          status: response.status,
          reason: response.statusText || null,
          body: bodyBuf,
          method: opts.method,
          path: opts.path,
        });
        if (isTransient(apiErr) && canRetry && attempt < MAX_RETRIES) {
          await sleep(RETRY_DELAY_MS, opts.signal);
          continue;
        }
        throw apiErr;
      }

      return response;
    }

    // Unreachable — the loop either returns or throws.
    throw connectionErrorFromError(lastError ?? new Error("retry loop exited unexpectedly"), {
      method: opts.method,
      path: opts.path,
    });
  }

  async requestNoContent(opts: RequestOpts): Promise<void> {
    const response = await this.request(opts);
    void response.body?.cancel().catch(() => {});
  }

  async requestModel<T>(opts: RequestOpts, decode: (json: unknown) => T): Promise<T> {
    const response = await this.request(opts);
    let payload: unknown;
    try {
      payload = await response.json();
    } catch (err) {
      throw new APIError({
        statusCode: response.status,
        message: "invalid response payload",
        cause: err,
      });
    }
    try {
      return decode(payload);
    } catch (err) {
      throw new APIError({
        statusCode: response.status,
        message: "invalid response payload",
        cause: err,
      });
    }
  }

  async requestBytes(opts: RequestOpts): Promise<Uint8Array> {
    const response = await this.request(opts);
    return new Uint8Array(await response.arrayBuffer());
  }

  // Stream connect timeout via manual AbortController + clearTimeout after
  // await fetch() returns headers. Native fetch takes one signal, so simply
  // composing user+timeout signals would also abort the body when the 5s timer
  // fires. The connect controller is composed with the user signal via
  // AbortSignal.any for the fetch call only; body lifetime is then governed
  // solely by the user signal.
  async fetchStream(
    path: string,
    opts: { headers?: Record<string, string>; signal?: AbortSignal } = {},
  ): Promise<Response> {
    const url = buildUrl(this.url, path);
    if (opts.signal?.aborted) throw opts.signal.reason;

    const connectController = new AbortController();
    let connectTimedOut = false;
    const timer = setTimeout(() => {
      connectTimedOut = true;
      connectController.abort(new DOMException("stream connect timeout", "TimeoutError"));
    }, STREAM_CONNECT_TIMEOUT_MS);

    const signals = [connectController.signal];
    if (opts.signal) signals.push(opts.signal);
    const composedSignal = signals.length === 1 ? signals[0] : AbortSignal.any(signals);

    try {
      const init: RequestInit = { method: "GET" };
      if (opts.headers) init.headers = opts.headers;
      if (composedSignal) init.signal = composedSignal;
      const response = await this._fetch(url, init);
      clearTimeout(timer);
      return response;
    } catch (err) {
      clearTimeout(timer);
      if (opts.signal?.aborted) throw opts.signal.reason ?? err;
      if (connectTimedOut || connectController.signal.aborted) {
        throw connectionErrorFromError(err, { method: "GET", path });
      }
      throw err;
    }
  }
}
