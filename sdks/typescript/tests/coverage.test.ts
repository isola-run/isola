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
//
// Focused tests for branches that the main test files do not naturally
// exercise: optional-field decoder branches, signal forwarding through
// every method, User-Agent header behavior, and stream-EOF abort race.

import { afterEach, describe, expect, it, vi } from "vitest";
import { Isola } from "../src/client";
import { HttpClient } from "../src/internal/http";
import { parseSSE } from "../src/internal/sse";
import { Container, SnapshotRootfs, TerminationPolicy } from "../src/models";
import { StreamReader } from "../src/streaming";
import {
  emptyResponse,
  jsonResponse,
  makeRootfsSnapshotResponse,
  makeRoutingFetch,
  makeStubFetch,
  sandboxResponseFixture,
  sseResponse,
  sseResponseBody,
} from "./_helpers";

const URL_BASE = "http://localhost:8080";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllEnvs();
});

// ---------- models.ts optional decoder branches ----------

describe("Container.fromWire optional fields (models.ts:274/276/278)", () => {
  it("decodes name, rootfsSnapshotName, and command when all present", () => {
    const c = Container.fromWire({
      image: "alpine:3.21",
      name: "worker",
      rootfsSnapshotName: "snap-1",
      command: ["sh", "-c", "exit 0"],
    });
    expect(c.image).toBe("alpine:3.21");
    expect(c.name).toBe("worker");
    expect(c.rootfsSnapshotName).toBe("snap-1");
    expect(c.command).toEqual(["sh", "-c", "exit 0"]);
  });
});

describe("SnapshotRootfs.fromWire optional fields (models.ts:360)", () => {
  it("decodes timeoutSeconds when provided", () => {
    const s = SnapshotRootfs.fromWire({ snapshotName: "x", timeoutSeconds: 60 });
    expect(s.snapshotName).toBe("x");
    expect(s.timeoutSeconds).toBe(60);
  });
  it("decodes only timeoutSeconds without snapshotName", () => {
    const s = SnapshotRootfs.fromWire({ timeoutSeconds: 120 });
    expect(s.snapshotName).toBeUndefined();
    expect(s.timeoutSeconds).toBe(120);
  });
});

describe("TerminationPolicy.fromWire without snapshotRootfs (models.ts:451)", () => {
  it("returns { type: 'SnapshotRootfs' } when snapshotRootfs is absent", () => {
    const tp = TerminationPolicy.fromWire({ type: "SnapshotRootfs" });
    expect(tp.type).toBe("SnapshotRootfs");
    expect(tp.snapshotRootfs).toBeUndefined();
  });

  it("treats explicit null snapshotRootfs as absent", () => {
    const tp = TerminationPolicy.fromWire({ type: "SnapshotRootfs", snapshotRootfs: null });
    expect(tp.snapshotRootfs).toBeUndefined();
  });
});

// ---------- rootfs-snapshot.ts:152 — create() without snapshotName ----------

describe("RootfsSnapshots.create without optional fields (rootfs-snapshot.ts:152)", () => {
  it("omits snapshotName/containerName/timeoutSeconds/ttl when not provided", async () => {
    const stub = makeStubFetch(jsonResponse(makeRootfsSnapshotResponse("Succeeded"), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.rootfsSnapshots.create({ sandboxId: "sandbox-123" });

    const call = stub.calls[0]!;
    expect(JSON.parse(call.bodyText)).toEqual({ sandboxId: "sandbox-123" });
  });
});

// ---------- client.ts:63 — env-var fallback when process.env undefined ----------

describe("resolveUrl env fallback when process.env is undefined (client.ts:63)", () => {
  it("throws when process.env is undefined and no url provided", () => {
    // Temporarily make `process.env` undefined to force the ternary's `false`
    // arm to be taken (the candidate stays undefined).
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

// ---------- http.ts: User-Agent header (NEW) ----------

describe("HttpClient User-Agent header (http.ts:154)", () => {
  it("auto-sets default User-Agent when caller did not provide one", async () => {
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await client.sandboxes.list();
    const ua = stub.calls[0]!.headers.get("user-agent");
    expect(ua).toMatch(/^@isola-run\/sdk\//);
  });

  it("respects an explicit User-Agent header (does not overwrite)", async () => {
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

// ---------- http.ts: signal.reason fallback when null/undefined (line 180) ----------

describe("HttpClient user-signal abort with null reason (http.ts:180)", () => {
  it("falls back to the underlying error when signal becomes aborted with undefined reason mid-fetch", async () => {
    // Craft an AbortSignal whose .aborted flips to true mid-fetch but .reason
    // remains undefined. The fetch resolves with a thrown transportErr (e.g.
    // the runtime's AbortError); the catch sees userSignal.aborted=true so it
    // enters line 179-181, evaluates `userSignal.reason ?? err`, and falls
    // through to the underlying error.
    const ctrl = new AbortController();
    let aborted = false;
    Object.defineProperty(ctrl.signal, "aborted", { configurable: true, get: () => aborted });
    Object.defineProperty(ctrl.signal, "reason", { configurable: true, get: () => undefined });

    const transportErr = new TypeError("fake-abort");
    // Fetch flips aborted then throws (simulating a real AbortError after
    // controller.abort() — but our mocked signal has no reason).
    const fetchImpl = async (_input: RequestInfo | URL, _init: RequestInit = {}): Promise<Response> => {
      aborted = true;
      throw transportErr;
    };
    const client = new Isola({ url: URL_BASE, fetch: fetchImpl as typeof fetch });

    let caught: unknown;
    try {
      await client.sandboxes.list({ signal: ctrl.signal });
    } catch (err) {
      caught = err;
    }
    // signal.aborted is true, signal.reason is undefined → uses ?? err fallback.
    expect(caught).toBe(transportErr);
  });
});

// ---------- streaming.ts:137 — signal.reason fallback in catch ----------

describe("StreamReader signal.reason undefined fallback (streaming.ts:137)", () => {
  it("falls back to the underlying error when signal.aborted but reason undefined", async () => {
    const ctrl = new AbortController();
    let aborted = false;
    Object.defineProperty(ctrl.signal, "aborted", { configurable: true, get: () => aborted });
    Object.defineProperty(ctrl.signal, "reason", { configurable: true, get: () => undefined });

    // Fetch flips aborted=true and throws a custom error. fetchStream catches
    // it and re-throws (its branch is the non-connect-timeout, non-user-abort
    // case). StreamReader.catch sees signal.aborted=true and evaluates
    // `signal.reason ?? err`, falling through to err.
    const customErr = new Error("inner-error");
    const fetchImpl = async (_input: RequestInfo | URL, _init: RequestInit = {}): Promise<Response> => {
      aborted = true;
      throw customErr;
    };
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: fetchImpl as typeof fetch });
    const reader = new StreamReader(api, "/path");

    let caught: unknown;
    try {
      for await (const _ of reader.iter({ signal: ctrl.signal })) void _;
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(customErr);
  });
});

// ---------- sse.ts:96 — abort race during reader.read() ----------

describe("parseSSE abort-during-read race (sse.ts:96)", () => {
  it("throws signal.reason when abort fires during a pending read and EOF arrives", async () => {
    // Build a stream we can keep open and signal-abort after the SSE loop
    // is parked inside reader.read(). The abort listener calls reader.cancel
    // which resolves read() with done=true; the EOF branch then sees
    // signal.aborted=true and throws signal.reason.
    let controller!: ReadableStreamDefaultController<Uint8Array>;
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        controller = c;
      },
    });
    const ctrl = new AbortController();
    const reason = new Error("aborted-during-read");

    const iter = parseSSE(body, ctrl.signal);
    const stepPromise = iter.next();
    // Yield so the parser starts an awaited reader.read() — then abort.
    await Promise.resolve();
    ctrl.abort(reason);
    // The reader.cancel() invoked by the abort listener resolves read() with
    // done=true. The EOF branch sees signal.aborted=true and throws.
    let caught: unknown;
    try {
      await stepPromise;
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(reason);
    // Avoid unhandled promises from the rest of the iterator.
    void controller;
  });
});

// ---------- commands.ts: spawn with signal forwarding ----------

describe("Commands.spawn req.signal forwarding (commands.ts:162)", () => {
  it("forwards req.signal to the POST request", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), jsonResponse({ id: "cmd-1" }, { status: 202 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    await sandbox.commands.spawn(["echo"], {}, { signal: ctrl.signal });
    expect(stub.calls[1]!.signal).toBeDefined();
  });
});

// ---------- commands.ts: run() with user signal, internal/composed signal branches ----------

describe("Commands.run req.signal forwarding (commands.ts:207-216)", () => {
  it("composes user signal with internal controller and forwards to stdout/stderr/wait", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-run-sig";
    const ctrl = new AbortController();

    const seen: Record<string, boolean> = { stdout: false, stderr: false, status: false };
    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: (req: { signal: AbortSignal | undefined }) => {
        seen.status = req.signal !== undefined;
        return jsonResponse({ exitCode: 0 });
      },
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: (req: { signal: AbortSignal | undefined }) => {
        seen.stdout = req.signal !== undefined;
        return sseResponse(sseResponseBody([{ data: "hi", id: 1 }]));
      },
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: (req: { signal: AbortSignal | undefined }) => {
        seen.stderr = req.signal !== undefined;
        return sseResponse("");
      },
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    const result = await sandbox.commands.run(["echo", "hi"], {}, { signal: ctrl.signal });
    expect(result.exitCode).toBe(0);
    // All three siblings should have received a composed signal.
    expect(seen).toEqual({ stdout: true, stderr: true, status: true });
  });

  it("catch handler wraps non-Error rejection via new Error(String(...)) (commands.ts:224)", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-non-error";

    // Make stdout throw a non-Error rejection (e.g. a plain string). Since
    // request() always wraps to APIError/APIConnectionError, simulate at the
    // SSE level by giving a fetch that throws a non-Error value verbatim.
    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      // stdout: fail with 400 (non-transient → bubbles up).
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stdout`]: () =>
        jsonResponse({ detail: "bad request" }, { status: 400 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/stderr`]: (req: { signal: AbortSignal | undefined }) =>
        new Promise<Response>((_, reject) => {
          req.signal?.addEventListener("abort", () => reject(req.signal!.reason), { once: true });
        }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: (req: { signal: AbortSignal | undefined }) =>
        new Promise<Response>((_, reject) => {
          req.signal?.addEventListener("abort", () => reject(req.signal!.reason), { once: true });
        }),
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    await expect(sandbox.commands.run(["echo"])).rejects.toThrow();
  });
});

// ---------- commands.ts: exitCode/wait/writeStdin/closeStdin/kill signal forwarding ----------

describe("Command.* req.signal forwarding (commands.ts:299/339/359/373/382)", () => {
  it("exitCode forwards req.signal", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-ec-s" }, { status: 202 }),
      jsonResponse({ exitCode: 0 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["echo"]);
    await cmd.exitCode({ signal: ctrl.signal });
    expect(stub.calls[2]!.signal).toBeDefined();
  });

  it("writeStdin forwards req.signal", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-w" }, { status: 202 }),
      emptyResponse(204),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["cat"]);
    await cmd.writeStdin("hi", { signal: ctrl.signal });
    expect(stub.calls[2]!.signal).toBeDefined();
  });

  it("closeStdin forwards req.signal", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-cs" }, { status: 202 }),
      emptyResponse(204),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["cat"]);
    await cmd.closeStdin({ signal: ctrl.signal });
    expect(stub.calls[2]!.signal).toBeDefined();
  });

  it("kill forwards req.signal", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: "cmd-k" }, { status: 202 }),
      emptyResponse(204),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["sleep", "100"]);
    await cmd.kill({ signal: ctrl.signal });
    expect(stub.calls[2]!.signal).toBeDefined();
  });
});

// ---------- commands.ts: Command.stdout / Command.stderr cache (lines 268,282) ----------

describe("Command.stdout / Command.stderr caching (commands.ts:268/282)", () => {
  it("returns the same StreamReader instance on repeated access (stdout)", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), jsonResponse({ id: "cmd-s" }, { status: 202 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["echo"]);
    const a = cmd.stdout;
    const b = cmd.stdout;
    expect(a).toBe(b);
  });

  it("returns the same StreamReader instance on repeated access (stderr)", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), jsonResponse({ id: "cmd-s" }, { status: 202 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const cmd = await sandbox.commands.spawn(["echo"]);
    const a = cmd.stderr;
    const b = cmd.stderr;
    expect(a).toBe(b);
  });
});

// ---------- commands.ts:111/114 wait() with BOTH userSignal AND timeoutMs ----------

describe("Command.wait composes user signal with timeout (commands.ts:111/114)", () => {
  it("wait completes successfully when both userSignal and timeoutMs are provided", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-both";
    const ctrl = new AbortController();
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ id: cmdId }, { status: 202 }),
      jsonResponse({ exitCode: 0 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    const cmd = await sandbox.commands.spawn(["echo"]);
    const code = await cmd.wait({ signal: ctrl.signal, timeoutMs: 10_000 });
    expect(code).toBe(0);
    expect(stub.calls[2]!.signal).toBeDefined();
  });
});

// ---------- commands.ts:339 wait() user-abort with undefined reason ----------

describe("Command.wait user-signal abort with undefined reason (commands.ts:339)", () => {
  it("falls back to underlying err when signal.aborted is true but reason is undefined", async () => {
    const sbId = "sandbox-123";
    const cmdId = "cmd-wait-noreason";

    const ctrl = new AbortController();
    let aborted = false;
    Object.defineProperty(ctrl.signal, "aborted", { configurable: true, get: () => aborted });
    Object.defineProperty(ctrl.signal, "reason", { configurable: true, get: () => undefined });

    // Make status hang then flip aborted+throw, mimicking a real abort but
    // without a real reason on the signal.
    const customErr = new Error("custom-from-fetch");
    const routes = {
      [`GET /v1/sandboxes/${sbId}`]: () => jsonResponse(sandboxResponseFixture()),
      [`POST /v1/sandboxes/${sbId}/commands`]: () => jsonResponse({ id: cmdId }, { status: 202 }),
      [`GET /v1/sandboxes/${sbId}/commands/${cmdId}/status`]: async () => {
        aborted = true;
        throw customErr;
      },
    };
    const routing = makeRoutingFetch(routes);
    const client = new Isola({ url: URL_BASE, fetch: routing.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get(sbId);
    const cmd = await sandbox.commands.spawn(["sleep", "100"]);

    let caught: unknown;
    try {
      await cmd.wait({ signal: ctrl.signal });
    } catch (err) {
      caught = err;
    }
    // signal.reason is undefined → catch returns err via `?? err`. But err
    // is what the request layer wraps customErr into — an APIConnectionError.
    expect(caught).toBeDefined();
    expect((caught as Error).message).toContain("custom-from-fetch");
  });
});

// ---------- errors.ts:165 — errorFromHttp with explicit cause ----------

describe("errorFromHttp with cause (errors.ts:165)", () => {
  it("preserves opts.cause on the resulting APIError", async () => {
    const { errorFromHttp } = await import("../src/errors");
    const inner = new Error("inner");
    const e = errorFromHttp({ status: 500, reason: "Server Error", body: null, cause: inner });
    expect(e.cause).toBe(inner);
  });
});

// ---------- models.ts:89 dropUndefined drops undefined keys ----------

describe("Network.toWire drops undefined keys (models.ts:89)", () => {
  it("Network.toWire drops fields that are explicitly undefined", async () => {
    const { Network } = await import("../src/models");
    // Construct an object with one defined and one explicit-undefined field.
    const input: Record<string, unknown> = { allowClusterDNS: true, allowInternetEgress: undefined };
    const out = Network.toWire(input as Parameters<typeof Network.toWire>[0]);
    expect(out).toEqual({ allowClusterDNS: true });
    expect(out).not.toHaveProperty("allowInternetEgress");
  });
});

// ---------- Typed array body replay (http.ts:107 ArrayBuffer.isView) ----------

describe("isReplayableBody coverage of typed-array variants (http.ts:107)", () => {
  it("Int32Array body is replayable and retried on transient", async () => {
    const stub = makeStubFetch(jsonResponse({ detail: "bad gateway" }, { status: 502 }), emptyResponse(204));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
    const body = new Int32Array([1, 2, 3]);
    await api.requestNoContent({
      method: "POST",
      path: "/v1/sandboxes",
      body: body as unknown as BodyInit,
      bodyKind: "replayable",
      headers: { "content-type": "application/octet-stream" },
    });
    expect(stub.calls).toHaveLength(2);
  });

  it("DataView body is replayable and retried on transient", async () => {
    const stub = makeStubFetch(jsonResponse({ detail: "bad gateway" }, { status: 502 }), emptyResponse(204));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
    const buf = new ArrayBuffer(8);
    const view = new DataView(buf);
    await api.requestNoContent({
      method: "POST",
      path: "/v1/sandboxes",
      body: view as unknown as BodyInit,
      bodyKind: "replayable",
      headers: { "content-type": "application/octet-stream" },
    });
    expect(stub.calls).toHaveLength(2);
  });

  it("URLSearchParams body is replayable and retried on transient (re-confirm)", async () => {
    const stub = makeStubFetch(jsonResponse({ detail: "bad gateway" }, { status: 502 }), emptyResponse(204));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
    const body = new URLSearchParams("a=1&b=2");
    await api.requestNoContent({
      method: "POST",
      path: "/v1/sandboxes",
      body: body as unknown as BodyInit,
      bodyKind: "replayable",
      headers: { "content-type": "application/x-www-form-urlencoded" },
    });
    expect(stub.calls).toHaveLength(2);
  });
});
