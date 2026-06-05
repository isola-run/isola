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

// Cancellation correctness + error-recovery scenarios sharing a long-lived
// sandbox. Verifies the sandbox stays healthy after timeouts, kills, and
// failed commands so a user / agent can continue using it.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, IsolaTimeoutError, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: cancellation correctness + error recovery", () => {
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
  });

  // Client-side wait timeout → IsolaTimeoutError; then kill() works; then
  // the sandbox is still alive for subsequent commands. This is the
  // "agent gives up on a slow tool call" pattern.
  it("wait(timeoutMs=100) on slow proc throws IsolaTimeoutError; kill then continue", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "300"]);
    await expect(cmd.wait({ timeoutMs: 100 })).rejects.toThrow(IsolaTimeoutError);

    // The process is still running server-side; client-side kill() must
    // succeed and the sandbox must remain usable.
    await cmd.kill();
    const code = await cmd.wait();
    expect(code).not.toBe(0); // signal-killed

    // Sandbox still healthy.
    const r = await sandbox.commands.run(["echo", "still-here"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("still-here\n");
  }, 60_000);

  // AbortSignal-based cancellation: user aborts a long-running command via
  // the SDK's RequestOptions.signal. The wait should reject promptly.
  it("AbortSignal cancels a wait() promptly", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "300"]);
    const ac = new AbortController();
    const waitPromise = cmd.wait({ signal: ac.signal });

    // Give the long-poll a moment to start, then abort.
    setTimeout(() => ac.abort(), 50);
    await expect(waitPromise).rejects.toThrow();

    await cmd.kill();
    await cmd.wait();

    // Sandbox still usable.
    const r = await sandbox.commands.run(["echo", "after-abort"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("after-abort\n");
  }, 60_000);

  // Error recovery: a failing command (nonexistent binary) does not poison
  // the sandbox. Mirrors how an agent retries after a typo.
  it("nonexistent binary returns non-zero; sandbox still healthy after", async () => {
    const r = await sandbox.commands.run(["nonexistent-binary-xyz"]);
    expect(r.exitCode).not.toBe(0);
    // stderr should mention the failure; exact wording depends on the
    // shell/exec path the sidecar uses, so we just assert non-empty.
    // (Some paths emit empty stderr and rely solely on exit code 127.)
    // Be lenient: either stderr is non-empty or exit code is 127-ish.
    if (r.stderr.length === 0) {
      expect([126, 127]).toContain(r.exitCode);
    }

    // Recovery: a normal command works on the same sandbox.
    const ok = await sandbox.commands.run(["echo", "recovered"]);
    expect(ok.exitCode).toBe(0);
    expect(ok.stdout).toBe("recovered\n");
  }, 60_000);

  // Error recovery: a command that exits with a specific non-zero code.
  // Then immediately a successful command. Verifies no stuck state in the
  // sidecar between commands.
  it("explicit exit 42 followed by exit 0 works on the same sandbox", async () => {
    const fail = await sandbox.commands.run(["sh", "-c", "echo bad 1>&2; exit 42"]);
    expect(fail.exitCode).toBe(42);
    expect(fail.stderr).toBe("bad\n");

    const ok = await sandbox.commands.run(["sh", "-c", "echo good"]);
    expect(ok.exitCode).toBe(0);
    expect(ok.stdout).toBe("good\n");
  }, 60_000);

  // Filesystem error followed by recovery: a read of a missing file
  // throws; a subsequent write+read on the same sandbox succeeds.
  it("missing-file read fails; subsequent write+read on same sandbox succeeds", async () => {
    await expect(sandbox.filesystem.read("/tmp/no-such-file.txt")).rejects.toThrow();

    await sandbox.filesystem.write("/tmp/healthy.txt", "healthy");
    const bytes = await sandbox.filesystem.read("/tmp/healthy.txt");
    expect(new TextDecoder().decode(bytes)).toBe("healthy");
  }, 60_000);
});
