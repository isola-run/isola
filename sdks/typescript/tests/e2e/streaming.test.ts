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

// Mirrors sdks/python/tests/e2e/test_streaming.py.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: streaming", () => {
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

  it("streams stdout chunk-by-chunk before exit", async () => {
    const cmd = await sandbox.commands.spawn(["sh", "-c", "for i in 1 2 3 4 5; do echo line-$i; sleep 0.05; done"]);
    const chunks: string[] = [];
    for await (const chunk of cmd.stdout) chunks.push(chunk);
    const out = chunks.join("");
    expect(out).toContain("line-1");
    expect(out).toContain("line-5");
    expect(await cmd.wait()).toBe(0);
  }, 60_000);

  it("stdout single-use guard fires on second iteration", async () => {
    const cmd = await sandbox.commands.spawn(["echo", "hi"]);
    await cmd.stdout.read();
    await expect(cmd.stdout.read()).rejects.toThrow(/single-use/);
    await cmd.wait();
  }, 30_000);

  it("read() after iteration completes is forbidden", async () => {
    const cmd = await sandbox.commands.spawn(["echo", "ok"]);
    const chunks: string[] = [];
    for await (const chunk of cmd.stdout) chunks.push(chunk);
    expect(chunks.join("")).toBe("ok\n");
    await expect(cmd.stdout.read()).rejects.toThrow(/single-use/);
    await cmd.wait();
  }, 30_000);

  it("captures stderr separately from stdout", async () => {
    const cmd = await sandbox.commands.spawn(["sh", "-c", "echo to-stdout; echo to-stderr 1>&2"]);
    const [stdout, stderr] = await Promise.all([cmd.stdout.read(), cmd.stderr.read()]);
    expect(stdout).toBe("to-stdout\n");
    expect(stderr).toBe("to-stderr\n");
    expect(await cmd.wait()).toBe(0);
  }, 30_000);

  it("output arrives incrementally while the command is still running", async () => {
    // Sleeps create observable windows; we expect at least one chunk to arrive
    // while exitCode() is still null, proving no end-of-process buffering.
    const cmd = await sandbox.commands.spawn(["sh", "-c", "echo line1; sleep 0.5; echo line2; sleep 0.5; echo line3"]);
    let receivedWhileRunning = false;
    const chunks: string[] = [];
    for await (const chunk of cmd.stdout) {
      chunks.push(chunk);
      if (!receivedWhileRunning && (await cmd.exitCode()) === null) {
        receivedWhileRunning = true;
      }
    }
    const output = chunks.join("");
    expect(output).toContain("line1\n");
    expect(output).toContain("line2\n");
    expect(output).toContain("line3\n");
    expect(receivedWhileRunning).toBe(true);
    expect(await cmd.wait()).toBe(0);
  }, 60_000);

  it("multi-line stderr output", async () => {
    const cmd = await sandbox.commands.spawn(["sh", "-c", "echo err1 >&2; echo err2 >&2"]);
    await cmd.wait();
    const output = await cmd.stderr.read();
    expect(output).toContain("err1\n");
    expect(output).toContain("err2\n");
  }, 60_000);

  it("stream completes cleanly for a short-lived command", async () => {
    const cmd = await sandbox.commands.spawn(["echo", "done"]);
    const output = await cmd.stdout.read();
    expect(output).toContain("done\n");
    expect(await cmd.wait()).toBe(0);
  }, 60_000);

  it("read() after wait() returns immediately", async () => {
    const cmd = await sandbox.commands.spawn(["echo", "fast"]);
    await cmd.wait();
    const output = await cmd.stdout.read();
    expect(output).toBe("fast\n");
  }, 10_000);

  it("read() returns empty string for a command that produces no output", async () => {
    const cmd = await sandbox.commands.spawn(["true"]);
    const output = await cmd.stdout.read();
    expect(output).toBe("");
  }, 10_000);
});
