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

// Mirrors sdks/python/src/isola/_streaming.py:AsyncStreamReader.
//
// Single-use SSE iterator with Last-Event-ID resume across reconnects.
// Reconnect counter resets only on successful **data** event, not on
// heartbeat-only events.
//
// HTTP error path mirrors Python _streaming.py:113-114: errorFromHttp(status,
// null, body) intentionally omits method/path (deliberate divergence from the
// non-streaming error path). isTransient() classifies APIError; non-transient
// errors raise immediately (no reconnect). Cause chained on reconnect-exhausted
// error.

import { APIConnectionError, APIError, connectionErrorFromError, errorFromHttp, isTransient } from "./errors";
import type { HttpClient } from "./internal/http";
import { sleep } from "./internal/http";
import { parseSSE } from "./internal/sse";

export const MAX_RECONNECTS = 5;
const RETRY_DELAY_MS = 1_000;

/** Options for {@link StreamReader.iter} and {@link StreamReader.read}. */
export interface StreamReadOptions {
  /** AbortSignal for per-call cancellation. */
  signal?: AbortSignal;
}

/**
 * A single-use reader for a streaming output.
 *
 * Iterate over it with `for await` to receive chunks as they arrive,
 * or call {@link StreamReader.read} to collect everything into a
 * string. Attempting to iterate a second time throws.
 *
 * The reader reconnects automatically on transient network errors,
 * resuming from where it left off (`Last-Event-ID`).
 *
 * @example
 * ```ts
 * const cmd = await sandbox.commands.spawn(["ls", "-la"]);
 * for await (const chunk of cmd.stdout) {
 *   process.stdout.write(chunk);
 * }
 *
 * // Or read everything at once:
 * const output = await cmd.stderr.read();
 * ```
 */
export class StreamReader implements AsyncIterable<string> {
  private readonly _api: HttpClient;
  private readonly _path: string;
  private _lastEventId: string | null = null;
  private _consumed = false;
  private _retryDelayMs = RETRY_DELAY_MS;

  constructor(api: HttpClient, path: string) {
    this._api = api;
    this._path = path;
  }

  [Symbol.asyncIterator](): AsyncIterator<string> {
    return this.iter();
  }

  /**
   * Begin iterating the stream.
   *
   * @throws {Error} If the stream has already been consumed.
   */
  iter(opts: StreamReadOptions = {}): AsyncIterableIterator<string> {
    if (this._consumed) {
      throw new Error("StreamReader is single-use and has already been consumed");
    }
    this._consumed = true;
    return this._generate(opts.signal);
  }

  /**
   * Read the entire stream and return it as a string.
   *
   * @throws {Error} If the stream has already been consumed.
   */
  async read(opts: StreamReadOptions = {}): Promise<string> {
    const chunks: string[] = [];
    for await (const chunk of this.iter(opts)) {
      chunks.push(chunk);
    }
    return chunks.join("");
  }

  private async *_generate(signal: AbortSignal | undefined): AsyncIterableIterator<string> {
    let reconnects = 0;
    while (true) {
      try {
        const headers: Record<string, string> = {};
        if (this._lastEventId !== null) {
          headers["Last-Event-ID"] = this._lastEventId;
        }
        const response = await this._api.fetchStream(this._path, {
          headers,
          ...(signal ? { signal } : {}),
        });
        if (response.status >= 400) {
          const body = new Uint8Array(await response.arrayBuffer());
          throw errorFromHttp({
            status: response.status,
            reason: null,
            body,
          });
        }

        if (!response.body) return;

        for await (const event of parseSSE(response.body, signal)) {
          if (event.id !== null) this._lastEventId = event.id;
          if (event.retryMs !== undefined) this._retryDelayMs = event.retryMs;
          if (event.data) {
            reconnects = 0;
            yield event.data;
          }
        }
        return;
      } catch (err) {
        if (signal?.aborted) throw signal.reason ?? err;
        // Retry only network-shaped or SDK transport/server errors. Anything
        // else (TypeError from a parser bug, etc.) is a programming error and
        // must propagate immediately rather than be hidden behind 5 reconnect
        // attempts and a misleading "failed to reach Isola API" wrap.
        if (!(err instanceof APIError) && !(err instanceof APIConnectionError) && !(err instanceof TypeError)) {
          throw err;
        }
        if (err instanceof APIError && !isTransient(err)) throw err;
        reconnects += 1;
        if (reconnects > MAX_RECONNECTS) {
          if (err instanceof APIError) throw err;
          throw connectionErrorFromError(err);
        }
        await sleep(this._retryDelayMs, signal);
      }
    }
  }
}
