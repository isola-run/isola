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
import { VERSION } from "../version";
import { buildUrl } from "./url";

// Total per-attempt wall-clock budget. AbortSignal.timeout() aborts the whole
// fetch (connect+send+receive) when it fires. Python httpx splits this into
// connect/read/write/pool (5s/30s/30s/5s, all per-chunk); we use one total
// budget per attempt. Streams whose chunks arrive within 30s but exceed 30s
// total succeed in Python yet time out in TS.
export const DEFAULT_REQUEST_TIMEOUT_MS = 30_000;
export const MAX_RETRIES = 5;
export const RETRY_DELAY_MS = 1_000;

const DEFAULT_USER_AGENT = `@isola-run/sdk/${VERSION}`;

const STREAM_CONNECT_TIMEOUT_MS = 5_000;

/** @internal */
export type FetchLike = typeof fetch;

/** @internal */
export interface HttpClientOptions {
  url: string;
  requestTimeoutMs: number | null;
  fetch?: FetchLike;
}

/** @internal */
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

/** @internal */
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

// Replayable body kinds: string | ArrayBufferView (Uint8Array, Int8Array, etc.)
// | ArrayBuffer | Blob | URLSearchParams.
// Streams are non-replayable; FormData would be too if we supported it.
export function isReplayableBody(body: unknown): boolean {
  if (body == null) return true;
  if (typeof body === "string") return true;
  if (body instanceof ArrayBuffer) return true;
  if (ArrayBuffer.isView(body)) return true; // covers Uint8Array, Int8Array, DataView, etc.
  if (typeof Blob !== "undefined" && body instanceof Blob) return true;
  if (typeof URLSearchParams !== "undefined" && body instanceof URLSearchParams) return true;
  return false;
}

interface BodyAttempt {
  body: BodyInit | undefined;
  duplex?: "half";
  canRetry: boolean;
}

/** @internal */
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
    // User-Agent is rejected by some runtimes (Workers, browser-fetch in older
    // Node) so add it only if the runtime allows it. Worker runtimes ignore
    // attempts to set it; Node and Bun accept it. Send a best-effort default.
    if (!headers.has("user-agent")) {
      try {
        headers.set("user-agent", DEFAULT_USER_AGENT);
      } catch (err) {
        // TypeError on a forbidden-header runtime is expected; anything else
        // is a real bug.
        if (!(err instanceof TypeError)) throw err;
      }
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
        // SDK-typed errors from a custom fetch: route through the same
        // transient gate the response-status path uses, so a custom fetch
        // throwing APIConnectionError or APIError(502|503|504) still benefits
        // from the retry policy. Non-transient SDK errors pass through.
        if (err instanceof APIError || err instanceof APIConnectionError) {
          if (isTransient(err) && canRetry && attempt < MAX_RETRIES) {
            await sleep(RETRY_DELAY_MS, opts.signal);
            continue;
          }
          throw err;
        }
        // Only retry on transport-shaped failures: native fetch rejects with
        // TypeError on network/DNS/TLS errors, AbortError on aborts, or our
        // per-attempt timeout signal firing. Other thrown values (e.g. a
        // user-supplied fetch throwing a RangeError) are wrapped as
        // APIConnectionError and propagated without retry, matching Python.
        const transportTimedOut = attemptSignal.timeoutSignal?.aborted === true || isAbortError(err);
        const isTransport = transportTimedOut || err instanceof TypeError;
        if (isTransport && canRetry && attempt < MAX_RETRIES) {
          await sleep(RETRY_DELAY_MS, opts.signal);
          continue;
        }
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

    // Unreachable: the loop either returns or throws.
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
      // If the caller aborted mid-body, surface the abort, not a wrapped
      // "invalid response payload". Otherwise an in-flight cancellation
      // becomes indistinguishable from a server-side malformed JSON.
      if (opts.signal?.aborted) throw opts.signal.reason ?? err;
      if (isAbortError(err)) throw err;
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
      // Wrap connect-timeout and transport-shaped failures so the StreamReader
      // catch can classify them via isTransient() and retry. Native fetch
      // throws TypeError on network/DNS/TLS errors; AbortError indicates the
      // connect timer fired. SDK-typed errors are forwarded verbatim so a
      // custom fetch can already classify failures.
      if (err instanceof APIError || err instanceof APIConnectionError) throw err;
      if (connectTimedOut || connectController.signal.aborted || isAbortError(err) || err instanceof TypeError) {
        throw connectionErrorFromError(err, { method: "GET", path });
      }
      throw err;
    }
  }
}
