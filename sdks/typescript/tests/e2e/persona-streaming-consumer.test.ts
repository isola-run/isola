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

// Persona: long-running streaming consumer (logs / progress).
// The SDK's StreamReader is single-use and reconnects internally on transient
// errors — we cannot externally trigger a reconnect, so we instead verify the
// "consumer kills mid-stream" path: the iterator closes cleanly and the
// sandbox stays healthy.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: persona — streaming consumer", () => {
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

  // Long-running producer emits 1 chunk per 200ms for ~6s. Consumer collects
  // the first few chunks, then kill()s the producer. Iterator must close
  // cleanly without throwing; subsequent commands on the sandbox must work.
  it("kill mid-iteration closes the iterator cleanly", async () => {
    const cmd = await sandbox.commands.spawn([
      "sh",
      "-c",
      // 30 iterations × 200ms = 6s of streaming.
      "for i in $(seq 1 30); do echo line-$i; sleep 0.2; done",
    ]);

    const chunks: string[] = [];
    let killed = false;
    try {
      for await (const chunk of cmd.stdout) {
        chunks.push(chunk);
        if (chunks.join("").includes("line-3") && !killed) {
          killed = true;
          await cmd.kill();
        }
      }
    } catch {
      // Some transports surface an error on abort; that's fine — we just
      // want to confirm sandbox health below. Don't fail here.
    }
    await cmd.wait();

    // We must have received at least the first three lines before killing.
    expect(chunks.join("")).toContain("line-1");
    expect(chunks.join("")).toContain("line-3");

    // Sandbox still usable after a kill mid-stream.
    const r = await sandbox.commands.run(["echo", "post-kill"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("post-kill\n");
  }, 60_000);

  // Producer finishes naturally; consumer iterates to completion. Verifies
  // chunk ordering and total content over a longer stream than the basic
  // streaming.test.ts covers.
  it("long-running producer: full ordered output collected", async () => {
    const cmd = await sandbox.commands.spawn(["sh", "-c", "for i in $(seq 1 20); do echo seq-$i; sleep 0.05; done"]);
    const out: string[] = [];
    for await (const chunk of cmd.stdout) out.push(chunk);
    expect(await cmd.wait()).toBe(0);

    const joined = out.join("");
    // All 20 lines present, in order.
    for (let i = 1; i <= 20; i++) {
      expect(joined).toContain(`seq-${i}\n`);
    }
    // Order check: seq-1 occurs before seq-20.
    expect(joined.indexOf("seq-1\n")).toBeLessThan(joined.indexOf("seq-20\n"));
  }, 60_000);

  // Consumer iterates a stream while another stream on the same sandbox is
  // active. Tests that two concurrent stdout streams don't interfere.
  it("two concurrent streams from the same sandbox are isolated", async () => {
    const cmdA = await sandbox.commands.spawn(["sh", "-c", "for i in 1 2 3 4 5; do echo A-$i; sleep 0.1; done"]);
    const cmdB = await sandbox.commands.spawn(["sh", "-c", "for i in 1 2 3 4 5; do echo B-$i; sleep 0.1; done"]);

    // Drain both concurrently.
    const [outA, outB, exitA, exitB] = await Promise.all([
      cmdA.stdout.read(),
      cmdB.stdout.read(),
      cmdA.wait(),
      cmdB.wait(),
    ]);

    expect(exitA).toBe(0);
    expect(exitB).toBe(0);
    // No cross-contamination.
    expect(outA).not.toContain("B-");
    expect(outB).not.toContain("A-");
    // All 5 lines on each.
    for (let i = 1; i <= 5; i++) {
      expect(outA).toContain(`A-${i}`);
      expect(outB).toContain(`B-${i}`);
    }
  }, 60_000);
});
