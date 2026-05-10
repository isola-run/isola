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

// Mirrors sdks/python/tests/e2e/test_commands.py.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { BadRequestError, ConflictError, Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: commands", () => {
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

  it("run('echo', 'hello') captures stdout", async () => {
    const r = await sandbox.commands.run(["echo", "hello"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("hello\n");
    expect(r.stderr).toBe("");
  }, 30_000);

  it("run() with non-zero exit code reports stderr", async () => {
    const r = await sandbox.commands.run(["sh", "-c", "echo oops 1>&2; exit 7"]);
    expect(r.exitCode).toBe(7);
    expect(r.stderr).toBe("oops\n");
    expect(r.stdout).toBe("");
  }, 30_000);

  it("run() with input pipes stdin", async () => {
    const r = await sandbox.commands.run(["cat"], { input: "hello stdin\n" });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("hello stdin\n");
  }, 30_000);

  it("run() respects env vars", async () => {
    const r = await sandbox.commands.run(["sh", "-c", "echo $MY_VAR"], { env: { MY_VAR: "value42" } });
    expect(r.stdout).toBe("value42\n");
  }, 30_000);

  it("run() respects cwd", async () => {
    const r = await sandbox.commands.run(["pwd"], { cwd: "/tmp" });
    expect(r.stdout).toBe("/tmp\n");
  }, 30_000);

  it("spawn() + stdout streaming", async () => {
    const cmd = await sandbox.commands.spawn(["sh", "-c", "for i in 1 2 3; do echo $i; sleep 0.05; done"]);
    const chunks: string[] = [];
    for await (const chunk of cmd.stdout) chunks.push(chunk);
    expect(chunks.join("")).toBe("1\n2\n3\n");
    expect(await cmd.wait()).toBe(0);
  }, 30_000);

  it("spawn() + writeStdin/closeStdin", async () => {
    const cmd = await sandbox.commands.spawn(["cat"]);
    await cmd.writeStdin("first line\n");
    await cmd.writeStdin(new TextEncoder().encode("second line\n"));
    await cmd.closeStdin();
    expect(await cmd.wait()).toBe(0);
    expect(await cmd.stdout.read()).toBe("first line\nsecond line\n");
  }, 30_000);

  it("kill() terminates a long-running command", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "300"]);
    await cmd.kill();
    const code = await cmd.wait();
    expect(code).not.toBe(0); // killed processes report non-zero
  }, 30_000);

  it("exitCode() returns null while running, then the code after wait", async () => {
    const cmd = await sandbox.commands.spawn(["sh", "-c", "sleep 0.2; exit 5"]);
    const before = await cmd.exitCode();
    expect(before === null || before === 5).toBe(true);
    expect(await cmd.wait()).toBe(5);
    expect(await cmd.exitCode()).toBe(5);
  }, 30_000);

  it("spawn validates non-empty args", async () => {
    await expect(sandbox.commands.spawn([])).rejects.toThrow(/at least one argument/);
  });

  // Mirrors test_commands.py:test_exit_code parametrized test.
  it.each([
    ["zero", "exit 0", 0],
    ["one", "exit 1", 1],
    ["arbitrary", "exit 42", 42],
    ["command-not-found", "exit 127", 127],
    ["max-byte", "exit 255", 255],
  ] as const)("exit code %s is faithfully propagated", async (_label, exitArg, expected) => {
    const r = await sandbox.commands.run(["sh", "-c", exitArg]);
    expect(r.exitCode).toBe(expected);
  }, 30_000);

  it("kill() is idempotent — second kill does not raise", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "300"], { timeoutSeconds: 15 });
    await cmd.kill();
    await cmd.wait();
    // Second kill should not throw.
    await cmd.kill();
  }, 30_000);

  it("kill() of a naturally-exited command is a no-op", async () => {
    const cmd = await sandbox.commands.spawn(["true"]);
    await cmd.wait();
    await cmd.kill();
  }, 30_000);

  it("closeStdin() called twice raises ConflictError on the second call", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "30"], { timeoutSeconds: 15 });
    try {
      await cmd.closeStdin();
      await expect(cmd.closeStdin()).rejects.toThrow(ConflictError);
    } finally {
      await cmd.kill();
    }
  }, 30_000);

  it("writeStdin() after closeStdin() raises ConflictError", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "30"], { timeoutSeconds: 15 });
    try {
      await cmd.closeStdin();
      await expect(cmd.writeStdin("hello")).rejects.toThrow(ConflictError);
    } finally {
      await cmd.kill();
    }
  }, 30_000);

  it("empty stdin write succeeds (zero bytes)", async () => {
    const cmd = await sandbox.commands.spawn(["sleep", "30"], { timeoutSeconds: 15 });
    try {
      await cmd.writeStdin(new Uint8Array(0));
    } finally {
      await cmd.kill();
    }
  }, 30_000);

  it("ISOLA_CONTAINER_NAME is stripped from user command env", async () => {
    // Operator injects ISOLA_CONTAINER_NAME into the container; the sidecar
    // strips it before exec'ing the user command.
    const r = await sandbox.commands.run(["sh", "-c", 'echo "${ISOLA_CONTAINER_NAME}"']);
    expect(r.stdout.trim()).toBe("");
  }, 30_000);

  it("command with nonexistent cwd raises BadRequestError (400)", async () => {
    await expect(sandbox.commands.run(["pwd"], { cwd: "/nonexistent_path" })).rejects.toThrow(BadRequestError);
  }, 30_000);

  it("targeting the primary container by name works (sandbox0)", async () => {
    const r = await sandbox.commands.run(["echo", "hello"], { container: "sandbox0" });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("hello");
  }, 30_000);

  it("a command with no output returns empty stdout and stderr", async () => {
    const r = await sandbox.commands.run(["true"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("");
    expect(r.stderr).toBe("");
  }, 30_000);
});
