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

// Filesystem stress: many small files concurrently. Verifies the sidecar
// can handle parallel writes/reads without dropping or corrupting data.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: filesystem stress (many small files in parallel)", () => {
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

  it("100 concurrent writes then 100 concurrent reads round-trip with integrity", async () => {
    const N = 100;
    const indices = Array.from({ length: N }, (_, i) => i);

    // Each file gets a unique deterministic payload so we can verify integrity.
    const payload = (i: number) => `file-${i}-content\n`;

    // Concurrent writes.
    await Promise.all(indices.map((i) => sandbox.filesystem.write(`/tmp/stress/${i}.txt`, payload(i))));

    // Concurrent reads, verify each matches expected payload.
    const dec = new TextDecoder();
    const reads = await Promise.all(
      indices.map((i) =>
        sandbox.filesystem.read(`/tmp/stress/${i}.txt`).then((bytes) => ({ i, content: dec.decode(bytes) })),
      ),
    );
    for (const { i, content } of reads) {
      expect(content).toBe(payload(i));
    }
  }, 180_000);

  it("ls of stress dir reports all 100 files via command", async () => {
    // After the previous test, the dir has 100 files. Verify the sidecar
    // (which works via nsenter into the container's mount ns) sees the
    // same set the filesystem API wrote.
    const r = await sandbox.commands.run(["sh", "-c", "ls /tmp/stress | wc -l"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout.trim()).toBe("100");
  }, 60_000);
});
