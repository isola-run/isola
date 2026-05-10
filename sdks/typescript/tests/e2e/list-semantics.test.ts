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

// Verifies list() semantics: creating N sandboxes makes them all eventually
// appear in list(); deleting them eventually makes them disappear. Tests the
// eventual-consistency contract that callers rely on for dashboards / cleanup
// jobs.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, NotFoundError, type Sandbox } from "../../src";
import { ISOLA_URL, POLL_INTERVAL_MS, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: list() semantics", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  // Create N sandboxes, eventually-consistently observe all N in list(),
  // then delete them and observe all N disappear.
  it("create N -> list shows N; delete N -> list eventually omits all N", async () => {
    const N = 5;
    const sandboxes: Sandbox[] = await Promise.all(
      Array.from({ length: N }, () =>
        client.sandboxes.create({ image: "alpine:3.21" }).then((sb) => {
          created.push(sb.id);
          return sb;
        }),
      ),
    );
    const wantIds = new Set(sandboxes.map((s) => s.id));

    // Eventually consistent: poll list() until all N are visible.
    const visibleDeadline = performance.now() + 60_000;
    let allVisible = false;
    while (performance.now() < visibleDeadline) {
      const summaries = await client.sandboxes.list();
      const listedIds = new Set(summaries.map((s) => s.id));
      if ([...wantIds].every((id) => listedIds.has(id))) {
        allVisible = true;
        break;
      }
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
    }
    expect(allVisible).toBe(true);

    // Verify each summary has the fields callers depend on.
    const summaries = await client.sandboxes.list();
    const ours = summaries.filter((s) => wantIds.has(s.id));
    expect(ours).toHaveLength(N);
    for (const s of ours) {
      expect(s.id).toBeTruthy();
      expect(s.status).toBeTruthy();
      expect(s.creationTimestamp).toBeInstanceOf(Date);
    }

    // Wait for them to fully become Running so the delete path is exercised
    // on a real pod (not just a pending one).
    await Promise.all(sandboxes.map((sb) => waitForRunning(client, sb.id)));

    // Delete in parallel.
    await Promise.all(sandboxes.map((sb) => sb.delete()));

    // Eventually-consistent removal: list() should omit all of them, and
    // get() should return NotFoundError. Allow up to 60s for finalizers.
    const goneDeadline = performance.now() + 60_000;
    let allGone = false;
    while (performance.now() < goneDeadline) {
      const remaining = await client.sandboxes.list();
      const stillThere = remaining.filter((s) => wantIds.has(s.id));
      if (stillThere.length === 0) {
        allGone = true;
        break;
      }
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
    }
    expect(allGone).toBe(true);

    // Final get() on each must be NotFoundError.
    for (const sb of sandboxes) {
      await expect(client.sandboxes.get(sb.id)).rejects.toThrow(NotFoundError);
    }
  }, 240_000);
});
