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

// Mirrors sdks/python/tests/e2e/test_stream_idle.py.
// Verifies stdout/stderr streams survive idle periods longer than the
// per-write deadline (10s). The 50s/80s gap variants from Python are
// marked @pytest.mark.slow there; we omit them here because the file's
// 120s testTimeout is already tight against the 75s sidecar WriteTimeout.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: stream idle gap", () => {
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

  it("stream survives a 15s idle gap", async () => {
    const cmd = await sandbox.commands.spawn(["sh", "-c", "echo before; sleep 15; echo after"]);

    const chunks: string[] = [];
    for await (const chunk of cmd.stdout) chunks.push(chunk);
    const output = chunks.join("");

    expect(output).toContain("before\n");
    expect(output).toContain("after\n");
    expect(await cmd.wait()).toBe(0);
  }, 45_000);

  it("stream survives a 20s idle gap", async () => {
    const cmd = await sandbox.commands.spawn(["sh", "-c", "echo first; sleep 20; echo second"]);
    const output = await cmd.stdout.read();
    expect(output).toContain("first\n");
    expect(output).toContain("second\n");
    expect(await cmd.wait()).toBe(0);
  }, 45_000);

  it("zero output for 30s then a burst", async () => {
    // No initial output → DeadlineWriter's per-write deadline never fires;
    // only the server-level WriteTimeout applies.
    const cmd = await sandbox.commands.spawn(["sh", "-c", "sleep 30; echo burst"]);
    const output = await cmd.stdout.read();
    expect(output).toContain("burst\n");
    expect(await cmd.wait()).toBe(0);
  }, 60_000);

  it("multiple 12s idle gaps in sequence", async () => {
    // Each gap exceeds the 10s per-write deadline. If each gap consumed a
    // reconnect, the SDK's MAX_RECONNECTS=5 budget would be threatened.
    const cmd = await sandbox.commands.spawn([
      "sh",
      "-c",
      "echo a; sleep 12; echo b; sleep 12; echo c; sleep 12; echo d",
    ]);
    const output = await cmd.stdout.read();
    for (const ch of ["a", "b", "c", "d"]) {
      expect(output).toContain(`${ch}\n`);
    }
    expect(await cmd.wait()).toBe(0);
  }, 90_000);

  it("clean EOF: fast command completes without reconnect logic", async () => {
    const cmd = await sandbox.commands.spawn(["echo", "quick"]);
    const output = await cmd.stdout.read();
    expect(output).toBe("quick\n");
    expect(await cmd.wait()).toBe(0);
  }, 10_000);

  // Skipped: the 50s / 80s idle-gap variants from Python (marked @slow).
  // They cross the gateway 45s / sidecar 75s WriteTimeout. Vitest's default
  // testTimeout for this file would need to be raised significantly to
  // accommodate them. See sdks/python/tests/e2e/test_stream_idle.py if you
  // want to bring them back.
});
