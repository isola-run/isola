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

// Mirrors sdks/python/tests/e2e/test_client_disconnect.py.
// Verifies that a client that disconnects mid-stream (via abort or short
// timeout) does not break the running command. The sidecar's ctx.Done()
// path should reset cleanly so subsequent operations on the same command
// continue to work.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: client disconnect mid-poll", () => {
  let client: Isola;
  let sandbox: Sandbox;
  const created: string[] = [];

  beforeAll(async () => {
    client = new Isola({ url: ISOLA_URL });
    sandbox = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sandbox.id);
    sandbox = await waitForRunning(client, sandbox.id);
  }, 90_000);

  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  it("aborting a wait() mid-poll does not break the command", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "30"]);
    try {
      // Trigger the long-poll status path, then abort the SDK call before
      // it can complete (mirrors httpx ReadTimeout in Python). The sidecar
      // sees ctx.Done() and the gateway short-circuits.
      const ctrl = new AbortController();
      const waitPromise = cmd.wait({ signal: ctrl.signal });
      setTimeout(() => ctrl.abort(new Error("client disconnect")), 2_000);
      let raised = false;
      try {
        await waitPromise;
      } catch {
        raised = true;
      }
      expect(raised).toBe(true);

      // After the abort, the command should still be running and usable.
      expect(await cmd.exitCode()).toBeNull();

      // Kill cleanly and verify final exit code reports.
      await cmd.kill();
      const code = await cmd.wait();
      expect(code).not.toBe(0);
    } finally {
      // Best-effort cleanup if the kill above already succeeded.
      try {
        await cmd.kill();
      } catch {
        // ignore
      }
    }
  }, 30_000);

  it("multiple aborts mid-poll do not corrupt command state", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "30"]);
    try {
      for (let i = 0; i < 3; i++) {
        const ctrl = new AbortController();
        const waitPromise = cmd.wait({ signal: ctrl.signal });
        setTimeout(() => ctrl.abort(new Error(`abort ${i}`)), 1_000);
        let raised = false;
        try {
          await waitPromise;
        } catch {
          raised = true;
        }
        expect(raised).toBe(true);
      }

      // Command should still be alive.
      expect(await cmd.exitCode()).toBeNull();

      await cmd.kill();
      const code = await cmd.wait();
      expect(code).not.toBe(0);
    } finally {
      try {
        await cmd.kill();
      } catch {
        // ignore
      }
    }
  }, 30_000);
});
