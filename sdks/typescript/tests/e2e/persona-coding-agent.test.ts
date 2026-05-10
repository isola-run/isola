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

// Persona: AI coding agent.
// Pattern: spawn a fresh Python sandbox, write a code file, execute it, capture
// output, delete. Repeat N times. Exercises the create -> write -> run -> delete
// loop that an AI agent's tool-use loop hits hardest.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: persona — AI coding agent (per-task sandbox)", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  // 5 iterations: each spawns a sandbox, writes a small program with a
  // verifiable output, runs it, asserts, and deletes. Mirrors an agent
  // running tool calls back-to-back without sharing state.
  it("5-iteration write/run/delete loop produces correct output every time", async () => {
    for (let i = 1; i <= 5; i++) {
      const sb = await client.sandboxes.create({ image: "python:3.12-alpine" });
      created.push(sb.id);
      const running = await waitForRunning(client, sb.id);

      // Agent generates a Python program parametrized on the iteration so
      // the output is unique per run (catches accidental cross-iter caching).
      const program = `n = ${i}\nprint("agent:" + str(n * n))\n`;
      await running.filesystem.write("/tmp/main.py", program);

      const r = await running.commands.run(["python3", "/tmp/main.py"]);
      expect(r.exitCode).toBe(0);
      expect(r.stdout).toBe(`agent:${i * i}\n`);
      expect(r.stderr).toBe("");

      await running.delete();
    }
  }, 600_000);

  // Same 5-iteration loop but the agent uses stdin instead of writing a file
  // first. Exercises the lower-friction code path many agents prefer.
  it("5-iteration run-via-stdin (no fs write) loop", async () => {
    for (let i = 1; i <= 5; i++) {
      const sb = await client.sandboxes.create({ image: "python:3.12-alpine" });
      created.push(sb.id);
      const running = await waitForRunning(client, sb.id);

      const program = `import sys\nfor line in sys.stdin:\n    print("echo:" + line.strip())\n`;
      const r = await running.commands.run(["python3", "-c", program], {
        input: `iter${i}\n`,
      });
      expect(r.exitCode).toBe(0);
      expect(r.stdout).toBe(`echo:iter${i}\n`);

      await running.delete();
    }
  }, 600_000);
});
