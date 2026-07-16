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
import { combineSignals } from "./signal";
import { buildUrl } from "./url";

// Total per-attempt wall-clock budget. AbortSignal.timeout() aborts the whole
// fetch (connect+send+receive) when it fires. Python httpx splits this into
// connect/read/write/pool (5s/30s/30s/5s, all per-chunk); we use one total
// budget per attempt. Streams whose chunks arrive within 30s but exceed 30s
// total succeed in Python yet time out in TS.
export const DEFAULT_REQUEST_TIMEOUT_MS = 30_000;
export const MAX_RETRIES = 5;
export const RETRY_DELAY_MS = 1_000;

const STREAM_CONNECT_TIMEOUT_MS = 5_000;
const RETRYABLE_METHODS = new Set(["GET", "HEAD", "OPTIONS", "PUT", "DELETE"]);

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
  // Uint8Array is accepted alongside BodyInit: it is a valid fetch body at
  // runtime, but since TS 5.7 made Uint8Array generic it no longer matches
  // lib.dom's BodyInit. Reconciled with a single assertion in request().
  body?: BodyInit | Uint8Array;
  headers?: Record<string, string>;
  signal?: AbortSignal;
}

interface AttemptSignal {
  userSignal: AbortSignal | undefined;
  timeoutSignal: AbortSignal | undefined;
  fetchSignal: AbortSignal | undefined;
}

function buildAttemptSignal(userSignal: AbortSignal | undefined, requestTimeoutMs: number | null): AttemptSignal {
  if (userSignal?.aborted) throw userSignal.reason;

  const timeoutSignal = requestTimeoutMs === null ? undefined : AbortSignal.timeout(requestTimeoutMs);
  return { userSignal, timeoutSignal, fetchSignal: combineSignals(userSignal, timeoutSignal) };
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

// A ReadableStream body is consumed once and cannot be replayed. Other body
// kinds can be replayed, but only idempotent methods are safe to resend.
function isStreamBody(body: BodyInit | undefined): boolean {
  return typeof ReadableStream !== "undefined" && body instanceof ReadableStream;
}

// Native fetch + arrayBuffer reject with TypeError on network/DNS/TLS/stream
// errors, AbortError/TimeoutError on abort. Anything else is a non-transport
// failure (e.g. a user-supplied fetch throwing RangeError) and gets wrapped
// as APIConnectionError without retry.
function isTransportFailure(err: unknown, attemptSignal: AttemptSignal): boolean {
  if (attemptSignal.timeoutSignal?.aborted === true) return true;
  if (isAbortError(err)) return true;
  return err instanceof TypeError;
}

const UTF8_DECODER = new TextDecoder("utf-8", { fatal: false });

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

  // Body is consumed inside the retry loop so mid-stream drops and per-attempt
  // timeouts retry like a headers-phase fetch() rejection, matching Python httpx.
  async request(opts: RequestOpts): Promise<{ response: Response; bodyBytes: Uint8Array }> {
    const url = buildUrl(this.url, opts.path, opts.params);
    // The lone BodyInit assertion for the whole SDK: opts.body may be a
    // Uint8Array (see the RequestOpts.body note). Both arms are real fetch bodies.
    const body: BodyInit | undefined =
      opts.jsonBody !== undefined ? JSON.stringify(opts.jsonBody) : (opts.body as BodyInit | undefined);
    const streaming = isStreamBody(body);
    const canRetry = RETRYABLE_METHODS.has(opts.method.toUpperCase()) && !streaming;

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
      if (streaming) init.duplex = "half";
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
        if (isTransportFailure(err, attemptSignal) && canRetry && attempt < MAX_RETRIES) {
          await sleep(RETRY_DELAY_MS, opts.signal);
          continue;
        }
        throw connectionErrorFromError(err, { method: opts.method, path: opts.path });
      }

      let bodyBytes: Uint8Array;
      try {
        bodyBytes = new Uint8Array(await response.arrayBuffer());
      } catch (err) {
        lastError = err;
        if (attemptSignal.userSignal?.aborted) {
          throw attemptSignal.userSignal.reason ?? err;
        }
        if (isTransportFailure(err, attemptSignal) && canRetry && attempt < MAX_RETRIES) {
          await sleep(RETRY_DELAY_MS, opts.signal);
          continue;
        }
        throw connectionErrorFromError(err, { method: opts.method, path: opts.path });
      }

      if (response.status >= 400) {
        const apiErr = errorFromHttp({
          status: response.status,
          reason: response.statusText || null,
          body: bodyBytes,
          method: opts.method,
          path: opts.path,
        });
        if (isTransient(apiErr) && canRetry && attempt < MAX_RETRIES) {
          await sleep(RETRY_DELAY_MS, opts.signal);
          continue;
        }
        throw apiErr;
      }

      return { response, bodyBytes };
    }

    // Unreachable: the loop either returns or throws.
    throw connectionErrorFromError(lastError ?? new Error("retry loop exited unexpectedly"), {
      method: opts.method,
      path: opts.path,
    });
  }

  async requestNoContent(opts: RequestOpts): Promise<void> {
    await this.request(opts);
  }

  async requestModel<T>(opts: RequestOpts, decode: (json: unknown) => T): Promise<T> {
    const { response, bodyBytes } = await this.request(opts);
    let payload: unknown;
    try {
      payload = JSON.parse(UTF8_DECODER.decode(bodyBytes));
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
    const { bodyBytes } = await this.request(opts);
    return bodyBytes;
  }

  // Bounds the connect phase without bounding the body: a dedicated
  // AbortController fires after STREAM_CONNECT_TIMEOUT_MS and is cleared once
  // headers arrive. Body lifetime is then governed by the user signal alone.
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

    const composedSignal = combineSignals(connectController.signal, opts.signal);

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
      // Wrap connect-timeout and transport failures (TypeError / AbortError) as
      // APIConnectionError so StreamReader can classify and retry them. SDK-typed
      // errors pass through for a custom fetch to classify.
      if (err instanceof APIError || err instanceof APIConnectionError) throw err;
      if (connectTimedOut || connectController.signal.aborted || isAbortError(err) || err instanceof TypeError) {
        throw connectionErrorFromError(err, { method: "GET", path });
      }
      throw err;
    }
  }
}
