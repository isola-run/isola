import {
  type IsolaError,
  APIError,
  APIConnectionError,
  isTransient,
} from "./errors.js";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MAX_RECONNECTS = 5;
const RETRY_DELAY_MS = 1_000;

// ---------------------------------------------------------------------------
// StreamAPI interface (avoids circular dependency with client.ts)
// ---------------------------------------------------------------------------

/** Minimal contract that {@link StreamReader} needs from the HTTP layer. */
export interface StreamAPI {
  openStream(
    path: string,
    options?: { headers?: Record<string, string> },
  ): Promise<Response>;
}

// ---------------------------------------------------------------------------
// SSE parser
// ---------------------------------------------------------------------------

interface SSEEvent {
  data?: string;
  id?: string;
}

/** Parse a ReadableStream of bytes as an SSE event stream. */
async function* parseSSE(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<SSEEvent> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let dataLines: string[] = [];
  let currentId: string | undefined;

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      // Keep the last (potentially incomplete) line in the buffer.
      buffer = lines.pop()!;

      for (const rawLine of lines) {
        const line = rawLine.endsWith("\r")
          ? rawLine.slice(0, -1)
          : rawLine;

        if (line === "") {
          // Blank line → dispatch event.
          if (dataLines.length > 0) {
            yield { data: dataLines.join("\n"), id: currentId };
            dataLines = [];
            currentId = undefined;
          }
          continue;
        }

        const colonIdx = line.indexOf(":");
        if (colonIdx === 0) continue; // Comment (keepalive).

        const field =
          colonIdx > 0 ? line.slice(0, colonIdx) : line;
        const val =
          colonIdx > 0 ? line.slice(colonIdx + 1).replace(/^ /, "") : "";

        switch (field) {
          case "data":
            dataLines.push(val);
            break;
          case "id":
            currentId = val;
            break;
        }
      }
    }

    // Process any remaining data left in the buffer.
    if (buffer) {
      const line = buffer.endsWith("\r") ? buffer.slice(0, -1) : buffer;
      const colonIdx = line.indexOf(":");
      if (colonIdx !== 0) {
        const field = colonIdx > 0 ? line.slice(0, colonIdx) : line;
        const val = colonIdx > 0 ? line.slice(colonIdx + 1).replace(/^ /, "") : "";
        if (field === "data") dataLines.push(val);
        else if (field === "id") currentId = val;
      }
    }

    // Flush any trailing data that wasn't terminated by a blank line.
    if (dataLines.length > 0) {
      yield { data: dataLines.join("\n"), id: currentId };
    }
  } finally {
    // cancel() both cancels the underlying stream and releases the lock,
    // ensuring the connection is freed on error or early return.
    await reader.cancel().catch(() => {});
  }
}

// ---------------------------------------------------------------------------
// StreamReader
// ---------------------------------------------------------------------------

/**
 * Async-iterable reader for an SSE stream with transparent reconnection.
 *
 * Tracks the last event `id` and sends it as `Last-Event-ID` on reconnect
 * so the server can resume from the correct byte offset.
 *
 * Single-use: iterating a second time throws an error.
 */
export class StreamReader implements AsyncIterable<string> {
  private readonly api: StreamAPI;
  private readonly path: string;
  private consumed = false;
  private lastEventId: string | null = null;

  /** @internal */
  constructor(api: StreamAPI, path: string) {
    this.api = api;
    this.path = path;
  }

  async *[Symbol.asyncIterator](): AsyncIterableIterator<string> {
    if (this.consumed) {
      throw new Error("StreamReader has already been consumed");
    }
    this.consumed = true;

    let reconnects = 0;

    for (;;) {
      try {
        const headers: Record<string, string> = {};
        if (this.lastEventId !== null) {
          headers["Last-Event-ID"] = this.lastEventId;
        }

        const response = await this.api.openStream(this.path, { headers });

        if (!response.body) {
          throw new Error("Response body is not readable");
        }

        for await (const event of parseSSE(response.body)) {
          if (event.id !== undefined) {
            this.lastEventId = event.id;
          }
          if (event.data !== undefined) {
            // Reset reconnect counter on successful data delivery,
            // matching the Python SDK behavior.
            reconnects = 0;
            yield event.data;
          }
        }

        // Stream completed normally.
        return;
      } catch (error) {
        const isIsolaErr =
          error instanceof APIError ||
          error instanceof APIConnectionError;
        if (
          isIsolaErr &&
          isTransient(error as IsolaError) &&
          reconnects < MAX_RECONNECTS
        ) {
          reconnects++;
          await new Promise((r) => setTimeout(r, RETRY_DELAY_MS));
          continue;
        }
        throw error;
      }
    }
  }

  /** Consume the entire stream and return its content as a single string. */
  async read(): Promise<string> {
    const chunks: string[] = [];
    for await (const chunk of this) {
      chunks.push(chunk);
    }
    return chunks.join("");
  }
}
