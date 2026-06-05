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

// Mirrors sdks/python/tests/e2e/test_advanced_commands.py.
// Covers env merge semantics, parallel commands, kill semantics, stdin-after-exit,
// concurrent stdin, default cwd, and signal-killed exit codes.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { ConflictError, Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: advanced commands", () => {
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

  it("container env vars accessible inside command", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      env: { E2E_SECRET: "expected_value" },
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const r = await running.commands.run(["sh", "-c", "echo $E2E_SECRET"]);
    expect(r.stdout).toContain("expected_value");
  }, 90_000);

  it("command env overrides container env (buildCmdEnv merge semantics)", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      env: { MY_VAR: "original" },
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const r = await running.commands.run(["sh", "-c", "echo $MY_VAR"], { env: { MY_VAR: "overridden" } });
    expect(r.stdout).toContain("overridden");
    expect(r.stdout).not.toContain("original");
  }, 90_000);

  it("writeStdin after exit raises ConflictError (409)", async () => {
    const cmd = await sandbox.commands.spawn(["true"]);
    await cmd.wait();

    await expect(cmd.writeStdin("data after exit\n")).rejects.toThrow(ConflictError);
  }, 60_000);

  it("parallel commands have isolated stdout streams", async () => {
    const cmds = await Promise.all([0, 1, 2].map((i) => sandbox.commands.spawn(["echo", `marker_${i}`])));

    for (let i = 0; i < cmds.length; i++) {
      const cmd = cmds[i] as NonNullable<(typeof cmds)[number]>;
      await cmd.wait();
      const out = await cmd.stdout.read();
      expect(out).toContain(`marker_${i}`);
      // verify no cross-contamination
      for (let j = 0; j < cmds.length; j++) {
        if (j === i) continue;
        expect(out).not.toContain(`marker_${j}`);
      }
    }
  }, 60_000);

  it("kill() yields exit code -1 (signal-killed convention in sidecar)", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "300"], { timeoutSeconds: 15 });
    expect(await cmd.exitCode()).toBeNull();

    await cmd.kill();
    const code = await cmd.wait();

    // Sidecar's convention: signal-killed processes report -1 when there's
    // no clean exit status (gVisor SIGKILL path, mirrors Python test).
    expect(code).toBe(-1);
  }, 60_000);

  it("stdout written before kill is still readable after the command terminates", async () => {
    const cmd = await sandbox.commands.spawn(["sh", "-c", "echo before_kill; sleep 300"], { timeoutSeconds: 15 });

    // Give the echo time to flush through SSE pipeline
    await new Promise((r) => setTimeout(r, 3_000));

    await cmd.kill();
    await cmd.wait();

    const output = await cmd.stdout.read();
    expect(output).toContain("before_kill");
  }, 60_000);

  it("default cwd is container root (/)", async () => {
    const r = await sandbox.commands.run(["pwd"]);
    // Alpine's default WORKDIR is /
    expect(r.stdout.trim()).toBe("/");
  }, 30_000);

  it("concurrent writeStdin calls produce non-interleaved blocks", async () => {
    // Mirrors Python test_concurrent_stdin_writes_are_non_interleaved.
    // sidecar holds stdinMu for the entire io.Copy, so each writer's chars
    // land in the pipe as one uninterrupted block. head -c exits after
    // consuming TOTAL bytes; no kill needed.
    const NUM_WRITERS = 8;
    const BLOCK_SIZE = 1024; // < 32KB io.Copy chunk → one write() per call
    const TOTAL = NUM_WRITERS * BLOCK_SIZE;

    const cmd = await sandbox.commands.spawn(["head", "-c", String(TOTAL)], { timeoutSeconds: 15 });

    const errors: unknown[] = [];
    await Promise.all(
      Array.from({ length: NUM_WRITERS }, (_, writerId) =>
        cmd.writeStdin(String.fromCharCode("A".charCodeAt(0) + writerId).repeat(BLOCK_SIZE)).catch((e) => {
          errors.push(e);
        }),
      ),
    );
    expect(errors).toEqual([]);

    await cmd.wait();
    const output = await cmd.stdout.read();
    expect(output.length).toBe(TOTAL);

    // Scan for contiguous runs. Each writer uses a unique char so no two
    // adjacent runs share a char, every run must be exactly BLOCK_SIZE.
    let i = 0;
    while (i < output.length) {
      const ch = output[i];
      let runEnd = i;
      while (runEnd < output.length && output[runEnd] === ch) runEnd++;
      const runLen = runEnd - i;
      expect(runLen).toBe(BLOCK_SIZE);
      i = runEnd;
    }
  }, 60_000);
});
