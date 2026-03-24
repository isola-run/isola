import { describe, it, expect, vi } from "vitest";
import { StreamReader, type StreamAPI } from "../src/streaming.js";
import {
  APIConnectionError,
  BadGatewayError,
  NotFoundError,
} from "../src/errors.js";
import { sseResponse, sseResponseChunked } from "./helpers.js";

function createMockStreamAPI(): StreamAPI & {
  openStream: ReturnType<typeof vi.fn>;
} {
  return {
    openStream: vi.fn(),
  };
}

/**
 * Create a ReadableStream that yields one chunk of SSE data, then errors
 * on the next read. This simulates a connection drop mid-stream.
 */
function failingSSEStream(
  data: string,
  error: Error,
): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  let delivered = false;
  return new ReadableStream<Uint8Array>({
    pull(controller) {
      if (!delivered) {
        delivered = true;
        controller.enqueue(encoder.encode(data));
      } else {
        controller.error(error);
      }
    },
  });
}

describe("StreamReader", () => {
  it("yields data events from SSE stream", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponse("id: 0\ndata: hello\n\nid: 5\ndata: world\n\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const chunks: string[] = [];
    for await (const chunk of reader) {
      chunks.push(chunk);
    }

    expect(chunks).toEqual(["hello", "world"]);
  });

  it("handles multiline data events", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponse("data: line1\ndata: line2\ndata: line3\n\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const chunks: string[] = [];
    for await (const chunk of reader) {
      chunks.push(chunk);
    }

    expect(chunks).toEqual(["line1\nline2\nline3"]);
  });

  it("ignores keepalive comments", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponse(
        ": keepalive\ndata: actual\n\n: another\ndata: data\n\n",
      ),
    );

    const reader = new StreamReader(api, "/stream");
    const chunks: string[] = [];
    for await (const chunk of reader) {
      chunks.push(chunk);
    }

    expect(chunks).toEqual(["actual", "data"]);
  });

  it("handles empty data events", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponse("id: 0\ndata: \n\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe("");
  });

  it("handles chunked delivery across chunk boundaries", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponseChunked(["id: 0\nda", "ta: hel", "lo\n\n"]),
    );

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe("hello");
  });

  it("read() returns full concatenated content", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponse("data: a\n\ndata: b\n\ndata: c\n\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe("abc");
  });
});

describe("StreamReader single-use", () => {
  it("throws on second iteration", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValue(sseResponse("data: x\n\n"));

    const reader = new StreamReader(api, "/stream");

    for await (const _ of reader) {
      // consume
    }

    await expect(async () => {
      for await (const _ of reader) {
        // should not reach here
      }
    }).rejects.toThrow("already been consumed");
  });
});

describe("StreamReader reconnection", () => {
  it("reconnects when openStream fails with transient error", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockRejectedValueOnce(
      new APIConnectionError("connect failed"),
    );
    api.openStream.mockResolvedValueOnce(
      sseResponse("id: 0\ndata: recovered\n\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe("recovered");
    expect(api.openStream).toHaveBeenCalledTimes(2);
  }, 10_000);

  it("reconnects when stream errors mid-read and sends Last-Event-ID", async () => {
    const api = createMockStreamAPI();

    // First connection: delivers data then errors during next read
    api.openStream.mockResolvedValueOnce(
      new Response(
        failingSSEStream(
          "id: 42\ndata: partial\n\n",
          new APIConnectionError("dropped"),
        ),
        {
          status: 200,
          headers: { "Content-Type": "text/event-stream" },
        },
      ),
    );
    // Second connection: resumes from where we left off
    api.openStream.mockResolvedValueOnce(
      sseResponse("id: 50\ndata: rest\n\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const chunks: string[] = [];
    for await (const chunk of reader) {
      chunks.push(chunk);
    }

    expect(chunks).toEqual(["partial", "rest"]);
    expect(api.openStream).toHaveBeenCalledTimes(2);

    // Verify Last-Event-ID header on reconnect
    const secondCall = api.openStream.mock.calls[1]!;
    expect(secondCall[1]).toEqual({
      headers: { "Last-Event-ID": "42" },
    });
  }, 10_000);

  it("does not retry on non-transient HTTP errors", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockRejectedValueOnce(
      new NotFoundError(404, "not found", new Headers()),
    );

    const reader = new StreamReader(api, "/stream");
    await expect(reader.read()).rejects.toThrow(NotFoundError);
    expect(api.openStream).toHaveBeenCalledOnce();
  });

  it("retries on 502 errors", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockRejectedValueOnce(
      new BadGatewayError(502, "bad gateway", new Headers()),
    );
    api.openStream.mockResolvedValueOnce(sseResponse("data: ok\n\n"));

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe("ok");
    expect(api.openStream).toHaveBeenCalledTimes(2);
  }, 10_000);

  it("gives up after max reconnects", async () => {
    const api = createMockStreamAPI();
    // 6 attempts (1 initial + 5 reconnects)
    for (let i = 0; i < 6; i++) {
      api.openStream.mockRejectedValueOnce(
        new APIConnectionError("refused"),
      );
    }

    const reader = new StreamReader(api, "/stream");
    await expect(reader.read()).rejects.toThrow(APIConnectionError);
    expect(api.openStream).toHaveBeenCalledTimes(6);
  }, 30_000);
});

describe("StreamReader with \\r\\n line endings", () => {
  it("handles Windows-style line endings", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponse("data: hello\r\n\r\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe("hello");
  });
});

describe("StreamReader SSE edge cases", () => {
  it("reconnects with event id 0 (falsy but valid)", async () => {
    const api = createMockStreamAPI();

    api.openStream.mockResolvedValueOnce(
      new Response(
        failingSSEStream(
          "id: 0\ndata: first\n\n",
          new APIConnectionError("dropped"),
        ),
        { status: 200, headers: { "Content-Type": "text/event-stream" } },
      ),
    );
    api.openStream.mockResolvedValueOnce(
      sseResponse("id: 5\ndata: second\n\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const chunks: string[] = [];
    for await (const chunk of reader) {
      chunks.push(chunk);
    }

    expect(chunks).toEqual(["first", "second"]);
    const secondCall = api.openStream.mock.calls[1]!;
    expect(secondCall[1]).toEqual({
      headers: { "Last-Event-ID": "0" },
    });
  }, 10_000);

  it("flushes trailing data not terminated by blank line", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponse("id: 0\ndata: unterminated"),
    );

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe("unterminated");
  });

  it("throws when response has no body", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      new Response(null, {
        status: 200,
        headers: { "Content-Type": "text/event-stream" },
      }),
    );

    const reader = new StreamReader(api, "/stream");
    await expect(reader.read()).rejects.toThrow("not readable");
  });

  it("ignores unknown SSE fields", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponse("event: message\nretry: 5000\ndata: hello\nid: 1\n\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe("hello");
  });

  it("strips only one leading space after colon per SSE spec", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(
      sseResponse("data:  two spaces\n\n"),
    );

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe(" two spaces");
  });

  it("handles data field with no colon", async () => {
    const api = createMockStreamAPI();
    api.openStream.mockResolvedValueOnce(sseResponse("data\n\n"));

    const reader = new StreamReader(api, "/stream");
    const content = await reader.read();
    expect(content).toBe("");
  });
});
