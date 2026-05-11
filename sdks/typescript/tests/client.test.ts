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

// Mirrors sdks/python/tests/test_client.py — same scenarios, same assertions.

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
import { HttpClient, MAX_RETRIES } from "../src/internal/http";
import { jsonResponse, makeStubFetch, sandboxResponseFixture, sseResponse, sseResponseBody } from "./_helpers";

const URL_BASE = "http://localhost:8080";

beforeEach(() => {
  vi.unstubAllEnvs();
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.useRealTimers();
});

// --- URL handling ---

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
});

// --- Error mapping ---

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
      // 502 is transient and would retry — pre-load enough responders to exhaust.
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

// --- Transport / connection errors ---

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

// --- JSON decode failures ---

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
});

// --- Retry behavior ---

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

  it("exhausts retries on transport error -> raises APIConnectionError", async () => {
    const errs = Array.from({ length: 1 + MAX_RETRIES }, () => new TypeError("connect failed"));
    const stub = makeStubFetch(...errs);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await expect(client.sandboxes.list()).rejects.toThrow(APIConnectionError);
    expect(stub.calls).toHaveLength(1 + MAX_RETRIES);
  }, 15_000);
});

// --- Replayable body retry ---

describe("body replay on retry", () => {
  it("Uint8Array (replayable) is resent on retry", async () => {
    const payload = new TextEncoder().encode('{"some":"json"}');
    const stub = makeStubFetch(
      jsonResponse({ detail: "bad gateway" }, { status: 502 }),
      jsonResponse({ sandboxes: [] }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    // Use the internal _api to directly send a Uint8Array body and check retry sees it.
    const response = await client._api.request({
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
        bodyKind: "stream",
      }),
    ).rejects.toThrow(APIConnectionError);

    // Stream bodies cannot be retried — exactly one attempt.
    expect(stub.calls).toHaveLength(1);
  });
});

// --- requestTimeoutMs validation and behavior ---

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
    // First attempt: real fetch hangs forever — abort fires per-attempt.
    // Second attempt: succeed. If timeouts didn't reset per-attempt, the
    // second attempt would also see an aborted signal at request time.
    let attempts = 0;
    const slowThenFast = async (_req: { signal: AbortSignal | undefined }): Promise<Response> => {
      attempts++;
      if (attempts === 1) {
        // Hang until aborted.
        return await new Promise<Response>((_, reject) => {
          _req.signal?.addEventListener(
            "abort",
            () => {
              reject(_req.signal?.reason ?? new DOMException("timed out", "TimeoutError"));
            },
            { once: true },
          );
        });
      }
      return jsonResponse({ sandboxes: [] });
    };
    const stub = makeStubFetch(slowThenFast, slowThenFast);
    const client = new Isola({ url: URL_BASE, requestTimeoutMs: 50, fetch: stub.fetch });
    const result = await client.sandboxes.list();
    expect(result).toEqual([]);
    expect(stub.calls).toHaveLength(2);
  }, 15_000);
});

// --- AbortSignal handling ---

describe("AbortSignal handling", () => {
  it("user-supplied signal cancels with the original signal.reason", async () => {
    const reason = new Error("user-cancelled");
    const ctrl = new AbortController();
    // Hang forever — never resolves.
    const stub = makeStubFetch(
      async (req): Promise<Response> =>
        new Promise<Response>((_, reject) => {
          req.signal?.addEventListener(
            "abort",
            () => {
              reject(req.signal?.reason ?? new DOMException("aborted", "AbortError"));
            },
            { once: true },
          );
        }),
    );
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
    const stub = makeStubFetch(
      async (req): Promise<Response> =>
        new Promise<Response>((_, reject) => {
          req.signal?.addEventListener(
            "abort",
            () => {
              reject(req.signal?.reason ?? new DOMException("aborted", "AbortError"));
            },
            { once: true },
          );
        }),
    );
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

// --- fetch injection ---

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

// --- Lifecycle ---

describe("client lifecycle", () => {
  it("await using calls close() via Symbol.asyncDispose", async () => {
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    let leaked: Isola | undefined;
    {
      await using client = new Isola({ url: URL_BASE, fetch: stub.fetch });
      leaked = client;
      await client.sandboxes.list();
      expect(client.isClosed).toBe(false);
    }
    expect(leaked.isClosed).toBe(true);
  });

  it("close() is idempotent", async () => {
    const stub = makeStubFetch();
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await client.close();
    await client.close();
    expect(client.isClosed).toBe(true);
  });
});

// --- Internal request() error pass-through ---

describe("transport error pass-through to APIConnectionError", () => {
  it("re-throws an APIError that bubbled up from a custom fetch on the last attempt", async () => {
    // Simulate a fetch that throws an APIError directly. That triggers the
    // `err instanceof APIError` short-circuit on the last attempt
    // (http.ts:177). We must exhaust retries since APIError is not detected
    // as transient at the catch level (canRetry is true for replayable
    // bodies, so retries proceed; on the last attempt the branch fires).
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
    // The original APIError must be re-thrown verbatim — NOT wrapped in
    // APIConnectionError, even though the inner catch reaches that branch.
    expect(caught).toBeInstanceOf(APIError);
    expect((caught as APIError).statusCode).toBe(502);
    expect((caught as APIError).message).toContain("from-fetch");
  }, 15_000);

  it("re-throws an APIConnectionError that bubbled up from a custom fetch", async () => {
    // Same shape but with APIConnectionError to cover the second OR branch.
    const errs = Array.from({ length: 1 + MAX_RETRIES }, () => new APIConnectionError("from-fetch"));
    const stub = makeStubFetch(...errs);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await expect(client.sandboxes.list()).rejects.toThrow(APIConnectionError);
  }, 15_000);
});

// --- fetchStream connect timeout & error pass-through ---

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
      const fetchImpl = (_input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
        return new Promise<Response>((_, reject) => {
          const sig = init.signal;
          if (sig?.aborted) {
            reject(sig.reason);
            return;
          }
          sig?.addEventListener(
            "abort",
            () => {
              reject(sig.reason);
            },
            { once: true },
          );
        });
      };
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
    const fetchImpl = (_input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
      return new Promise<Response>((_, reject) => {
        const sig = init.signal;
        if (sig?.aborted) {
          reject(sig.reason);
          return;
        }
        sig?.addEventListener(
          "abort",
          () => {
            reject(sig.reason);
          },
          { once: true },
        );
      });
    };
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

// --- Per-attempt timeout fires then a fresh attempt succeeds ---

describe("per-attempt request timeout", () => {
  it("transport timeout on the last attempt produces APIConnectionError with timeout cause path", async () => {
    // Use a really short requestTimeoutMs and a fetch that hangs forever.
    // Each attempt's AbortSignal.timeout fires; canRetry is true so we
    // retry up to MAX_RETRIES; the final attempt then takes the
    // `transportTimedOut` branch (http.ts:173-174).
    const stub = makeStubFetch(
      ...Array.from(
        { length: 1 + MAX_RETRIES },
        () =>
          async (req: { signal: AbortSignal | undefined }): Promise<Response> => {
            return await new Promise<Response>((_, reject) => {
              if (req.signal?.aborted) {
                reject(req.signal.reason);
                return;
              }
              req.signal?.addEventListener(
                "abort",
                () => {
                  reject(req.signal?.reason ?? new DOMException("timed out", "TimeoutError"));
                },
                { once: true },
              );
            });
          },
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
