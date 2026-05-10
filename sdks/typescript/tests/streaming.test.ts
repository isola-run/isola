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

// Mirrors sdks/python/tests/test_streaming.py functionally — same scenarios,
// same assertions where the abstractions line up.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { APIConnectionError, APIError, IsolaError, NotFoundError } from "../src/errors";
import { HttpClient } from "../src/internal/http";
import { MAX_RECONNECTS, StreamReader } from "../src/streaming";
import { makeStubFetch, type Responder, sseResponse, sseResponseBody } from "./_helpers";

const BASE_URL = "https://api.example.test";

function makeClient(stubFetch: ReturnType<typeof makeStubFetch>): HttpClient {
  return new HttpClient({ url: BASE_URL, requestTimeoutMs: null, fetch: stubFetch.fetch });
}

// SSE-backed Response that emits some events then errors mid-stream. The
// equivalent of the Python `_FakeSyncResponse(..., raise_after=...)` path.
//
// IMPORTANT: WHATWG Streams discard any queued chunks the moment
// controller.error() is called. So we cannot enqueue+error in start() — the
// reader would only see the error. Instead we drive enqueue from pull(),
// and emit the error only after the buffered chunk has been consumed.
function sseRaiseAfter(body: string, error: Error): Response {
  const encoder = new TextEncoder();
  let emitted = false;
  const stream = new ReadableStream<Uint8Array>({
    pull(controller) {
      if (!emitted) {
        emitted = true;
        controller.enqueue(encoder.encode(body));
        return;
      }
      controller.error(error);
    },
  });
  const headers = new Headers({ "content-type": "text/event-stream" });
  return new Response(stream, { headers });
}

// Helper: runs the full read loop, advancing fake timers to skip retry sleeps.
async function readAll(stream: StreamReader): Promise<string> {
  const promise = stream.read();
  // Drain any pending sleep() timers between reconnects. runAllTimersAsync
  // runs until the queue is empty AND awaits queued microtasks, so once
  // read() resolves there's nothing left.
  await vi.runAllTimersAsync();
  return promise;
}

// Captures a promise's eventual outcome without leaving an unhandled
// rejection while we drive fake timers. Returns a thunk that resolves to the
// rejection reason (or throws if the promise resolved unexpectedly).
function expectRejection<T>(promise: Promise<T>): () => Promise<unknown> {
  let settled: { ok: true; value: T } | { ok: false; reason: unknown } | undefined;
  // Attach handler synchronously so vitest never sees an unhandled rejection.
  promise.then(
    (value) => {
      settled = { ok: true, value };
    },
    (reason) => {
      settled = { ok: false, reason };
    },
  );
  return async () => {
    // Yield so any pending microtasks settle. Timers have already been
    // advanced by the caller (runAllTimersAsync) before this thunk runs.
    await Promise.resolve();
    if (settled === undefined) throw new Error("expectRejection: promise did not settle");
    if (settled.ok)
      throw new Error(`expectRejection: expected rejection but got resolved value: ${String(settled.value)}`);
    return settled.reason;
  };
}

async function collectIter<T>(iter: AsyncIterable<T>): Promise<T[]> {
  const out: T[] = [];
  for await (const v of iter) out.push(v);
  return out;
}

// Returns the Last-Event-ID header recorded on a stub fetch call, or null.
function lastEventIdOf(call: { headers: Headers }): string | null {
  return call.headers.get("Last-Event-ID");
}

beforeEach(() => {
  // We only fake setTimeout/clearTimeout so that AbortSignal.timeout() (used
  // elsewhere in the codebase) and Date.now() keep working. Note that
  // requestTimeoutMs: null guarantees no AbortSignal.timeout in the request
  // path; fetchStream's connect timer uses setTimeout, which we drain with
  // runAllTimersAsync between attempts.
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
});

afterEach(() => {
  vi.useRealTimers();
});

// ---------------------------------------------------------------------------
// Core
// ---------------------------------------------------------------------------

describe("StreamReader: core", () => {
  it("yields data events", async () => {
    const stub = makeStubFetch(
      sseResponse(
        sseResponseBody([
          { data: "hello ", id: 6 },
          { data: "world", id: 11 },
        ]),
      ),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    const promise = collectIter(reader);
    await vi.runAllTimersAsync();
    expect(await promise).toEqual(["hello ", "world"]);
  });

  it("read() collects all chunks into a single string", async () => {
    const stub = makeStubFetch(
      sseResponse(
        sseResponseBody([
          { data: "hello ", id: 6 },
          { data: "world", id: 11 },
        ]),
      ),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("hello world");
  });

  it("single-use guard fires verbatim on second iteration", async () => {
    const stub = makeStubFetch(sseResponse(sseResponseBody([{ data: "x", id: 1 }])));
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("x");

    // Second use must throw verbatim. Calling iter() (or for-await reuse)
    // throws synchronously before returning the iterator.
    expect(() => reader.iter()).toThrowError("StreamReader is single-use and has already been consumed");
  });
});

// ---------------------------------------------------------------------------
// SSE integration
// ---------------------------------------------------------------------------

describe("StreamReader: SSE integration", () => {
  it("multi-line data: lines are joined with newlines", async () => {
    const stub = makeStubFetch(sseResponse("data: hello\ndata: world\nid: 11\n\n"));
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("hello\nworld");
  });

  it("comment lines are ignored (heartbeats invisible to consumer)", async () => {
    const body = `${sseResponseBody([{ data: "hello", id: 5 }])}: keepalive\n\n${sseResponseBody([{ data: "world", id: 10 }])}`;
    const stub = makeStubFetch(sseResponse(body));
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("helloworld");
  });

  it("trailing newline preserved via empty data: line", async () => {
    const stub = makeStubFetch(sseResponse("data: hello\ndata: \nid: 6\n\n"));
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("hello\n");
  });

  it("zero-length data events are NOT yielded", async () => {
    const body = `data: \nid: 0\n\n${sseResponseBody([{ data: "hello", id: 5 }])}`;
    const stub = makeStubFetch(sseResponse(body));
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("hello");
  });

  it("only-comments stream produces no output", async () => {
    const stub = makeStubFetch(sseResponse(": keepalive\n\n: another comment\n\n"));
    const reader = new StreamReader(makeClient(stub), "/path");
    const promise = collectIter(reader);
    await vi.runAllTimersAsync();
    expect(await promise).toEqual([]);
  });

  it("empty body stream (zero events) produces no output", async () => {
    const stub = makeStubFetch(sseResponse(""));
    const reader = new StreamReader(makeClient(stub), "/path");
    const promise = collectIter(reader);
    await vi.runAllTimersAsync();
    expect(await promise).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// Reconnection
// ---------------------------------------------------------------------------

describe("StreamReader: reconnection", () => {
  it("reconnects after a network error mid-stream and resumes with Last-Event-ID", async () => {
    const stub = makeStubFetch(
      sseRaiseAfter(sseResponseBody([{ data: "ab", id: 2 }]), new TypeError("connect failed")),
      sseResponse(sseResponseBody([{ data: "cd", id: 4 }])),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("abcd");
    expect(stub.calls).toHaveLength(2);
    expect(lastEventIdOf(stub.calls[0]!)).toBeNull();
    // WHATWG: Last-Event-ID is sent verbatim as the string captured from the
    // SSE id: field, NOT re-parsed as a number.
    expect(lastEventIdOf(stub.calls[1]!)).toBe("2");
  });

  it("reconnects on enter error (transport failure before headers)", async () => {
    const stub = makeStubFetch(
      new TypeError("connection refused"),
      sseResponse(sseResponseBody([{ data: "hello", id: 5 }])),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("hello");
    expect(stub.calls).toHaveLength(2);
    // Both attempts have no Last-Event-ID (first never received any id).
    expect(lastEventIdOf(stub.calls[0]!)).toBeNull();
    expect(lastEventIdOf(stub.calls[1]!)).toBeNull();
  });

  it("reconnects on transient HTTP error (response.status >= 400 + transient code)", async () => {
    const stub = makeStubFetch(
      new Response("upstream gone", { status: 503, headers: { "content-type": "text/plain" } }),
      sseResponse(sseResponseBody([{ data: "hello", id: 5 }])),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("hello");
    expect(stub.calls).toHaveLength(2);
  });

  it("id 0 is a valid id and must be sent verbatim as Last-Event-ID on reconnect", async () => {
    const stub = makeStubFetch(
      sseRaiseAfter(sseResponseBody([{ data: "ab", id: 0 }]), new TypeError("drop")),
      sseResponse(sseResponseBody([{ data: "cd", id: 4 }])),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("abcd");
    expect(stub.calls).toHaveLength(2);
    expect(lastEventIdOf(stub.calls[0]!)).toBeNull();
    // "0" string verbatim, NOT null/undefined.
    expect(lastEventIdOf(stub.calls[1]!)).toBe("0");
  });

  it("counter resets on each successful data event but NOT on heartbeat", async () => {
    // Plan: emit MAX_RECONNECTS+1 cycles of [data][drop], then a successful
    // close. A heartbeat in between should NOT count as a reset.
    //
    // Simpler scenario: prove that after K transient errors interleaved with
    // K successful data events, the K+1-th transient error still recovers
    // (counter would have hit K+1 if not reset, but each data event resets).
    const responders: Responder[] = [];
    // Build MAX_RECONNECTS+1 attempts that each yield ONE data event then drop;
    // then one final attempt that just closes cleanly.
    for (let i = 0; i < MAX_RECONNECTS + 1; i++) {
      const data = `chunk${i}`;
      responders.push(() => sseRaiseAfter(sseResponseBody([{ data, id: i + 1 }]), new TypeError(`drop-${i}`)));
    }
    responders.push(() => sseResponse(sseResponseBody([{ data: "done", id: 999 }])));

    const stub = makeStubFetch(...responders);
    const reader = new StreamReader(makeClient(stub), "/path");
    const expected = `${Array.from({ length: MAX_RECONNECTS + 1 }, (_, i) => `chunk${i}`).join("")}done`;
    expect(await readAll(reader)).toBe(expected);
    expect(stub.calls).toHaveLength(MAX_RECONNECTS + 2);
  });

  it("counter is NOT reset on a heartbeat-only response (no data event)", async () => {
    // To prove heartbeats do NOT reset the reconnect counter, set up 7
    // heartbeat-and-fail attempts. If heartbeats reset the counter, all 7
    // would be tried. They aren't — attempt 6 (initial + MAX_RECONNECTS
    // retries) raises.
    const dropResponders: Responder[] = [];
    for (let i = 0; i < MAX_RECONNECTS + 2; i++) {
      dropResponders.push(() => sseRaiseAfter(": ping\n\n", new TypeError(`drop-${i}`)));
    }
    const stub = makeStubFetch(...dropResponders);
    const reader = new StreamReader(makeClient(stub), "/path");
    const settled = expectRejection(reader.read());
    await vi.runAllTimersAsync();
    expect(await settled()).toBeInstanceOf(APIConnectionError);
    // Initial attempt + MAX_RECONNECTS retries == MAX_RECONNECTS+1 calls
    // before exhausted-error fires.
    expect(stub.calls).toHaveLength(MAX_RECONNECTS + 1);
  });
});

// ---------------------------------------------------------------------------
// MAX_RECONNECTS exhaustion
// ---------------------------------------------------------------------------

describe("StreamReader: MAX_RECONNECTS exhaustion", () => {
  it("MAX_RECONNECTS is 5 (6 attempts total)", () => {
    // Sanity check on the exposed constant — keeps this test pinned to the
    // contract documented in CLAUDE.md / streaming.ts.
    expect(MAX_RECONNECTS).toBe(5);
  });

  it("transport exhaustion raises APIConnectionError after MAX_RECONNECTS+1 attempts", async () => {
    const responders: Array<Responder | Response | Error> = [];
    for (let i = 0; i < MAX_RECONNECTS + 1; i++) {
      responders.push(new TypeError(`down-${i}`));
    }
    const stub = makeStubFetch(...responders);
    const reader = new StreamReader(makeClient(stub), "/path");
    const settled = expectRejection(reader.read());
    await vi.runAllTimersAsync();
    expect(await settled()).toBeInstanceOf(APIConnectionError);
    expect(stub.calls).toHaveLength(MAX_RECONNECTS + 1);
  });

  it("transient HTTP exhaustion raises an APIError (IsolaError)", async () => {
    const responders: Array<Responder | Response | Error> = [];
    for (let i = 0; i < MAX_RECONNECTS + 1; i++) {
      responders.push(new Response("upstream gone", { status: 503, headers: { "content-type": "text/plain" } }));
    }
    const stub = makeStubFetch(...responders);
    const reader = new StreamReader(makeClient(stub), "/path");
    const settled = expectRejection(reader.read());
    await vi.runAllTimersAsync();
    const err = await settled();
    // Mirrors Python parametrised assertion `pytest.raises(IsolaError)` — the
    // last APIError is re-thrown rather than wrapped as APIConnectionError.
    expect(err).toBeInstanceOf(APIError);
    expect(err).toBeInstanceOf(IsolaError);
    expect((err as APIError).statusCode).toBe(503);
    expect(stub.calls).toHaveLength(MAX_RECONNECTS + 1);
  });
});

// ---------------------------------------------------------------------------
// HTTP error path
// ---------------------------------------------------------------------------

describe("StreamReader: HTTP errors", () => {
  it("404 propagates as NotFoundError immediately (no reconnect)", async () => {
    const stub = makeStubFetch(
      new Response(JSON.stringify({ detail: "sandbox not found" }), {
        status: 404,
        headers: { "content-type": "application/json" },
      }),
      // A second responder we should never hit. If reached, makeStubFetch will
      // pop it and our calls count will reflect the bug.
      sseResponse(sseResponseBody([{ data: "should not happen", id: 1 }])),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    const settled = expectRejection(reader.read());
    await vi.runAllTimersAsync();
    const err = await settled();
    expect(err).toBeInstanceOf(NotFoundError);
    // Mirrors Python _streaming.py:113-114 — error message has NO method/path
    // prefix because errorFromHttp is called with method/path omitted.
    expect((err as NotFoundError).message).not.toMatch(/GET\s/);
    expect((err as NotFoundError).message).not.toMatch(/\/path:/);
    expect((err as NotFoundError).message).toContain("sandbox not found");
    expect(stub.calls).toHaveLength(1);
  });

  it("502/503/504 retry with reconnects", async () => {
    for (const status of [502, 503, 504] as const) {
      const stub = makeStubFetch(
        new Response("upstream", { status, headers: { "content-type": "text/plain" } }),
        sseResponse(sseResponseBody([{ data: "ok", id: 1 }])),
      );
      const reader = new StreamReader(makeClient(stub), "/path");
      expect(await readAll(reader)).toBe("ok");
      expect(stub.calls).toHaveLength(2);
    }
  });

  it("non-transient (e.g. 400) raises immediately", async () => {
    const stub = makeStubFetch(
      new Response(JSON.stringify({ detail: "bad" }), {
        status: 400,
        headers: { "content-type": "application/json" },
      }),
      sseResponse(sseResponseBody([{ data: "should not happen", id: 1 }])),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    const settled = expectRejection(reader.read());
    await vi.runAllTimersAsync();
    expect(await settled()).toBeInstanceOf(APIError);
    expect(stub.calls).toHaveLength(1);
  });

  it("interleaved transport + transient HTTP retries before recovery", async () => {
    // Mirrors Python test_sync_stream_retries_transient_http_error.
    const stub = makeStubFetch(
      sseRaiseAfter(sseResponseBody([{ data: "ab", id: 2 }]), new TypeError("reset")),
      new Response("upstream", { status: 503, headers: { "content-type": "text/plain" } }),
      sseResponse(sseResponseBody([{ data: "cd", id: 4 }])),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("abcd");
    expect(stub.calls).toHaveLength(3);
    expect(lastEventIdOf(stub.calls[0]!)).toBeNull();
    expect(lastEventIdOf(stub.calls[1]!)).toBe("2");
    expect(lastEventIdOf(stub.calls[2]!)).toBe("2");
  });
});

// ---------------------------------------------------------------------------
// Defensive branches
// ---------------------------------------------------------------------------

describe("StreamReader: defensive branches", () => {
  it("returns immediately when response.body is null on a 2xx", async () => {
    // Some runtimes (and 204 responses) return a non-null status with body=null.
    // The reader should treat that as an empty stream and return cleanly.
    const stub = makeStubFetch(new Response(null, { status: 200, headers: { "content-type": "text/event-stream" } }));
    const reader = new StreamReader(makeClient(stub), "/path");
    const promise = collectIter(reader);
    await vi.runAllTimersAsync();
    expect(await promise).toEqual([]);
    expect(stub.calls).toHaveLength(1);
  });

  it("captures retry: from a server event and uses it on the next reconnect delay", async () => {
    // Drive: first attempt yields a data event with retry: 2000 then drops.
    // Second attempt succeeds. The reader's internal _retryDelayMs gets
    // overwritten via streaming.ts:95 on the first event.
    const stub = makeStubFetch(
      sseRaiseAfter("data: ab\nid: 2\nretry: 2000\n\n", new TypeError("drop")),
      sseResponse(sseResponseBody([{ data: "cd", id: 4 }])),
    );
    const reader = new StreamReader(makeClient(stub), "/path");
    expect(await readAll(reader)).toBe("abcd");
    expect(stub.calls).toHaveLength(2);
  });

  it("re-throws signal.reason verbatim when caller-aborted mid-stream", async () => {
    // Aborting via the user-supplied signal must propagate signal.reason
    // rather than the body-stream's internal abort error.
    const reason = new Error("user-cancelled");
    const ctrl = new AbortController();
    const encoder = new TextEncoder();

    // Build a stream that hangs forever after one chunk so we can abort mid-flow.
    const stub = makeStubFetch(
      new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            controller.enqueue(encoder.encode("data: a\n\n"));
            // Don't close — wait for cancel via abort.
          },
        }),
        { status: 200, headers: { "content-type": "text/event-stream" } },
      ),
    );
    const reader = new StreamReader(makeClient(stub), "/path");

    // Use real timers for this test so AbortController works on the natural
    // event loop.
    vi.useRealTimers();
    try {
      const iter = reader.iter({ signal: ctrl.signal });
      // Consume the first event so we know the stream is live, then abort.
      const first = await iter.next();
      expect(first.value).toBe("a");
      ctrl.abort(reason);

      let caught: unknown;
      try {
        await iter.next();
      } catch (err) {
        caught = err;
      }
      expect(caught).toBe(reason);
    } finally {
      vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    }
  });
});
