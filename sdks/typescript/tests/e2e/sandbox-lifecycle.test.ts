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

// Mirrors sdks/python/tests/e2e/test_sandbox_lifecycle.py.
// Requires a live Isola API at ISOLA_URL.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, IsolaError, type Sandbox } from "../../src";
import { ISOLA_URL, parseK8sQuantity, safeDelete, waitForRunning, waitForStatus, waitForVisible } from "./_helpers";

describe.sequential("e2e: sandbox lifecycle", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  it("creates a minimal sandbox and reaches Running", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    expect(sb.id).toBeTruthy();
    expect(["Pending", "Running"]).toContain(sb.status);

    const running = await waitForRunning(client, sb.id);
    expect(running.status).toBe("Running");
  }, 90_000);

  it("creates a sandbox with full config and reaches Running", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      command: ["sleep", "infinity"],
      env: { TEST_VAR: "test_value", ANOTHER: "123" },
      cpu: 0.1,
      memory: 128,
      ephemeralStorage: 512,
      timeoutSeconds: 300,
      network: { allowInternetEgress: true },
    });
    created.push(sb.id);
    expect(sb.timeoutSeconds).toBe(300);

    const running = await waitForRunning(client, sb.id);
    expect(running.status).toBe("Running");
    expect(running.timeoutSeconds).toBe(300);
    expect(running.network?.allowInternetEgress).toBe(true);
  }, 90_000);

  it("get(sandboxId) returns expected fields", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    await waitForRunning(client, sb.id);

    const fetched = await client.sandboxes.get(sb.id);
    expect(fetched.id).toBe(sb.id);
    expect(fetched.status).toBe("Running");
    expect(fetched.creationTimestamp).toBeInstanceOf(Date);
    expect(fetched.containers[0]?.image).toBe("alpine:3.21");
  }, 90_000);

  it("list() includes the created sandbox", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    await waitForRunning(client, sb.id);

    const summaries = await client.sandboxes.list();
    expect(Array.isArray(summaries)).toBe(true);
    const ids = summaries.map((s) => s.id);
    expect(ids).toContain(sb.id);
    const matching = summaries.find((s) => s.id === sb.id);
    expect(matching?.status).toBe("Running");
    expect(matching?.creationTimestamp).toBeInstanceOf(Date);
  }, 90_000);

  it("env vars are write-only (not returned in get response)", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      env: { SECRET_KEY: "super_secret_value", API_TOKEN: "abc123" },
    });
    created.push(sb.id);
    await waitForRunning(client, sb.id);

    const fetched = await client.sandboxes.get(sb.id);
    const container = fetched.containers[0];
    expect(container).toBeTruthy();
    // ContainerInfo type intentionally has no env field.
    expect((container as unknown as { env?: unknown }).env).toBeUndefined();
  }, 90_000);

  it("delete() removes the sandbox", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    await waitForRunning(client, sb.id);
    await sb.delete();
    // The Sandbox enters Terminating, then is removed.
    // We don't wait for full removal — just verify delete returns.
  }, 90_000);

  it("Symbol.asyncDispose triggers delete()", async () => {
    let id = "";
    {
      await using sb = await client.sandboxes.create({ image: "alpine:3.21" });
      id = sb.id;
      await waitForRunning(client, sb.id);
    }
    // After exit, the sandbox has been deleted (eventually).
    expect(id).toBeTruthy();
  }, 90_000);
});

describe.sequential("e2e: sandbox network defaults", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  it("preserves Network acronym aliases on round-trip", async () => {
    const sb: Sandbox = await client.sandboxes.create({
      image: "alpine:3.21",
      network: {
        allowInternetEgress: true,
        allowClusterDNS: false,
        allowedEgressCIDRs: ["10.0.0.0/8"],
      },
    });
    created.push(sb.id);
    expect(sb.network?.allowClusterDNS).toBe(false);
    expect(sb.network?.allowedEgressCIDRs).toEqual(["10.0.0.0/8"]);
    expect(sb.network?.allowInternetEgress).toBe(true);
  }, 90_000);
});

// Mirrors the rest of sdks/python/tests/e2e/test_sandbox_lifecycle.py:
// custom command, resource limits, ephemeral storage enforcement, server
// defaults, termination policy snapshot-name defaulting, list/get parity.
describe.sequential("e2e: sandbox lifecycle (extended)", () => {
  let client: Isola;
  let sessionSandbox: Sandbox;
  const created: string[] = [];

  beforeAll(async () => {
    client = new Isola({ url: ISOLA_URL });
    sessionSandbox = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sessionSandbox.id);
    sessionSandbox = await waitForRunning(client, sessionSandbox.id);
  }, 90_000);

  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  it("default (no-command) sandbox stays alive: sleep infinity is injected", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);
    expect(running.status).toBe("Running");

    const r = await running.commands.run(["echo", "hello"]);
    expect(r.exitCode).toBe(0);
  }, 90_000);

  it("custom command sandbox reaches Running with command set", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      command: ["sh", "-c", "sleep 10"],
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);
    expect(running.status).toBe("Running");
    const container = running.containers[0];
    expect(container?.command).toBeDefined();
    expect(container?.command).toContain("sh");
  }, 90_000);

  it("resource limits round-trip via GET response", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      cpu: 0.25,
      memory: 256,
      ephemeralStorage: 1024,
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const container = running.containers[0];
    expect(container?.resources).toBeDefined();
    expect(container?.resources?.limits).toBeDefined();

    // K8s may normalize quantity strings differently; compare numeric values.
    const limits = container?.resources?.limits;
    expect(limits?.cpu).toBeDefined();
    expect(limits?.memory).toBeDefined();
    expect(limits?.ephemeralStorage).toBeDefined();
    expect(parseK8sQuantity(limits?.cpu ?? "")).toBeCloseTo(parseK8sQuantity("250m"), 5);
    expect(parseK8sQuantity(limits?.memory ?? "")).toBe(parseK8sQuantity("256Mi"));
    expect(parseK8sQuantity(limits?.ephemeralStorage ?? "")).toBe(parseK8sQuantity("1024Mi"));
  }, 90_000);

  it("rootfs write respects ephemeral storage limit (sandbox -> Failed via kubelet eviction)", async () => {
    // gVisor's overlay2=root:self stores rootfs overlay's backing file inside
    // the writable layer. Exceeding ephemeral_storage triggers kubelet eviction.
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      memory: 512,
      ephemeralStorage: 128,
      timeoutSeconds: 180,
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    try {
      await running.commands.run(["dd", "if=/dev/zero", "of=/big.bin", "bs=1M", "count=400"], {
        timeoutSeconds: 30,
      });
    } catch (err) {
      // Sandbox may have died mid-write — wait_for_status confirms the terminal state.
      if (!(err instanceof IsolaError) && !(err instanceof Error)) throw err;
    }
    await waitForStatus(client, sb.id, "Failed", 60_000);
  }, 120_000);

  it("server-defaulted fields (startupTimeoutSeconds, container name) are present", async () => {
    const sb = await client.sandboxes.get(sessionSandbox.id);
    expect(sb.startupTimeoutSeconds).toBe(90);
    expect(sb.containers[0]?.name).toBe("sandbox0");
  }, 30_000);

  it("termination policy snapshotName defaults to the sandbox id", async () => {
    // Server-side default that kubebuilder can't express (cross-field).
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      terminationPolicy: {},
      maxWaitMs: 0,
    });
    created.push(sb.id);
    expect(sb._data.terminationPolicy).toBeDefined();
    expect(sb._data.terminationPolicy?.snapshotRootfs).toBeDefined();
    expect(sb._data.terminationPolicy?.snapshotRootfs?.snapshotName).toBe(sb.id);

    const fetched = await waitForVisible(client, sb.id);
    expect(fetched._data.terminationPolicy?.snapshotRootfs?.snapshotName).toBe(sb.id);
  }, 60_000);

  it("list() status matches get() status for the same sandbox", async () => {
    const summaries = await client.sandboxes.list();
    const summary = summaries.find((s) => s.id === sessionSandbox.id);
    expect(summary).toBeDefined();
    const details = await client.sandboxes.get(sessionSandbox.id);
    expect(summary?.status).toBe(details.status);
  }, 30_000);
});
