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

// Mirrors sdks/python/tests/test_sandbox.py — same scenarios, same assertions.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Isola } from "../src/client";
import { InternalError, IsolaError, IsolaTimeoutError } from "../src/errors";
import {
  emptyResponse,
  jsonResponse,
  makeSandboxResponse,
  makeStubFetch,
  sandboxResponseFixture,
  sandboxSummaryResponseFixture,
  spyMonotonicAdvancingBy,
} from "./_helpers";

const URL_BASE = "http://localhost:8080";

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("create() flat resources mapping", () => {
  it("maps cpu/memory/ephemeralStorage onto BOTH limits and requests in podTemplate", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture(), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const sandbox = await client.sandboxes.create({
      image: "python:3.12",
      command: ["sleep", "infinity"],
      env: { KEY: "value" },
      cpu: 0.5,
      memory: 1024,
      ephemeralStorage: 2048,
      timeoutSeconds: 3600,
    });

    expect(sandbox.id).toBe("sandbox-123");
    expect(sandbox.status).toBe("Running");

    expect(stub.calls).toHaveLength(1);
    const call = stub.calls[0];
    expect(call?.method).toBe("POST");
    expect(call?.url).toBe(`${URL_BASE}/v1/sandboxes`);

    const payload = JSON.parse(call?.bodyText ?? "");
    expect(payload).toEqual({
      podTemplate: {
        containers: [
          {
            image: "python:3.12",
            command: ["sleep", "infinity"],
            env: { KEY: "value" },
            resources: {
              limits: { cpu: "500m", memory: "1024Mi", ephemeralStorage: "2048Mi" },
              requests: { cpu: "500m", memory: "1024Mi", ephemeralStorage: "2048Mi" },
            },
          },
        ],
      },
      timeoutSeconds: 3600,
    });
  });

  it.each([
    // Positive half-ties: Math.round is half-away-from-zero (1.5 -> 2, 2.5 -> 3),
    // so for ties we steer toward the even neighbour. Python's built-in `round`
    // is the spec.
    { cpu: 0.0025, expected: "2m" }, // 2.5 -> 2 (even)
    { cpu: 0.0035, expected: "4m" }, // 3.5 -> 4 (even)
    { cpu: 0.0045, expected: "4m" }, // 4.5 -> 4 (even)
    { cpu: 0.5, expected: "500m" }, // not a tie
    // Negative half-ties: defended against future helper reuse with negative
    // inputs. The only current caller passes cpu * 1000 where cpu >= 0, but
    // `roundHalfEven` is named generically; these pin the math.
    { cpu: -0.0015, expected: "-2m" }, // -1.5 -> -2 (even)
    { cpu: -0.0035, expected: "-4m" }, // -3.5 -> -4 (even)
    { cpu: -0.0025, expected: "-2m" }, // -2.5 -> -2 (even — already)
  ])("CPU shorthand $cpu rounds half-to-even -> $expected", async ({ cpu, expected }) => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture(), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await client.sandboxes.create({ image: "alpine:3.21", cpu });
    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.podTemplate.containers[0].resources.limits.cpu).toBe(expected);
  });
});

describe("list()", () => {
  it("returns SandboxSummary[] with id, status, creationTimestamp", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxSummaryResponseFixture()));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const sandboxes = await client.sandboxes.list();

    expect(sandboxes).toHaveLength(2);
    expect(sandboxes[0]?.id).toBe("sandbox-123");
    expect(sandboxes[0]?.status).toBe("Running");
    expect(sandboxes[0]?.creationTimestamp).toEqual(new Date("2026-02-18T00:00:00Z"));
    expect(sandboxes[1]?.id).toBe("sandbox-456");
    expect(sandboxes[1]?.status).toBe("Pending");

    expect(stub.calls).toHaveLength(1);
    expect(stub.calls[0]?.method).toBe("GET");
    expect(stub.calls[0]?.url).toBe(`${URL_BASE}/v1/sandboxes`);
  });
});

describe("get() and delete()", () => {
  it("issues GET /v1/sandboxes/{id} and DELETE /v1/sandboxes/{id}", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), emptyResponse(204));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const sandbox = await client.sandboxes.get("sandbox-123");
    expect(sandbox.id).toBe("sandbox-123");

    await sandbox.delete();

    expect(stub.calls).toHaveLength(2);
    expect(stub.calls[0]?.method).toBe("GET");
    expect(stub.calls[0]?.url).toBe(`${URL_BASE}/v1/sandboxes/sandbox-123`);
    expect(stub.calls[1]?.method).toBe("DELETE");
    expect(stub.calls[1]?.url).toBe(`${URL_BASE}/v1/sandboxes/sandbox-123`);
  });
});

describe("Network acronym aliases round-trip", () => {
  it("sends and receives allowClusterDNS, allowedEgressCIDRs, allowIPv6Egress with exact OpenAPI casing", async () => {
    const responseBody = sandboxResponseFixture();
    responseBody.network = {
      allowInternetEgress: false,
      allowClusterDNS: true,
      allowIPv6Egress: true,
      allowedEgressCIDRs: ["10.0.0.0/8"],
      nameservers: ["8.8.8.8"],
    };
    const stub = makeStubFetch(jsonResponse(responseBody, { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const sandbox = await client.sandboxes.create({
      image: "python:3.12",
      network: {
        allowInternetEgress: false,
        allowClusterDNS: true,
        allowIPv6Egress: true,
        allowedEgressCIDRs: ["10.0.0.0/8"],
        nameservers: ["8.8.8.8"],
      },
    });

    // Response deserialization: server's OpenAPI casing must be parsed correctly.
    expect(sandbox.network).not.toBeNull();
    expect(sandbox.network?.allowClusterDNS).toBe(true);
    expect(sandbox.network?.allowIPv6Egress).toBe(true);
    expect(sandbox.network?.allowedEgressCIDRs).toEqual(["10.0.0.0/8"]);

    // Request serialization: SDK must send OpenAPI casing.
    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    const network = payload.network;
    expect(network).toHaveProperty("allowClusterDNS");
    expect(network).not.toHaveProperty("allowClusterDns");
    expect(network).toHaveProperty("allowIPv6Egress");
    expect(network).not.toHaveProperty("allowIpv6Egress");
    expect(network).toHaveProperty("allowedEgressCIDRs");
    expect(network).not.toHaveProperty("allowedEgressCidrs");
  });
});

describe("Symbol.asyncDispose", () => {
  it("await using sandbox calls sandbox.delete() on exit", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), emptyResponse(204));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    {
      await using sandbox = await client.sandboxes.get("sandbox-123");
      expect(sandbox.id).toBe("sandbox-123");
    }

    expect(stub.calls).toHaveLength(2);
    expect(stub.calls[1]?.method).toBe("DELETE");
    expect(stub.calls[1]?.url).toBe(`${URL_BASE}/v1/sandboxes/sandbox-123`);
  });

  it("await using sandbox still DELETEs when the block throws", async () => {
    // The whole point of `using` is exception-safe cleanup. A regression that
    // skipped the dispose on throw would orphan sandboxes.
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), emptyResponse(204));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const boom = new Error("from-inside-block");

    let caught: unknown;
    try {
      await using sandbox = await client.sandboxes.get("sandbox-123");
      expect(sandbox.id).toBe("sandbox-123");
      throw boom;
    } catch (err) {
      caught = err;
    }
    expect(caught).toBe(boom);
    expect(stub.calls[1]?.method).toBe("DELETE");
  });

  it("dispose swallows NotFoundError so already-deleted sandbox doesn't mask block exception", async () => {
    // Server-side timeout / manual delete / pod eviction can race: by the
    // time dispose fires, the sandbox is already gone. A propagating 404
    // would become a SuppressedError and shadow whatever the user's block
    // was raising.
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const boom = new Error("user-block-error");

    let caught: unknown;
    try {
      await using sandbox = await client.sandboxes.get("sandbox-123");
      expect(sandbox.id).toBe("sandbox-123");
      throw boom;
    } catch (err) {
      caught = err;
    }
    // The user's exception survives intact; no SuppressedError wrapping.
    expect(caught).toBe(boom);
    expect(stub.calls[1]?.method).toBe("DELETE");
  });

  it("dispose propagates non-404 errors from delete", async () => {
    // Idempotency is scoped to NotFoundError only. A 500 (or any other
    // failure) must still surface.
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "boom" }, { status: 500 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    let caught: unknown;
    try {
      await using sandbox = await client.sandboxes.get("sandbox-123");
      expect(sandbox.id).toBe("sandbox-123");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeDefined();
    // 500 maps to InternalError (subclass of APIError).
    expect((caught as Error).constructor.name).toBe("InternalError");
  });
});

describe("create() validation", () => {
  it("throws when both image and containers are provided", async () => {
    const stub = makeStubFetch();
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await expect(
      client.sandboxes.create({
        image: "python:3.12",
        containers: [{ image: "python:3.12" }],
      } as never),
    ).rejects.toThrow();
    expect(stub.calls).toHaveLength(0);
  });

  it("throws when neither image nor containers is provided", async () => {
    const stub = makeStubFetch();
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await expect(client.sandboxes.create({} as never)).rejects.toThrow();
    expect(stub.calls).toHaveLength(0);
  });

  it("throws when per-container fields like command are mixed with containers", async () => {
    const stub = makeStubFetch();
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await expect(
      client.sandboxes.create({
        containers: [{ image: "python:3.12" }],
        command: ["sleep", "infinity"],
      } as never),
    ).rejects.toThrow();
    expect(stub.calls).toHaveLength(0);
  });

  it("throws when containers list is empty", async () => {
    const stub = makeStubFetch();
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await expect(client.sandboxes.create({ containers: [] })).rejects.toThrow();
    expect(stub.calls).toHaveLength(0);
  });

  it("throws when containers is a non-array shape (JS-only)", async () => {
    // TS types forbid this; a JS caller passing { containers: {...} } would
    // otherwise be misrouted to the single-container path and silently drop
    // the value. buildContainers detects this and surfaces a clear error.
    const stub = makeStubFetch();
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await expect(client.sandboxes.create({ containers: { image: "alpine:3.21" } as never })).rejects.toThrow(
      /'containers' must be an array/,
    );
    expect(stub.calls).toHaveLength(0);
  });

  it("ignores `null` on optional sub-models (JS-only) instead of crashing", async () => {
    // TS forbids `network: null`/`terminationPolicy: null`; a JS caller can pass
    // them. The optional-sub-model guards in create() and CreateSandboxPayload
    // use `!= null` (Python's `exclude_none` analogue) so we never invoke
    // Network.toWire(null) / TerminationPolicy.toWire(null) and trigger a
    // cryptic Object.entries(null) TypeError.
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture(), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await client.sandboxes.create({
      image: "alpine:3.21",
      network: null as never,
      terminationPolicy: null as never,
    });
    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload).not.toHaveProperty("network");
    expect(payload).not.toHaveProperty("terminationPolicy");
  });
});

describe("rootfsSnapshotName on single-container", () => {
  it("includes rootfsSnapshotName in the wire containers[0]", async () => {
    const responseBody = sandboxResponseFixture();
    (responseBody.podTemplate as { containers: Array<Record<string, unknown>> }).containers[0]!.rootfsSnapshotName =
      "my-snapshot";
    const stub = makeStubFetch(jsonResponse(responseBody, { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const sandbox = await client.sandboxes.create({
      image: "python:3.12",
      rootfsSnapshotName: "my-snapshot",
    });

    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.podTemplate.containers[0].rootfsSnapshotName).toBe("my-snapshot");
    expect(sandbox.containers[0]?.rootfsSnapshotName).toBe("my-snapshot");
  });
});

describe("multi-container containers", () => {
  it("serialises each Container in the list", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture(), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await client.sandboxes.create({
      containers: [
        { image: "python:3.12", command: ["sleep", "infinity"] },
        { name: "worker", image: "nginx:latest" },
      ],
    });

    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.podTemplate.containers).toHaveLength(2);
    expect(payload.podTemplate.containers[0]).toEqual({
      image: "python:3.12",
      command: ["sleep", "infinity"],
    });
    expect(payload.podTemplate.containers[1]).toEqual({
      image: "nginx:latest",
      name: "worker",
    });
  });

  it("propagates a per-container rootfsSnapshotName", async () => {
    const responseBody = sandboxResponseFixture();
    (responseBody.podTemplate as { containers: Array<Record<string, unknown>> }).containers[0]!.rootfsSnapshotName =
      "snap-a";
    const stub = makeStubFetch(jsonResponse(responseBody, { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await client.sandboxes.create({
      containers: [{ image: "python:3.12", rootfsSnapshotName: "snap-a" }],
    });

    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.podTemplate.containers[0].rootfsSnapshotName).toBe("snap-a");
  });
});

describe("terminationPolicy: SnapshotRootfs", () => {
  it("wraps an empty SnapshotRootfs into { type, snapshotRootfs: {} }", async () => {
    const responseBody = sandboxResponseFixture();
    responseBody.terminationPolicy = { type: "SnapshotRootfs", snapshotRootfs: {} };
    const stub = makeStubFetch(jsonResponse(responseBody, { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await client.sandboxes.create({
      image: "python:3.12",
      terminationPolicy: {},
    });

    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.terminationPolicy).toEqual({
      type: "SnapshotRootfs",
      snapshotRootfs: {},
    });
  });

  it("includes snapshotName when provided", async () => {
    const responseBody = sandboxResponseFixture();
    responseBody.terminationPolicy = {
      type: "SnapshotRootfs",
      snapshotRootfs: { snapshotName: "my-snap" },
    };
    const stub = makeStubFetch(jsonResponse(responseBody, { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await client.sandboxes.create({
      image: "python:3.12",
      terminationPolicy: { snapshotName: "my-snap" },
    });

    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.terminationPolicy).toEqual({
      type: "SnapshotRootfs",
      snapshotRootfs: { snapshotName: "my-snap" },
    });
  });
});

describe("polling — wait until Running", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  });

  it("polls GET until Running", async () => {
    const stub = makeStubFetch(
      jsonResponse(makeSandboxResponse("Pending"), { status: 201 }),
      jsonResponse(makeSandboxResponse("Pending")),
      jsonResponse(makeSandboxResponse("Running")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.sandboxes.create({ image: "python:3.12" });
    await vi.runAllTimersAsync();
    const sandbox = await promise;

    expect(sandbox.status).toBe("Running");
    // 1 POST + 2 GETs.
    expect(stub.calls).toHaveLength(3);
    expect(stub.calls[1]?.method).toBe("GET");
    expect(stub.calls[2]?.method).toBe("GET");
  });

  it("Terminating is not in TERMINAL_STATUSES — keeps polling, not throwing", async () => {
    // Terminating is the in-progress termination state; it should NOT be
    // treated as a terminal-success state by waitUntilRunning. A regression
    // that added it to TERMINAL_STATUSES would throw a "terminal state" error
    // here; correct behavior is to keep polling until Running.
    const stub = makeStubFetch(
      jsonResponse(makeSandboxResponse("Pending"), { status: 201 }),
      jsonResponse(makeSandboxResponse("Terminating")),
      jsonResponse(makeSandboxResponse("Terminating")),
      jsonResponse(makeSandboxResponse("Running")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.sandboxes.create({ image: "python:3.12" });
    await vi.runAllTimersAsync();
    const sandbox = await promise;

    expect(sandbox.status).toBe("Running");
    expect(stub.calls).toHaveLength(4);
  });
});

describe("maxWaitMs: 0", () => {
  it("returns immediately without polling", async () => {
    const stub = makeStubFetch(jsonResponse(makeSandboxResponse("Pending"), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const sandbox = await client.sandboxes.create({ image: "python:3.12", maxWaitMs: 0 });

    expect(sandbox.status).toBe("Pending");
    expect(stub.calls).toHaveLength(1);
    expect(stub.calls[0]?.method).toBe("POST");
  });
});

describe("already terminal at create time", () => {
  it("throws IsolaError 'terminal state' when POST returns Failed", async () => {
    const stub = makeStubFetch(jsonResponse(makeSandboxResponse("Failed"), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    await expect(client.sandboxes.create({ image: "python:3.12" })).rejects.toThrow(/terminal state/);
    // Only the POST happened — no GET should follow.
    expect(stub.calls).toHaveLength(1);
    expect(stub.calls[0]?.method).toBe("POST");
  });

  it("throws IsolaError 'terminal state' when POST returns Succeeded", async () => {
    const stub = makeStubFetch(jsonResponse(makeSandboxResponse("Succeeded"), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    let caught: unknown;
    try {
      await client.sandboxes.create({ image: "python:3.12" });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(IsolaError);
    expect((caught as Error).message).toMatch(/terminal state/);
    expect(stub.calls).toHaveLength(1);
  });
});

describe("terminal during wait", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  });

  it("throws IsolaError when GET returns Failed", async () => {
    const stub = makeStubFetch(
      jsonResponse(makeSandboxResponse("Pending"), { status: 201 }),
      jsonResponse(makeSandboxResponse("Failed")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.sandboxes.create({ image: "python:3.12" });
    const expectation = expect(promise).rejects.toThrow(/terminal state/);
    await vi.runAllTimersAsync();
    await expectation;
  });

  it("throws IsolaError when GET returns Succeeded", async () => {
    const stub = makeStubFetch(
      jsonResponse(makeSandboxResponse("Pending"), { status: 201 }),
      jsonResponse(makeSandboxResponse("Succeeded")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.sandboxes.create({ image: "python:3.12" });
    let caught: unknown;
    promise.catch((err) => {
      caught = err;
    });
    await vi.runAllTimersAsync();
    await promise.catch(() => {});
    expect(caught).toBeInstanceOf(IsolaError);
    expect((caught as Error).message).toMatch(/terminal state/);
  });
});

describe("already Running at create", () => {
  it("skips wait entirely (no GETs)", async () => {
    const stub = makeStubFetch(jsonResponse(makeSandboxResponse("Running"), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const sandbox = await client.sandboxes.create({ image: "python:3.12" });
    expect(sandbox.status).toBe("Running");
    expect(stub.calls).toHaveLength(1);
    expect(stub.calls[0]?.method).toBe("POST");
  });
});

describe("startupTimeoutSeconds", () => {
  it("is sent in the request payload and exposed as a Sandbox property", async () => {
    const responseBody = makeSandboxResponse("Running");
    responseBody.startupTimeoutSeconds = 45;
    const stub = makeStubFetch(jsonResponse(responseBody, { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const sandbox = await client.sandboxes.create({
      image: "python:3.12",
      startupTimeoutSeconds: 45,
    });

    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.startupTimeoutSeconds).toBe(45);
    expect(sandbox.startupTimeoutSeconds).toBe(45);
  });
});

describe("eventual consistency", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  });

  it("tolerates transient NotFoundError until Running", async () => {
    const stub = makeStubFetch(
      jsonResponse(makeSandboxResponse("Pending"), { status: 201 }),
      jsonResponse({ detail: "not found" }, { status: 404 }),
      jsonResponse({ detail: "not found" }, { status: 404 }),
      jsonResponse(makeSandboxResponse("Pending")),
      jsonResponse(makeSandboxResponse("Running")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.sandboxes.create({ image: "python:3.12" });
    await vi.runAllTimersAsync();
    const sandbox = await promise;

    expect(sandbox.status).toBe("Running");
    expect(stub.calls).toHaveLength(5);
  });
});

describe("timeout on max wait", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    // performance.now() advances by 2000ms each call so the 5_000ms
    // deadline is exceeded after a few polls — mirrors Python's fake_monotonic.
    spyMonotonicAdvancingBy(2000);
  });

  it("raises IsolaTimeoutError when the sandbox stays Pending past maxWaitMs", async () => {
    const responders = [jsonResponse(makeSandboxResponse("Pending"), { status: 201 })];
    for (let i = 0; i < 20; i++) {
      responders.push(jsonResponse(makeSandboxResponse("Pending")));
    }
    const stub = makeStubFetch(...responders);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.sandboxes.create({ image: "python:3.12", maxWaitMs: 5000 });
    let caught: unknown;
    promise.catch((err) => {
      caught = err;
    });
    await vi.runAllTimersAsync();
    await promise.catch(() => {});

    expect(caught).toBeInstanceOf(IsolaTimeoutError);
    expect((caught as Error).message).toContain("did not reach running state within 5000ms");
    // The SDK must NOT auto-DELETE on timeout — that's the caller's choice.
    // A regression that added a "best-effort cleanup" would orphan sandboxes
    // belonging to a different caller (think race with `await using`).
    expect(stub.calls.filter((c) => c.method === "DELETE")).toHaveLength(0);
  });

  it("raises IsolaTimeoutError when 404 persists past maxWaitMs", async () => {
    const responders = [jsonResponse(makeSandboxResponse("Pending"), { status: 201 })];
    for (let i = 0; i < 20; i++) {
      responders.push(jsonResponse({ detail: "not found" }, { status: 404 }));
    }
    const stub = makeStubFetch(...responders);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.sandboxes.create({ image: "python:3.12", maxWaitMs: 5000 });
    let caught: unknown;
    promise.catch((err) => {
      caught = err;
    });
    await vi.runAllTimersAsync();
    await promise.catch(() => {});

    expect(caught).toBeInstanceOf(IsolaTimeoutError);
    expect((caught as Error).message).toContain("did not reach running state within 5000ms");
  });
});

describe("waitUntilRunning non-404 error propagation", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
  });

  it("re-throws InternalError without retrying", async () => {
    // Non-NotFound errors during polling must surface immediately rather
    // than being absorbed by the 404 retry branch.
    // We pre-load 1 + MAX_RETRIES failures because 500 is non-transient
    // (no retry), but the wait loop on a 500 also rejects immediately.
    const stub = makeStubFetch(
      jsonResponse(makeSandboxResponse("Pending"), { status: 201 }),
      jsonResponse({ detail: "sidecar exploded" }, { status: 500 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.sandboxes.create({ image: "python:3.12" });
    let caught: unknown;
    promise.catch((err) => {
      caught = err;
    });
    await vi.runAllTimersAsync();
    await promise.catch(() => {});

    expect(caught).toBeInstanceOf(InternalError);
    expect((caught as Error).message).toContain("sidecar exploded");
  });
});

describe("Sandboxes signal forwarding", () => {
  it("get() forwards req.signal to the GET request", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.sandboxes.get("sandbox-123", { signal: ctrl.signal });
    expect(stub.calls[0]?.signal).toBeDefined();
  });

  it("list() forwards req.signal to the GET request", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.sandboxes.list({ signal: ctrl.signal });
    expect(stub.calls[0]?.signal).toBeDefined();
  });

  it("create() forwards req.signal to the POST and to polling GETs", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture(), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.sandboxes.create({ image: "python:3.12" }, { signal: ctrl.signal });
    expect(stub.calls[0]?.signal).toBeDefined();
  });

  it("create() forwards req.signal to polling GETs (waitUntilRunning)", async () => {
    // The signal-spread inside waitUntilRunning.
    const ctrl = new AbortController();
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const stub = makeStubFetch(
        jsonResponse(makeSandboxResponse("Pending"), { status: 201 }),
        jsonResponse(makeSandboxResponse("Running")),
      );
      const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
      const promise = client.sandboxes.create({ image: "python:3.12" }, { signal: ctrl.signal });
      await vi.runAllTimersAsync();
      await promise;
      // Both POST and GET should have received a signal.
      expect(stub.calls[0]?.signal).toBeDefined();
      expect(stub.calls[1]?.signal).toBeDefined();
    } finally {
      vi.useRealTimers();
    }
  });

  it("delete() forwards req.signal to the DELETE request", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), emptyResponse(204));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    await sandbox.delete({ signal: ctrl.signal });
    expect(stub.calls[1]?.signal).toBeDefined();
  });
});

describe("buildResources individual fields", () => {
  it("includes only cpu when only cpu is set", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture(), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.sandboxes.create({ image: "x", cpu: 1 });
    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.podTemplate.containers[0].resources.limits).toEqual({ cpu: "1000m" });
  });

  it("includes only memory when only memory is set", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture(), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.sandboxes.create({ image: "x", memory: 512 });
    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.podTemplate.containers[0].resources.limits).toEqual({ memory: "512Mi" });
  });

  it("includes only ephemeralStorage when only ephemeralStorage is set", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture(), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.sandboxes.create({ image: "x", ephemeralStorage: 1024 });
    const payload = JSON.parse(stub.calls[0]?.bodyText ?? "");
    expect(payload.podTemplate.containers[0].resources.limits).toEqual({ ephemeralStorage: "1024Mi" });
  });
});

describe("Sandbox accessors", () => {
  it("exposes creationTimestamp, network=null, and timeoutSeconds=null when absent", async () => {
    // Response with no timeoutSeconds, no network.
    const responseBody = {
      id: "sandbox-x",
      status: "Running",
      creationTimestamp: "2026-03-15T12:30:00Z",
      podTemplate: { containers: [{ name: "n", image: "i" }] },
      startupTimeoutSeconds: 60,
    };
    const stub = makeStubFetch(jsonResponse(responseBody));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-x");

    expect(sandbox.creationTimestamp).toEqual(new Date("2026-03-15T12:30:00Z"));
    // network getter when undefined → null.
    expect(sandbox.network).toBeNull();
    // timeoutSeconds getter when undefined → null.
    expect(sandbox.timeoutSeconds).toBeNull();
    expect(sandbox.startupTimeoutSeconds).toBe(60);
    expect(sandbox.containers).toHaveLength(1);
  });

  it("exposes timeoutSeconds as a number when present", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    expect(sandbox.timeoutSeconds).toBe(3600);
    expect(sandbox.network).not.toBeNull();
  });
});
