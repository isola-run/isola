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

// Mirrors sdks/python/tests/e2e/test_sandbox_status.py.
// Verifies the sandbox status state machine:
//   Pending -> Running -> Terminating (on delete)
//   Pending -> Running -> Succeeded (clean exit)
//   Pending -> Running -> Failed (non-zero exit)

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, NotFoundError } from "../../src";
import { ISOLA_URL, POLL_INTERVAL_MS, safeDelete, waitForRunning, waitForStatus } from "./_helpers";

describe.sequential("e2e: sandbox status state machine", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  it("sandbox with sleep infinity reaches Running", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      command: ["sleep", "infinity"],
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);
    expect(running.status).toBe("Running");
  }, 90_000);

  it("sandbox with command=['true'] transitions to Succeeded", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      command: ["true"],
      maxWaitMs: 0,
    });
    created.push(sb.id);
    const result = await waitForStatus(client, sb.id, "Succeeded", 180_000);
    expect(result.status).toBe("Succeeded");
  }, 180_000);

  it("sandbox with non-zero exit transitions to Failed", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      command: ["sh", "-c", "exit 1"],
      maxWaitMs: 0,
    });
    created.push(sb.id);
    const result = await waitForStatus(client, sb.id, "Failed", 180_000);
    expect(result.status).toBe("Failed");
  }, 180_000);

  it("after delete, sandbox shows Terminating or has disappeared", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      command: ["sleep", "infinity"],
    });
    created.push(sb.id);
    await waitForRunning(client, sb.id);

    const running = await client.sandboxes.get(sb.id);
    await running.delete();

    // Immediately check status: Terminating, Running (not yet observed), or 404 (already gone).
    try {
      const current = await client.sandboxes.get(sb.id);
      expect(["Terminating", "Running"]).toContain(current.status);
    } catch (err) {
      if (!(err instanceof NotFoundError)) throw err;
      // Already gone — acceptable.
    }

    // The sandbox should eventually disappear.
    const deadline = performance.now() + 30_000;
    while (performance.now() < deadline) {
      try {
        await client.sandboxes.get(sb.id);
      } catch (err) {
        if (err instanceof NotFoundError) return;
        throw err;
      }
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
    }
    throw new Error(`Sandbox ${sb.id} was not fully deleted within 30s`);
  }, 120_000);
});
