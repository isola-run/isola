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

// Mirrors sdks/python/tests/e2e/test_timeout.py.
// Verifies server-side enforcement of sandbox timeoutSeconds and command
// timeoutSeconds, plus operations on a stopped sandbox.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, IsolaError, NotFoundError, type Sandbox } from "../../src";
import { ISOLA_URL, POLL_INTERVAL_MS, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: timeout", () => {
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

  it("active deadline: sandbox stops or disappears after timeoutSeconds", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21", timeoutSeconds: 10 });
    created.push(sb.id);
    await waitForRunning(client, sb.id);

    const deadline = performance.now() + 30_000;
    let lastStatus: string | undefined;
    while (performance.now() < deadline) {
      try {
        const current = await client.sandboxes.get(sb.id);
        lastStatus = current.status;
        if (current.status === "Succeeded" || current.status === "Failed") return;
      } catch (err) {
        if (err instanceof NotFoundError) return;
        throw err;
      }
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
    }
    throw new Error(`Sandbox ${sb.id} did not stop or disappear within 30s (last: ${lastStatus})`);
  }, 60_000);

  it("command timeoutSeconds=3: sidecar kills it with non-zero exit", async () => {
    const cmd = await sessionSandbox.commands.spawn(["sleep", "300"], { timeoutSeconds: 3 });
    expect(await cmd.exitCode()).toBeNull();

    const code = await cmd.wait();
    // Signal-killed processes should have a non-zero exit code (sidecar reports -1).
    expect(code).not.toBe(0);
  }, 60_000);

  it("no timeoutSeconds: sandbox stays running with null timeout", async () => {
    const sb = await client.sandboxes.get(sessionSandbox.id);
    expect(sb.status).toBe("Running");
    expect(sb.timeoutSeconds).toBeNull();
  }, 30_000);

  it("operations on a timed-out sandbox fail with NotFoundError or IsolaError", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21", timeoutSeconds: 10 });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const deadline = performance.now() + 30_000;
    let sandboxGone = false;
    let lastStatus: string | undefined;
    while (performance.now() < deadline) {
      try {
        const current = await client.sandboxes.get(sb.id);
        lastStatus = current.status;
        if (current.status === "Succeeded" || current.status === "Failed") {
          sandboxGone = true;
          break;
        }
      } catch (err) {
        if (err instanceof NotFoundError) {
          sandboxGone = true;
          break;
        }
        throw err;
      }
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
    }

    expect(sandboxGone).toBe(true);
    if (!sandboxGone) {
      throw new Error(`Sandbox ${sb.id} did not reach terminal state within 30s (last: ${lastStatus})`);
    }

    // Attempting a command on a stopped/deleted sandbox should error.
    let caught: unknown;
    try {
      await running.commands.spawn(["echo", "should fail"]);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeDefined();
    expect(caught instanceof NotFoundError || caught instanceof IsolaError).toBe(true);
  }, 240_000);
});
