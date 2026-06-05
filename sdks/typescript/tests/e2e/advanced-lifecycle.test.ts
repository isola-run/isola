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

// Mirrors sdks/python/tests/e2e/test_advanced_lifecycle.py.
// Covers multi-sandbox coexistence, delete-with-running-command, short-lived
// command sandbox transitioning to Succeeded, crashed container -> Failed,
// and list() always returns an array.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, NotFoundError, type Sandbox } from "../../src";
import { ISOLA_URL, POLL_INTERVAL_MS, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: advanced sandbox lifecycle", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
  });

  it("three sandboxes coexist and all appear in list()", async () => {
    const sandboxes: Sandbox[] = [];
    for (let i = 0; i < 3; i++) {
      const sb = await client.sandboxes.create({ image: "alpine:3.21" });
      created.push(sb.id);
      sandboxes.push(sb);
    }

    for (const sb of sandboxes) {
      await waitForRunning(client, sb.id);
    }

    const summaries = await client.sandboxes.list();
    const listedIds = new Set(summaries.map((s) => s.id));

    for (const sb of sandboxes) {
      expect(listedIds.has(sb.id)).toBe(true);
    }
  }, 120_000);

  it("delete sandbox with running command succeeds", async () => {
    // The finalizer cleans up the pod; the running command is lost.
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    // Start a long-running command (no need to await its completion).
    await running.commands.spawn(["sleep", "300"]);

    // Delete the sandbox without killing the command first.
    await running.delete();

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
    throw new Error(`Sandbox ${sb.id} was not deleted within 30s`);
  }, 120_000);

  it("short-lived command sandbox transitions to Succeeded", async () => {
    // Tests pod-terminated path: PodSucceeded → Succeeded.
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      command: ["true"],
      maxWaitMs: 0, // Don't wait for Running, sandbox may transition straight through.
    });
    created.push(sb.id);

    const deadline = performance.now() + 120_000;
    let lastStatus: string | undefined;
    while (performance.now() < deadline) {
      try {
        const current = await client.sandboxes.get(sb.id);
        lastStatus = current.status;
        if (current.status === "Succeeded" || current.status === "Failed") {
          expect(current.status).toBe("Succeeded");
          return;
        }
      } catch (err) {
        if (err instanceof NotFoundError) return; // already cleaned up
        throw err;
      }
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
    }
    throw new Error(`Sandbox ${sb.id} did not reach Succeeded within 120s (last: ${lastStatus})`);
  }, 180_000);

  it("crashed container sandbox transitions to Failed", async () => {
    // Verifies that the native sidecar (init container with restartPolicy: Always)
    // does not keep the pod alive in a zombie Running state after the regular
    // container exits non-zero.
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      command: ["sh", "-c", "sleep 3; exit 1"],
    });
    created.push(sb.id);

    const deadline = performance.now() + 120_000;
    let lastStatus: string | undefined;
    while (performance.now() < deadline) {
      try {
        const current = await client.sandboxes.get(sb.id);
        lastStatus = current.status;
        if (current.status === "Failed") return;
        if (current.status === "Succeeded") {
          throw new Error(`Sandbox ${sb.id} reached Succeeded but expected Failed (container exited non-zero)`);
        }
      } catch (err) {
        if (err instanceof NotFoundError) return;
        throw err;
      }
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
    }
    throw new Error(`Sandbox ${sb.id} did not reach Failed within 120s (last: ${lastStatus})`);
  }, 180_000);

  it("list() always returns an array, never null", async () => {
    const result = await client.sandboxes.list();
    expect(Array.isArray(result)).toBe(true);
  }, 30_000);
});
