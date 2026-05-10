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

// Hand-rolled SSE parser modeled on Cloudflare's parseSSEFrames.
// Adds id: parsing for Last-Event-ID resume per WHATWG.

export interface SSEEvent {
  id: string | null;
  data: string;
  retryMs?: number;
}

const NUL = "\u0000";

// WHATWG dispatch step: only emit on a blank-line boundary. EOF without a
// trailing blank line discards pending data — matches Python httpx-sse.
// retry: field is parsed and clamped to [1s, 60s].
// id: field stored as opaque string; NUL-containing ids ignored.
export async function* parseSSE(body: ReadableStream<Uint8Array>, signal?: AbortSignal): AsyncGenerator<SSEEvent> {
  const decoder = new TextDecoder("utf-8");
  const reader = body.getReader();
  let buffer = "";
  let currentId: string | null = null;
  let currentData: string[] = [];
  let currentRetry: number | undefined;

  function processLine(line: string): SSEEvent | null {
    if (line === "") {
      if (currentData.length > 0) {
        const data = currentData.join("\n");
        currentData = [];
        const ev: SSEEvent = { id: currentId, data };
        if (currentRetry !== undefined) {
          ev.retryMs = currentRetry;
          currentRetry = undefined;
        }
        return ev;
      }
      return null;
    }
    if (line.startsWith(":")) return null; // comment/heartbeat
    if (line.startsWith("data:")) {
      currentData.push(line.startsWith("data: ") ? line.slice(6) : line.slice(5));
      return null;
    }
    if (line.startsWith("id:")) {
      const value = line.startsWith("id: ") ? line.slice(4) : line.slice(3);
      if (value.includes(NUL)) return null; // WHATWG: ignore NUL-containing ids
      currentId = value;
      return null;
    }
    if (line.startsWith("retry:")) {
      const value = line.startsWith("retry: ") ? line.slice(7) : line.slice(6);
      if (/^\d+$/.test(value)) {
        const ms = Number.parseInt(value, 10);
        currentRetry = Math.max(1_000, Math.min(60_000, ms));
      }
      return null;
    }
    return null; // unknown field; ignore (event:, etc.)
  }

  // Abort listener calls reader.cancel() so a user abort during
  // `await reader.read()` actually unblocks the read instead of waiting
  // for the next chunk.
  const onAbort = (): void => {
    void reader.cancel(signal?.reason).catch(() => {});
  };
  if (signal?.aborted) {
    reader.releaseLock();
    throw signal.reason;
  }
  signal?.addEventListener("abort", onAbort, { once: true });

  try {
    while (true) {
      signal?.throwIfAborted();
      const { done, value } = await reader.read();
      if (done) {
        // WHATWG: discard any pending data buffer at EOF. Flush partial
        // UTF-8 (replacement char) and the final unterminated line through
        // processLine, but DO NOT dispatch a final unterminated event.
        buffer += decoder.decode();
        if (buffer.length > 0) {
          const ev = processLine(buffer.replace(/\r$/, ""));
          if (ev) yield ev;
          buffer = "";
        }
        return;
      }
      buffer += decoder.decode(value, { stream: true });
      let eolIdx: number;
      // biome-ignore lint/suspicious/noAssignInExpressions: idiomatic SSE parser.
      while ((eolIdx = buffer.indexOf("\n")) !== -1) {
        const line = buffer.slice(0, eolIdx).replace(/\r$/, "");
        buffer = buffer.slice(eolIdx + 1);
        const ev = processLine(line);
        if (ev) yield ev;
      }
    }
  } finally {
    signal?.removeEventListener("abort", onAbort);
    try {
      reader.releaseLock();
    } catch {
      // releaseLock throws if a read is pending; ignore.
    }
    void body.cancel().catch(() => {});
  }
}
