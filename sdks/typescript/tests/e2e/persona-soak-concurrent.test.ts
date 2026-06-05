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

// Soak / concurrency: N sandboxes created in parallel, each runs a tiny
// command, then cleaned up. Stresses controller throughput and the SDK's
// concurrent-request behavior.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: persona, soak / concurrent sandbox creation", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    // Best-effort: drain any sandboxes leaked by failing tests.
    await Promise.all(created.map((id) => safeDelete(client, id)));
  });

  // Single-node Kind cluster: 10 minimal sandboxes is enough to exercise
  // concurrency without OOMing the host. Tune down if cluster gets sluggish.
  const N = 10;

  it(`creates ${N} sandboxes concurrently, runs a command in each, cleans up`, async () => {
    const sandboxes: Sandbox[] = await Promise.all(
      Array.from({ length: N }, () =>
        client.sandboxes.create({ image: "alpine:3.21" }).then((sb) => {
          created.push(sb.id);
          return sb;
        }),
      ),
    );
    expect(sandboxes).toHaveLength(N);

    // Wait for all to reach Running. Some may already be Running from
    // create()'s wait, but list() / cache eventual consistency makes a
    // re-check cheap insurance.
    await Promise.all(sandboxes.map((sb) => waitForRunning(client, sb.id)));

    // Run a unique command in each, confirms per-sandbox isolation under
    // concurrency (no cross-talk on the streaming pipeline).
    const results = await Promise.all(sandboxes.map((sb, idx) => sb.commands.run(["sh", "-c", `echo soak_${idx}`])));
    for (let i = 0; i < N; i++) {
      const r = results[i];
      expect(r?.exitCode).toBe(0);
      expect(r?.stdout).toBe(`soak_${i}\n`);
    }

    // All N IDs should appear in list().
    const summaries = await client.sandboxes.list();
    const listed = new Set(summaries.map((s) => s.id));
    for (const sb of sandboxes) {
      expect(listed.has(sb.id)).toBe(true);
    }

    // Concurrent delete, verifies the gateway handles bursts of DELETEs.
    await Promise.all(sandboxes.map((sb) => sb.delete()));
  }, 300_000);
});
