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

// Mirrors sdks/python/tests/test_rootfs_snapshot.py, same scenarios.
// Polling tests use fake timers (setTimeout/clearTimeout only) so the SDK's
// sleep(POLL_INTERVAL_MS) is drained instantly via runAllTimersAsync().

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Isola } from "../src/client";
import { InternalError, IsolaError, IsolaTimeoutError } from "../src/errors";
import {
  jsonResponse,
  makeRootfsSnapshotResponse,
  makeStubFetch,
  rootfsSnapshotResponseFixture,
  settle,
  spyMonotonicAdvancingBy,
} from "./_helpers";

const URL_BASE = "http://localhost:8080";

beforeEach(() => {
  // Fake only setTimeout/clearTimeout so AbortSignal.timeout() and Date.now()
  // are unaffected; polling sleeps are drained via runAllTimersAsync().
  vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("RootfsSnapshots.create + get", () => {
  it("create() with all fields sends the right body and returns the snapshot", async () => {
    const fixture = { ...rootfsSnapshotResponseFixture(), ttlSecondsAfterFinished: 600 };
    const stub = makeStubFetch(jsonResponse(fixture, { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const snapshot = await client.rootfsSnapshots.create({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
      containerName: "worker",
      timeoutSeconds: 300,
      ttlSecondsAfterFinished: 600,
    });

    expect(snapshot.id).toBe("snapshot-123");
    expect(snapshot.sandboxId).toBe("sandbox-123");
    expect(snapshot.snapshotName).toBe("my-snapshot");
    expect(snapshot.containerName).toBe("worker");
    expect(snapshot.timeoutSeconds).toBe(300);
    expect(snapshot.ttlSecondsAfterFinished).toBe(600);
    expect(snapshot.status).toBe("Succeeded");
    expect(snapshot.creationTimestamp).toEqual(new Date("2026-02-18T00:00:00Z"));

    const call = stub.calls[0]!;
    expect(call.method).toBe("POST");
    expect(call.url).toBe(`${URL_BASE}/v1/rootfs-snapshots`);
    expect(JSON.parse(call.bodyText)).toEqual({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
      containerName: "worker",
      timeoutSeconds: 300,
      ttlSecondsAfterFinished: 600,
    });
  });

  it("create() with only sandboxId omits all optional fields from the wire payload", async () => {
    // The wire payload must contain only `sandboxId`, the SDK must not
    // serialize undefined snapshotName/containerName/timeoutSeconds/ttl as
    // null or "" on the wire (Python parity: `exclude_none=True`).
    const stub = makeStubFetch(jsonResponse(makeRootfsSnapshotResponse("Succeeded"), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.rootfsSnapshots.create({ sandboxId: "sandbox-123" });
    expect(JSON.parse(stub.calls[0]!.bodyText)).toEqual({ sandboxId: "sandbox-123" });
  });

  it("get(snapshotId) fetches by id", async () => {
    const stub = makeStubFetch(jsonResponse(rootfsSnapshotResponseFixture()));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const snapshot = await client.rootfsSnapshots.get("snapshot-123");
    expect(snapshot.id).toBe("snapshot-123");
    expect(snapshot.sandboxId).toBe("sandbox-123");
    expect(snapshot.snapshotName).toBe("my-snapshot");
    expect(snapshot.containerName).toBe("worker");
    expect(snapshot.timeoutSeconds).toBe(300);
    expect(snapshot.ttlSecondsAfterFinished).toBe(300);
    expect(snapshot.status).toBe("Succeeded");
    expect(snapshot.creationTimestamp).toEqual(new Date("2026-02-18T00:00:00Z"));

    const call = stub.calls[0]!;
    expect(call.method).toBe("GET");
    expect(call.url).toBe(`${URL_BASE}/v1/rootfs-snapshots/snapshot-123`);
  });
});

describe("RootfsSnapshots.create polling", () => {
  it("waits until status reaches Succeeded", async () => {
    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse(makeRootfsSnapshotResponse("Running")),
      jsonResponse(makeRootfsSnapshotResponse("Succeeded")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.rootfsSnapshots.create({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
    });
    // Drain pending sleeps so polling iterations advance.
    await vi.runAllTimersAsync();
    const snapshot = await promise;

    expect(snapshot.status).toBe("Succeeded");
    // POST + 2 GETs (Running, Succeeded).
    expect(stub.calls).toHaveLength(3);
    expect(stub.calls[1]!.method).toBe("GET");
    expect(stub.calls[2]!.method).toBe("GET");
  });

  it("Running status does NOT trigger checkFailed (only Failed is terminal-failure)", async () => {
    // If the implementation accidentally treated Running as a terminal failure
    // it would throw; we expect it to keep polling and finish on Succeeded.
    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse(makeRootfsSnapshotResponse("Running")),
      jsonResponse(makeRootfsSnapshotResponse("Running")),
      jsonResponse(makeRootfsSnapshotResponse("Succeeded")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.rootfsSnapshots.create({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
    });
    await vi.runAllTimersAsync();
    const snapshot = await promise;

    expect(snapshot.status).toBe("Succeeded");
    expect(stub.calls).toHaveLength(4);
  });

  it("throws IsolaError when status transitions Running -> Failed mid-poll", async () => {
    // Defense against a regression that misclassifies Running as success-terminal.
    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse(makeRootfsSnapshotResponse("Running")),
      jsonResponse(makeRootfsSnapshotResponse("Running")),
      jsonResponse(makeRootfsSnapshotResponse("Failed")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.rootfsSnapshots.create({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
    });
    let caught: unknown;
    promise.catch((err) => {
      caught = err;
    });
    await vi.runAllTimersAsync();
    try {
      await promise;
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(IsolaError);
    expect((caught as Error).message).toMatch(/terminal state.*Failed/);
    expect(stub.calls).toHaveLength(4);
  });

  it("maxWaitMs: 0 returns immediately without polling", async () => {
    const stub = makeStubFetch(jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const snapshot = await client.rootfsSnapshots.create({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
      maxWaitMs: 0,
    });

    expect(snapshot.status).toBe("Pending");
    // Only the POST, no GET polls.
    expect(stub.calls).toHaveLength(1);
  });

  it("throws IsolaError when POST already returns Failed", async () => {
    const stub = makeStubFetch(jsonResponse(makeRootfsSnapshotResponse("Failed"), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    let caught: unknown;
    try {
      await client.rootfsSnapshots.create({
        sandboxId: "sandbox-123",
        snapshotName: "my-snapshot",
        maxWaitMs: 0,
      });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(IsolaError);
    expect((caught as IsolaError).message).toMatch(/terminal state/);
    // Should not poll: the POST itself revealed Failed.
    expect(stub.calls).toHaveLength(1);
  });

  it("throws when status flips to Failed during wait", async () => {
    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse(makeRootfsSnapshotResponse("Failed")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    // settle() attaches a swallowing handler synchronously so any eventual
    // rejection is already considered handled by the time the timer microtask
    // flushes.
    const settled = settle(
      client.rootfsSnapshots.create({
        sandboxId: "sandbox-123",
        snapshotName: "my-snapshot",
      }),
    );
    await vi.runAllTimersAsync();
    const result = await settled;

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.e).toBeInstanceOf(IsolaError);
      expect((result.e as IsolaError).message).toMatch(/terminal state/);
    }
  });

  it("skips wait when POST already returns Succeeded", async () => {
    const stub = makeStubFetch(jsonResponse(makeRootfsSnapshotResponse("Succeeded"), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const snapshot = await client.rootfsSnapshots.create({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
    });
    expect(snapshot.status).toBe("Succeeded");
    // No GET polls.
    expect(stub.calls).toHaveLength(1);
  });

  it("tolerates eventual-consistency 404s during wait", async () => {
    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse({ detail: "not found" }, { status: 404 }),
      jsonResponse({ detail: "not found" }, { status: 404 }),
      jsonResponse(makeRootfsSnapshotResponse("Running")),
      jsonResponse(makeRootfsSnapshotResponse("Succeeded")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.rootfsSnapshots.create({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
    });
    await vi.runAllTimersAsync();
    const snapshot = await promise;

    expect(snapshot.status).toBe("Succeeded");
    expect(stub.calls).toHaveLength(5);
  });
});

describe("RootfsSnapshots.create completion via cleanup 404", () => {
  it("treats a 404 as success when ttlSecondsAfterFinished triggers immediate cleanup", async () => {
    // ttlSecondsAfterFinished:0 makes the controller delete the CR in the same
    // reconcile that marks it Succeeded, so polling only ever sees a 404. That
    // 404 is completion, not cache lag: the snapshot uploaded and is restorable.
    // performance.now() is fast-forwarded to prove we short-circuit before the
    // deadline rather than blocking to a false timeout.
    spyMonotonicAdvancingBy(2000);

    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse({ detail: "not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.rootfsSnapshots.create({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
      ttlSecondsAfterFinished: 0,
      maxWaitMs: 5,
    });
    await vi.runAllTimersAsync();
    const snapshot = await promise;

    expect(snapshot.status).toBe("Succeeded");
    expect(snapshot.snapshotName).toBe("my-snapshot");
    // POST + a single GET that 404s and resolves to success.
    expect(stub.calls).toHaveLength(2);
  });

  it("treats a 404 as success after the snapshot was seen mid-run", async () => {
    // Even with a large TTL, a snapshot observed mid-run that then 404s was
    // deleted by TTL cleanup after completing, so it is a success.
    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse(makeRootfsSnapshotResponse("Running")),
      jsonResponse({ detail: "not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const promise = client.rootfsSnapshots.create({
      sandboxId: "sandbox-123",
      snapshotName: "my-snapshot",
    });
    await vi.runAllTimersAsync();
    const snapshot = await promise;

    expect(snapshot.status).toBe("Succeeded");
    expect(stub.calls).toHaveLength(3);
  });
});

describe("RootfsSnapshots.create timeout", () => {
  it("raises IsolaTimeoutError when maxWaitMs is exhausted", async () => {
    // Fast-forward performance.now() so each call adds 2000ms; with
    // maxWaitMs:5, the first reading after the POST puts us past the
    // deadline so the very next status check trips the timeout.
    spyMonotonicAdvancingBy(2000);

    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse(makeRootfsSnapshotResponse("Running")),
      jsonResponse(makeRootfsSnapshotResponse("Running")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const settled = settle(
      client.rootfsSnapshots.create({
        sandboxId: "sandbox-123",
        snapshotName: "my-snapshot",
        maxWaitMs: 5,
      }),
    );
    await vi.runAllTimersAsync();
    const result = await settled;

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.e).toBeInstanceOf(IsolaTimeoutError);
      expect((result.e as IsolaTimeoutError).message).toMatch(/did not reach complete state within 5ms/);
    }
    // Snapshot timeouts must not auto-DELETE, caller decides cleanup.
    expect(stub.calls.filter((c) => c.method === "DELETE")).toHaveLength(0);
  });

  it("raises IsolaTimeoutError when 404 persists past the deadline", async () => {
    spyMonotonicAdvancingBy(2000);

    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse({ detail: "not found" }, { status: 404 }),
      jsonResponse({ detail: "not found" }, { status: 404 }),
      jsonResponse({ detail: "not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const settled = settle(
      client.rootfsSnapshots.create({
        sandboxId: "sandbox-123",
        snapshotName: "my-snapshot",
        maxWaitMs: 5,
      }),
    );
    await vi.runAllTimersAsync();
    const result = await settled;

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.e).toBeInstanceOf(IsolaTimeoutError);
      expect((result.e as IsolaTimeoutError).message).toMatch(/did not reach complete state within 5ms/);
    }
  });
});

describe("RootfsSnapshots signal forwarding", () => {
  it("get() forwards req.signal to the GET request", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(jsonResponse(rootfsSnapshotResponseFixture()));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.rootfsSnapshots.get("snapshot-123", { signal: ctrl.signal });
    expect(stub.calls[0]?.signal).toBeDefined();
  });

  it("create() forwards req.signal to the POST request", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(jsonResponse(rootfsSnapshotResponseFixture(), { status: 201 }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    await client.rootfsSnapshots.create(
      { sandboxId: "sandbox-123", snapshotName: "my-snapshot" },
      { signal: ctrl.signal },
    );
    expect(stub.calls[0]?.signal).toBeDefined();
  });

  it("create() forwards req.signal to polling GETs (waitUntilComplete)", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse(makeRootfsSnapshotResponse("Succeeded")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const promise = client.rootfsSnapshots.create(
      { sandboxId: "sandbox-123", snapshotName: "my-snapshot" },
      { signal: ctrl.signal },
    );
    await vi.runAllTimersAsync();
    await promise;
    expect(stub.calls[0]?.signal).toBeDefined();
    expect(stub.calls[1]?.signal).toBeDefined();
  });
});

describe("waitUntilComplete non-404 error propagation", () => {
  it("re-throws InternalError without retrying", async () => {
    // Non-NotFound errors during polling must surface immediately rather
    // than being absorbed by the 404 retry branch.
    const stub = makeStubFetch(
      jsonResponse(makeRootfsSnapshotResponse("Pending"), { status: 201 }),
      jsonResponse({ detail: "operator unreachable" }, { status: 500 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });

    const settled = settle(
      client.rootfsSnapshots.create({
        sandboxId: "sandbox-123",
        snapshotName: "my-snapshot",
      }),
    );
    await vi.runAllTimersAsync();
    const result = await settled;

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.e).toBeInstanceOf(InternalError);
      expect((result.e as InternalError).message).toContain("operator unreachable");
    }
  });
});
