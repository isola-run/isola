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

// Mirrors sdks/python/tests/test_client.py, same scenarios, same assertions.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Isola } from "../src/client";
import {
  APIConnectionError,
  APIError,
  BadGatewayError,
  BadRequestError,
  ConflictError,
  InternalError,
  NotFoundError,
  ValidationError,
} from "../src/errors";
import { HttpClient, MAX_RETRIES, RETRY_DELAY_MS } from "../src/internal/http";
import {
  hangUntilAbort,
  jsonResponse,
  makeStubFetch,
  sandboxResponseFixture,
  sseResponse,
  sseResponseBody,
} from "./_helpers";

const URL_BASE = "http://localhost:8080";

beforeEach(() => {
  vi.unstubAllEnvs();
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.useRealTimers();
});

describe("URL handling", () => {
  it("strips trailing slash from explicit URL", () => {
    const stub = makeStubFetch();
    const client = new Isola({ url: "http://localhost:8080/", fetch: stub.fetch });
    expect(client.url).toBe("http://localhost:8080");
  });

  it("falls back to ISOLA_URL env var", () => {
    vi.stubEnv("ISOLA_URL", "http://from-env:9090");
    const client = new Isola();
    expect(client.url).toBe("http://from-env:9090");
  });

  it("explicit URL overrides env var", () => {
    vi.stubEnv("ISOLA_URL", "http://from-env:9090");
    const client = new Isola({ url: "http://explicit:8080" });
    expect(client.url).toBe("http://explicit:8080");
  });

  it("throws helpful error when no URL is provided", () => {
    vi.stubEnv("ISOLA_URL", "");
    expect(() => new Isola()).toThrow(/ISOLA_URL/);
  });

  it("throws when explicit URL is empty", () => {
    expect(() => new Isola({ url: "" })).toThrow(/ISOLA_URL/);
  });

  it("throws when process.env is undefined and no URL provided", () => {
    // Some runtimes (Workers, edge) don't expose process.env. The env-var
    // lookup must guard against that and still surface the canonical
    // 'ISOLA_URL' error.
    const originalEnv = process.env;
    // @ts-expect-error - intentionally removing env to exercise the branch.
    process.env = undefined;
    try {
      expect(() => new Isola()).toThrow(/ISOLA_URL/);
    } finally {
      process.env = originalEnv;
    }
  });
});

describe("User-Agent header", () => {
  it("does not inject a custom User-Agent (aligns with the Python SDK)", async () => {
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await client.sandboxes.list();
    expect(stub.calls[0]!.headers.get("user-agent")).toBeNull();
  });

  it("passes through a caller-provided User-Agent header", async () => {
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
    await api.request({
      method: "GET",
      path: "/v1/sandboxes",
      headers: { "user-agent": "my-cli/1.0" },
    });
    expect(stub.calls[0]!.headers.get("user-agent")).toBe("my-cli/1.0");
  });
});

describe("error mapping", () => {
  const cases: Array<[number, new (...args: never[]) => APIError]> = [
    [400, BadRequestError],
    [404, NotFoundError],
    [409, ConflictError],
    [422, ValidationError],
    [500, InternalError],
    [502, BadGatewayError],
  ];

  for (const [status, Ctor] of cases) {
    it(`maps ${status} -> ${Ctor.name} with statusCode and detail`, async () => {
      // 502 is transient and would retry, pre-load enough responders to exhaust.
      const failures = Array.from({ length: 1 + MAX_RETRIES }, () =>
        jsonResponse({ status, detail: "test error" }, { status }),
      );
      const stub = makeStubFetch(...failures);
      const client = new Isola({ url: URL_BASE, fetch: stub.fetch });

      let caught: unknown;
      try {
        await client.sandboxes.get("bad");
      } catch (err) {
        caught = err;
      }
      expect(caught).toBeInstanceOf(Ctor);
      const apiErr = caught as APIError;
      expect(apiErr.statusCode).toBe(status);
      expect(apiErr.message).toContain("test error");
    }, 15_000);
  }

  const fallthrough = [401, 403, 503, 504];
  for (const status of fallthrough) {
    it(`falls through ${status} to plain APIError`, async () => {
      const failures = Array.from({ length: 1 + MAX_RETRIES }, () =>
        jsonResponse({ detail: "fallthrough" }, { status }),
      );
      const stub = makeStubFetch(...failures);
      const client = new Isola({ url: URL_BASE, fetch: stub.fetch });

      let caught: unknown;
      try {
        await client.sandboxes.get("bad");
      } catch (err) {
        caught = err;
      }
      expect(caught).toBeInstanceOf(APIError);
      expect((caught as { constructor: unknown }).constructor).toBe(APIError);
      expect((caught as APIError).statusCode).toBe(status);
    }, 15_000);
  }
});

describe("transport error mapping", () => {
  it("wraps transport errors as APIConnectionError with method/path prefix", async () => {
    const transportErr = new TypeError("connect failed");
    const errs = Array.from({ length: 1 + MAX_RETRIES }, () => transportErr);
    const stub = makeStubFetch(...errs);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });

    let caught: unknown;
    try {
      await client.sandboxes.list();
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIConnectionError);
    expect((caught as Error).message).toContain("GET /v1/sandboxes:");
    expect((caught as Error).message).toContain("connect failed");
  }, 15_000);
});

describe("response decoding failures", () => {
  it("invalid JSON body raises APIError with 200: invalid response payload", async () => {
    const stub = makeStubFetch(
      new Response("<html>not json</html>", {
        status: 200,
        headers: { "content-type": "text/html" },
      }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });

    let caught: unknown;
    try {
      await client.sandboxes.get("sandbox-123");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIError);
    expect((caught as APIError).message).toBe("200: invalid response payload");
  });

  it("schema mismatch raises APIError with 200: invalid response payload", async () => {
    const stub = makeStubFetch(jsonResponse({ unexpected: "schema" }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });

    let caught: unknown;
    try {
      await client.sandboxes.get("sandbox-123");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIError);
    expect((caught as APIError).message).toBe("200: invalid response payload");
  });

  it("user abort during response body read surfaces signal.reason, not 'invalid response payload'", async () => {
    // Body parking on a never-closed ReadableStream. While request() buffers
    // the body via arrayBuffer(), the user aborts. The catch must see
    // signal.aborted and throw signal.reason verbatim, NOT wrap it as
    // APIError("invalid response payload") or APIConnectionError, which
    // would mask a deliberate cancellation as a server/transport fault.
    const reason = new Error("user-abort-during-body-parse");
    const ctrl = new AbortController();

    // Build a Response whose body never closes and rejects (via the request
    // init signal) when the caller aborts.
    const fetchImpl = (_input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
      const sig = init.signal;
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          // Enqueue partial JSON so json() must keep reading.
          controller.enqueue(new TextEncoder().encode('{"partial":'));
          // Hook the signal: when it aborts, error the stream with the reason
          // so response.json() rejects.
          sig?.addEventListener(
            "abort",
            () => {
              controller.error(sig.reason);
            },
            { once: true },
          );
        },
      });
      return Promise.resolve(new Response(body, { status: 200, headers: { "content-type": "application/json" } }));
    };
    const client = new Isola({ url: URL_BASE, fetch: fetchImpl, requestTimeoutMs: null });

    // Fire the abort one microtask later so request() parks on
    // await response.arrayBuffer() before the abort fires.
    queueMicrotask(() => ctrl.abort(reason));

    let caught: unknown;
    try {
      await client.sandboxes.get("sandbox-123", { signal: ctrl.signal });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(reason);
  });
});

describe("retry behavior", () => {
  it("retries 502 then succeeds", async () => {
    const stub = makeStubFetch(
      jsonResponse({ detail: "bad gateway" }, { status: 502 }),
      jsonResponse({ sandboxes: [] }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    const result = await client.sandboxes.list();
    expect(result).toEqual([]);
    expect(stub.calls).toHaveLength(2);
  }, 15_000);

  it("retries 503 then succeeds", async () => {
    const stub = makeStubFetch(
      jsonResponse({ detail: "unavailable" }, { status: 503 }),
      jsonResponse({ sandboxes: [] }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    const result = await client.sandboxes.list();
    expect(result).toEqual([]);
    expect(stub.calls).toHaveLength(2);
  }, 15_000);

  it("retries 504 then succeeds", async () => {
    const stub = makeStubFetch(jsonResponse({ detail: "timeout" }, { status: 504 }), jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    const result = await client.sandboxes.list();
    expect(result).toEqual([]);
    expect(stub.calls).toHaveLength(2);
  }, 15_000);

  it("retries transport (APIConnectionError) then succeeds", async () => {
    const stub = makeStubFetch(new TypeError("connect failed"), jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    const result = await client.sandboxes.list();
    expect(result).toEqual([]);
    expect(stub.calls).toHaveLength(2);
  }, 15_000);

  it("does NOT retry on 400", async () => {
    const stub = makeStubFetch(jsonResponse({ detail: "bad request" }, { status: 400 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await expect(client.sandboxes.get("bad")).rejects.toThrow(BadRequestError);
    expect(stub.calls).toHaveLength(1);
  });

  it("does NOT retry on 404", async () => {
    const stub = makeStubFetch(jsonResponse({ detail: "not found" }, { status: 404 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await expect(client.sandboxes.get("bad")).rejects.toThrow(NotFoundError);
    expect(stub.calls).toHaveLength(1);
  });

  it("exhausts retries on 502 -> raises BadGatewayError after 1+MAX_RETRIES attempts", async () => {
    const failures = Array.from({ length: 1 + MAX_RETRIES }, () =>
      jsonResponse({ detail: "bad gateway" }, { status: 502 }),
    );
    const stub = makeStubFetch(...failures);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await expect(client.sandboxes.list()).rejects.toThrow(BadGatewayError);
    expect(stub.calls).toHaveLength(1 + MAX_RETRIES);
  }, 15_000);

  it("waits exactly RETRY_DELAY_MS between attempts (README-pinned 1s)", async () => {
    // Pin the README's "fixed 1s delay between attempts" promise. Without
    // this, a regression to backoff/jitter or to a different constant would
    // pass the attempt-count tests but break the documented behavior.
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const stub = makeStubFetch(
        jsonResponse({ detail: "bad gateway" }, { status: 502 }),
        jsonResponse({ sandboxes: [] }),
      );
      const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
      const promise = client.sandboxes.list();

      // After the first attempt the SDK should have scheduled a sleep of
      // exactly RETRY_DELAY_MS before the second. Advance N-1 ms, no second
      // call yet, then advance the last 1ms and assert it fired.
      await vi.advanceTimersByTimeAsync(RETRY_DELAY_MS - 1);
      expect(stub.calls).toHaveLength(1);
      await vi.advanceTimersByTimeAsync(1);
      await promise;
      expect(stub.calls).toHaveLength(2);
    } finally {
      vi.useRealTimers();
    }
  });

  it("exhausts retries on transport error -> raises APIConnectionError", async () => {
    const errs = Array.from({ length: 1 + MAX_RETRIES }, () => new TypeError("connect failed"));
    const stub = makeStubFetch(...errs);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await expect(client.sandboxes.list()).rejects.toThrow(APIConnectionError);
    expect(stub.calls).toHaveLength(1 + MAX_RETRIES);
  }, 15_000);
});

describe("body replay on retry", () => {
  it("Uint8Array (replayable) is resent on retry", async () => {
    const payload = new TextEncoder().encode('{"some":"json"}');
    const stub = makeStubFetch(
      jsonResponse({ detail: "bad gateway" }, { status: 502 }),
      jsonResponse({ sandboxes: [] }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    // Use the internal _api to directly send a Uint8Array body and check retry sees it.
    const { response } = await client._api.request({
      method: "POST",
      path: "/v1/sandboxes",
      body: payload,
      headers: { "content-type": "application/json" },
    });
    expect(response.status).toBe(200);
    expect(stub.calls).toHaveLength(2);
    expect(stub.calls[0]?.bodyText).toBe('{"some":"json"}');
    expect(stub.calls[1]?.bodyText).toBe('{"some":"json"}');
  }, 15_000);

  // Every non-stream body is held in memory and replayable, so typed-array
  // views (Int32Array, DataView, etc.) must be resent on a transient retry.
  it.each([
    { label: "Int32Array", make: (): BodyInit => new Int32Array([1, 2, 3]) as unknown as BodyInit },
    { label: "DataView", make: (): BodyInit => new DataView(new ArrayBuffer(8)) as unknown as BodyInit },
  ])(
    "typed-array body ($label) is replayable and retried on 502",
    async ({ make }) => {
      const stub = makeStubFetch(jsonResponse({ detail: "bad gateway" }, { status: 502 }), jsonResponse({ ok: true }));
      const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
      await api.request({
        method: "POST",
        path: "/v1/sandboxes",
        body: make(),
        headers: { "content-type": "application/octet-stream" },
      });
      expect(stub.calls).toHaveLength(2);
    },
    15_000,
  );

  it("ReadableStream (non-replayable) is NOT retried after transport error", async () => {
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("chunk"));
        controller.close();
      },
    });
    const errs = Array.from({ length: 1 + MAX_RETRIES }, () => new TypeError("connect failed"));
    const stub = makeStubFetch(...errs);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });

    await expect(
      client._api.request({
        method: "POST",
        path: "/v1/sandboxes",
        body: stream,
      }),
    ).rejects.toThrow(APIConnectionError);

    // Stream bodies cannot be retried, exactly one attempt.
    expect(stub.calls).toHaveLength(1);
  });
});

describe("requestTimeoutMs", () => {
  it("rejects negative values at construction", () => {
    expect(() => new Isola({ url: URL_BASE, requestTimeoutMs: -1 })).toThrow(TypeError);
  });

  it("rejects zero at construction", () => {
    expect(() => new Isola({ url: URL_BASE, requestTimeoutMs: 0 })).toThrow(TypeError);
  });

  it("rejects NaN at construction", () => {
    expect(() => new Isola({ url: URL_BASE, requestTimeoutMs: Number.NaN })).toThrow(TypeError);
  });

  it("rejects Infinity at construction", () => {
    expect(() => new Isola({ url: URL_BASE, requestTimeoutMs: Number.POSITIVE_INFINITY })).toThrow(TypeError);
  });

  it("null disables the timeout", () => {
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
    expect(client._api.requestTimeoutMs).toBeNull();
  });

  it("undefined uses the default 30_000ms", () => {
    const stub = makeStubFetch();
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    expect(client._api.requestTimeoutMs).toBe(30_000);
  });

  it("each retry attempt gets a fresh timeout budget", async () => {
    // First attempt: real fetch hangs forever, abort fires per-attempt.
    // Second attempt: succeed. If timeouts didn't reset per-attempt, the
    // second attempt would also see an aborted signal at request time.
    let attempts = 0;
    const slowThenFast = async (req: { signal: AbortSignal | undefined }): Promise<Response> => {
      attempts++;
      if (attempts === 1) return hangUntilAbort(req);
      return jsonResponse({ sandboxes: [] });
    };
    const stub = makeStubFetch(slowThenFast, slowThenFast);
    const client = new Isola({ url: URL_BASE, requestTimeoutMs: 50, fetch: stub.fetch });
    const result = await client.sandboxes.list();
    expect(result).toEqual([]);
    expect(stub.calls).toHaveLength(2);
  }, 15_000);
});

describe("AbortSignal handling", () => {
  it("user-supplied signal cancels with the original signal.reason", async () => {
    const reason = new Error("user-cancelled");
    const ctrl = new AbortController();
    // Hang forever, never resolves.
    const stub = makeStubFetch((req): Promise<Response> => hangUntilAbort(req));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });

    const promise = client.sandboxes.list({ signal: ctrl.signal });
    setTimeout(() => ctrl.abort(reason), 10);

    let caught: unknown;
    try {
      await promise;
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(reason);
  });

  it("user-cancel-during-fetch wins over internal timeout", async () => {
    const reason = new Error("user-wins");
    const ctrl = new AbortController();
    const stub = makeStubFetch((req): Promise<Response> => hangUntilAbort(req));
    const client = new Isola({ url: URL_BASE, requestTimeoutMs: 5_000, fetch: stub.fetch });

    const promise = client.sandboxes.list({ signal: ctrl.signal });
    setTimeout(() => ctrl.abort(reason), 10);

    let caught: unknown;
    try {
      await promise;
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(reason);
  });

  it("pre-aborted signal throws original reason without making any fetch call", async () => {
    const reason = new Error("pre-aborted");
    const stub = makeStubFetch();
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    const ctrl = new AbortController();
    ctrl.abort(reason);

    let caught: unknown;
    try {
      await client.sandboxes.list({ signal: ctrl.signal });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(reason);
    expect(stub.calls).toHaveLength(0);
  });
});

describe("fetch injection", () => {
  it("uses the supplied fetch implementation", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    const sandbox = await client.sandboxes.get("sandbox-123");
    expect(sandbox.id).toBe("sandbox-123");
    expect(stub.calls).toHaveLength(1);
    expect(stub.calls[0]?.url).toBe("http://localhost:8080/v1/sandboxes/sandbox-123");
    expect(stub.calls[0]?.method).toBe("GET");
  });
});

describe("transport error pass-through to APIConnectionError", () => {
  it("re-throws an APIError(502) from a custom fetch verbatim after retries exhaust", async () => {
    // A custom fetch throwing an APIError(502) goes through the SDK-typed
    // retry branch: isTransient(err) is true for 502, so the SDK retries up to
    // MAX_RETRIES. On the final attempt the branch falls through and re-throws
    // the original error verbatim (no wrap into APIConnectionError).
    const errs = Array.from(
      { length: 1 + MAX_RETRIES },
      () => new APIError({ statusCode: 502, message: "from-fetch" }),
    );
    const stub = makeStubFetch(...errs);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });

    let caught: unknown;
    try {
      await client.sandboxes.list();
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIError);
    expect((caught as APIError).statusCode).toBe(502);
    expect((caught as APIError).message).toContain("from-fetch");
    // A regression that bypasses the SDK-typed retry would either wrap as
    // APIConnectionError or stop after 1 attempt.
    expect(stub.calls.length).toBe(1 + MAX_RETRIES);
  }, 15_000);

  it("re-throws an APIConnectionError that bubbled up from a custom fetch", async () => {
    // Same shape but with APIConnectionError to cover the second OR branch.
    const errs = Array.from({ length: 1 + MAX_RETRIES }, () => new APIConnectionError("from-fetch"));
    const stub = makeStubFetch(...errs);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await expect(client.sandboxes.list()).rejects.toThrow(APIConnectionError);
    expect(stub.calls.length).toBe(1 + MAX_RETRIES);
  }, 15_000);

  it("re-throws non-transient APIError from a custom fetch with NO retry", async () => {
    // SDK-typed errors thrown by a custom fetch pass through the same
    // isTransient gate as response-status errors. A 400 (non-transient)
    // must NOT retry, exactly one attempt, original error verbatim.
    const stub = makeStubFetch(new BadRequestError({ statusCode: 400, message: "from-fetch-400" }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });

    let caught: unknown;
    try {
      await client.sandboxes.list();
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(BadRequestError);
    expect((caught as BadRequestError).statusCode).toBe(400);
    expect((caught as BadRequestError).message).toContain("from-fetch-400");
    expect(stub.calls.length).toBe(1);
  });

  it("fetchStream passes SDK-typed errors verbatim (no double-wrap)", async () => {
    // fetchStream's catch wraps native TypeError/AbortError as
    // APIConnectionError, but an SDK-typed error from
    // a custom fetch should pass through unchanged, no re-wrap chain.
    const sdkErr = new BadRequestError({ statusCode: 400, message: "from-fetch-stream" });
    const stub = makeStubFetch(sdkErr);
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });

    let caught: unknown;
    try {
      await api.fetchStream("/path");
    } catch (err) {
      caught = err;
    }
    // Reference-equality: the exact same error object, not a wrapped copy.
    expect(caught).toBe(sdkErr);
  });
});

describe("HttpClient.fetchStream", () => {
  it("returns the response when the headers arrive before the connect timeout", async () => {
    // Sanity check: a successful fast response should clear the connect timer.
    const stub = makeStubFetch(sseResponse(sseResponseBody([{ data: "x", id: 1 }])));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
    const response = await api.fetchStream("/path");
    expect(response.status).toBe(200);
    // Drain the body so the stream finalizer doesn't leak.
    await response.body?.cancel();
  });

  it("wraps connect timeout as APIConnectionError after the 5s timer fires", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      // fetch hangs forever, but as soon as the connect controller aborts the
      // signal we reject with the abort reason. fetchStream then translates
      // that into APIConnectionError because connectTimedOut === true.
      const fetchImpl = (_input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> =>
        hangUntilAbort({ signal: init.signal ?? undefined });
      const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: fetchImpl });

      const promise = api.fetchStream("/path");
      // Catch the rejection synchronously to avoid an unhandled rejection.
      let caught: unknown;
      const settled = promise.catch((err) => {
        caught = err;
      });
      // Advance past the 5_000ms STREAM_CONNECT_TIMEOUT_MS.
      await vi.advanceTimersByTimeAsync(6_000);
      await settled;

      expect(caught).toBeInstanceOf(APIConnectionError);
      expect((caught as Error).message).toContain("GET /path:");
    } finally {
      vi.useRealTimers();
    }
  });

  it("propagates user signal.reason verbatim when the user aborts during connect", async () => {
    const reason = new Error("user-abort");
    const ctrl = new AbortController();
    const fetchImpl = (_input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> =>
      hangUntilAbort({ signal: init.signal ?? undefined });
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: fetchImpl });

    const promise = api.fetchStream("/path", { signal: ctrl.signal });
    setTimeout(() => ctrl.abort(reason), 5);

    let caught: unknown;
    try {
      await promise;
    } catch (err) {
      caught = err;
    }
    // User signal short-circuits before the connect-timeout branch.
    expect(caught).toBe(reason);
  });

  it("wraps native TypeError transport errors as APIConnectionError", async () => {
    // fetchStream catches transport-shaped errors (TypeError from native fetch
    // on DNS/TLS/connect failure) and wraps them as APIConnectionError so the
    // StreamReader retry loop can classify them via isTransient().
    const transportErr = new TypeError("connect failed");
    const stub = makeStubFetch(transportErr);
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });

    let caught: unknown;
    try {
      await api.fetchStream("/path");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIConnectionError);
    expect((caught as Error).message).toContain("GET /path");
    expect((caught as Error).cause).toBe(transportErr);
  });

  it("re-throws unknown error shapes verbatim", async () => {
    // Non-transport, non-abort, non-SDK errors propagate as-is. A user-supplied
    // fetch throwing RangeError should not be silently classified.
    const weirdErr = new RangeError("unexpected");
    const stub = makeStubFetch(weirdErr);
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });

    let caught: unknown;
    try {
      await api.fetchStream("/path");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(weirdErr);
  });

  it("rejects synchronously when the supplied signal is already aborted", async () => {
    const reason = new Error("already-aborted");
    const ctrl = new AbortController();
    ctrl.abort(reason);
    const stub = makeStubFetch();
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });

    let caught: unknown;
    try {
      await api.fetchStream("/path", { signal: ctrl.signal });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(reason);
    expect(stub.calls).toHaveLength(0);
  });
});

describe("per-attempt request timeout", () => {
  it("transport timeout on the last attempt produces APIConnectionError with timeout cause path", async () => {
    // Use a really short requestTimeoutMs and a fetch that hangs forever.
    // Each attempt's AbortSignal.timeout fires; canRetry is true so we
    // retry up to MAX_RETRIES; the final attempt then takes the
    // `transportTimedOut` branch.
    const stub = makeStubFetch(
      ...Array.from(
        { length: 1 + MAX_RETRIES },
        () => (req: { signal: AbortSignal | undefined }) => hangUntilAbort(req),
      ),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: 20 });

    let caught: unknown;
    try {
      await client.sandboxes.list();
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIConnectionError);
    // Method/path prefix from the timeout-out branch.
    expect((caught as Error).message).toContain("GET /v1/sandboxes:");
    // All MAX_RETRIES+1 attempts attempted.
    expect(stub.calls.length).toBe(1 + MAX_RETRIES);
  }, 15_000);
});

describe("response body read failures", () => {
  // Build a Response whose body errors after enqueueing partial bytes,
  // simulating a connection drop / chunked-encoding error after headers
  // arrived. fetch() resolves; arrayBuffer()/json() reject.
  function bodyErroringResponse(
    err: unknown,
    init: ResponseInit = { status: 200, headers: { "content-type": "application/json" } },
  ): Response {
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("{"));
        controller.error(err);
      },
    });
    return new Response(stream, init);
  }

  it("requestBytes retries when response body errors mid-read, then succeeds", async () => {
    const stub = makeStubFetch(
      bodyErroringResponse(new TypeError("connection reset")),
      new Response(new Uint8Array([1, 2, 3])),
    );
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
    const bytes = await api.requestBytes({ method: "GET", path: "/v1/x" });
    expect(Array.from(bytes)).toEqual([1, 2, 3]);
    expect(stub.calls).toHaveLength(2);
  }, 15_000);

  it("requestModel retries when response body errors mid-read, then succeeds", async () => {
    const stub = makeStubFetch(bodyErroringResponse(new TypeError("connection reset")), jsonResponse({ ok: true }));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
    const result = await api.requestModel<unknown>({ method: "GET", path: "/v1/x" }, (json) => json);
    expect(result).toEqual({ ok: true });
    expect(stub.calls).toHaveLength(2);
  }, 15_000);

  it("requestBytes wraps exhausted body-read failures as APIConnectionError", async () => {
    const stub = makeStubFetch(
      ...Array.from({ length: 1 + MAX_RETRIES }, () => bodyErroringResponse(new TypeError("connection reset"))),
    );
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });

    let caught: unknown;
    try {
      await api.requestBytes({ method: "GET", path: "/v1/x" });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIConnectionError);
    expect((caught as Error).message).toContain("GET /v1/x:");
    expect(stub.calls).toHaveLength(1 + MAX_RETRIES);
  }, 15_000);

  it("requestModel wraps exhausted body-read failures as APIConnectionError (NOT 'invalid response payload')", async () => {
    // The bug being fixed: before, a transport-shaped body-read failure
    // surfaced as APIError(200, "invalid response payload"), non-transient,
    // unretryable, and indistinguishable from server-side malformed JSON.
    // After the fix, it must surface as the retryable APIConnectionError.
    const stub = makeStubFetch(
      ...Array.from({ length: 1 + MAX_RETRIES }, () => bodyErroringResponse(new TypeError("connection reset"))),
    );
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });

    let caught: unknown;
    try {
      await api.requestModel<unknown>({ method: "GET", path: "/v1/x" }, (json) => json);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIConnectionError);
    expect((caught as Error).message).toContain("GET /v1/x:");
    expect(stub.calls).toHaveLength(1 + MAX_RETRIES);
  }, 15_000);

  it("requestModel still raises APIError for actually-malformed JSON (no retry)", async () => {
    // Regression guard for the fix: when the body arrives intact but the
    // payload itself isn't JSON, that's a server-side fault, not a transport
    // one, retain APIError("invalid response payload") and do NOT retry.
    const stub = makeStubFetch(
      new Response("<html>not json</html>", { status: 200, headers: { "content-type": "text/html" } }),
    );
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });

    let caught: unknown;
    try {
      await api.requestModel<unknown>({ method: "GET", path: "/v1/x" }, (json) => json);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIError);
    expect(caught).not.toBeInstanceOf(APIConnectionError);
    expect((caught as APIError).message).toBe("200: invalid response payload");
    expect(stub.calls).toHaveLength(1);
  });

  it("non-transport body-read error is wrapped as APIConnectionError without retry", async () => {
    // Mirrors the headers-phase contract: a non-transport thrown value (e.g.
    // RangeError) gets wrapped as APIConnectionError once, not retried.
    // Stream errors propagate through arrayBuffer() verbatim, so this also
    // protects against future regressions where the runtime might wrap stream
    // errors as TypeError and silently re-enable retry.
    const stub = makeStubFetch(bodyErroringResponse(new RangeError("unexpected")));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });

    let caught: unknown;
    try {
      await api.requestBytes({ method: "GET", path: "/v1/x" });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIConnectionError);
    expect(stub.calls).toHaveLength(1);
  });

  it("non-replayable body (stream) does NOT retry on body-read failure", async () => {
    const requestStream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("chunk"));
        controller.close();
      },
    });
    const stub = makeStubFetch(bodyErroringResponse(new TypeError("connection reset")));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });

    let caught: unknown;
    try {
      await api.requestBytes({
        method: "POST",
        path: "/v1/x",
        body: requestStream,
      });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIConnectionError);
    expect(stub.calls).toHaveLength(1);
  });

  it("per-attempt timeout firing during body read wraps as APIConnectionError after retries", async () => {
    // Body never produces bytes; only the per-attempt AbortSignal.timeout
    // fires. Each attempt's abort surfaces a TimeoutError DOMException;
    // canRetry is true so the SDK retries up to MAX_RETRIES; the final
    // attempt wraps the timeout as APIConnectionError.
    const fetchImpl = (_input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
      const sig = init.signal;
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          sig?.addEventListener(
            "abort",
            () => {
              controller.error(sig.reason);
            },
            { once: true },
          );
        },
      });
      return Promise.resolve(new Response(body, { status: 200 }));
    };
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: 20, fetch: fetchImpl });

    let caught: unknown;
    try {
      await api.requestBytes({ method: "GET", path: "/v1/x" });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(APIConnectionError);
    expect((caught as Error).message).toContain("GET /v1/x:");
  }, 15_000);

  it("user abort during requestBytes body read surfaces signal.reason verbatim", async () => {
    // Counterpart to the requestModel user-abort test above, for the
    // requestBytes / filesystem.read path. User aborts while arrayBuffer()
    // is reading; signal.reason must propagate, not a wrapped APIConnectionError.
    const reason = new Error("user-abort-during-body-read");
    const ctrl = new AbortController();
    const fetchImpl = (_input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
      const sig = init.signal;
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          controller.enqueue(new TextEncoder().encode("{"));
          sig?.addEventListener(
            "abort",
            () => {
              controller.error(sig.reason);
            },
            { once: true },
          );
        },
      });
      return Promise.resolve(
        new Response(body, { status: 200, headers: { "content-type": "application/octet-stream" } }),
      );
    };
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: fetchImpl });

    queueMicrotask(() => ctrl.abort(reason));

    let caught: unknown;
    try {
      await api.requestBytes({ method: "GET", path: "/v1/x", signal: ctrl.signal });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(reason);
  });
});
