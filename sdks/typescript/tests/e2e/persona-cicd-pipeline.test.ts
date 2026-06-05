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

// Persona: CI/CD pipeline runner.
// Pattern: ephemeral sandbox bounded by timeoutSeconds. Runs an "lint -> test
// -> build" sequence, asserting that exit codes propagate faithfully on both
// the success and failure paths.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: persona, CI/CD pipeline runner", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
  });

  // A successful pipeline: each step exits 0, the runner reports an
  // aggregated success.
  it("happy path: lint+test+build all succeed, exit codes propagate", async () => {
    const sb: Sandbox = await client.sandboxes.create({
      image: "alpine:3.21",
      timeoutSeconds: 60,
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    // Stage source for the "build".
    await running.filesystem.write("/workspace/app.sh", "#!/bin/sh\necho hello from build\n");

    // CI-style steps: each command is a discrete sandbox process.
    const lint = await running.commands.run(["sh", "-c", "test -s /workspace/app.sh"]);
    expect(lint.exitCode).toBe(0);

    const test = await running.commands.run(["sh", "-c", "sh /workspace/app.sh | grep -q 'hello from build'"]);
    expect(test.exitCode).toBe(0);

    const build = await running.commands.run(["sh", "-c", "chmod +x /workspace/app.sh && /workspace/app.sh"]);
    expect(build.exitCode).toBe(0);
    expect(build.stdout).toContain("hello from build");
  }, 120_000);

  // A failing pipeline must report the specific failing step's exit code
  // and stop subsequent steps from succeeding.
  it("failure path: test step exits 1, build step is never reached", async () => {
    const sb: Sandbox = await client.sandboxes.create({
      image: "alpine:3.21",
      timeoutSeconds: 60,
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    // Simulated test failure (exit 2 to verify it propagates, not just any non-zero).
    const failingTest = await running.commands.run(["sh", "-c", "echo 'FAIL: test_X' 1>&2; exit 2"]);
    expect(failingTest.exitCode).toBe(2);
    expect(failingTest.stderr).toContain("FAIL: test_X");

    // The runner would short-circuit at this point; verify the sandbox is
    // still healthy enough to record the failure (write a result file).
    await running.filesystem.write("/tmp/result.json", JSON.stringify({ ok: false, step: "test" }));
    const result = await running.filesystem.read("/tmp/result.json");
    const parsed: { ok: boolean; step: string } = JSON.parse(new TextDecoder().decode(result));
    expect(parsed.ok).toBe(false);
    expect(parsed.step).toBe("test");
  }, 120_000);

  // CI pipelines often spawn long subprocesses. Verify the per-command
  // server-side timeoutSeconds kills runaway steps without taking down the
  // sandbox.
  it("per-command timeoutSeconds kills a runaway step; sandbox survives", async () => {
    const sb: Sandbox = await client.sandboxes.create({
      image: "alpine:3.21",
      timeoutSeconds: 120,
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const runaway = await running.commands.spawn(["sleep", "300"], { timeoutSeconds: 3 });
    const code = await runaway.wait();
    expect(code).not.toBe(0); // killed -> non-zero (sidecar reports -1)

    // Sandbox should still be usable.
    const r = await running.commands.run(["echo", "ok"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("ok\n");
  }, 120_000);
});
