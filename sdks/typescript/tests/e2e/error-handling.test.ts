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

// Mirrors sdks/python/tests/e2e/test_error_handling.py.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { BadRequestError, Commands, Filesystem, Isola, IsolaError, NotFoundError, ValidationError } from "../../src";
import { ISOLA_URL, POLL_INTERVAL_MS, safeDelete, waitForRunning } from "./_helpers";

const FAKE_SANDBOX_ID = "nonexistent-sandbox-xyz";

describe.sequential("e2e: error handling", () => {
  let client: Isola;

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    await client.close();
  });

  it("get of nonexistent sandbox raises NotFoundError", async () => {
    await expect(client.sandboxes.get("does-not-exist-12345")).rejects.toThrow(NotFoundError);
  }, 30_000);

  it("create with invalid image fails validation server-side", async () => {
    // Empty image is rejected by the server at create time (BadRequest/Validation).
    await expect(client.sandboxes.create({ image: "" } as never)).rejects.toThrow();
  }, 30_000);

  it("delete of nonexistent sandbox is idempotent (server returns 2xx)", async () => {
    // Isola's DELETE is idempotent — deleting a missing sandbox does not error.
    // Mirrors test_sandbox_lifecycle.py:test_delete_idempotent equivalent behavior.
    await client._api.requestNoContent({
      method: "DELETE",
      path: "/v1/sandboxes/does-not-exist-67890",
    });
  }, 30_000);

  it("error class types are exported from index", () => {
    // sanity check that imports work
    expect(BadRequestError).toBeDefined();
    expect(ValidationError).toBeDefined();
    expect(NotFoundError).toBeDefined();
  });

  it("commands on a nonexistent sandbox raise IsolaError with status >= 400", async () => {
    const commands = new Commands(client._api, FAKE_SANDBOX_ID);
    let caught: unknown;
    try {
      await commands.spawn(["echo", "hello"]);
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(IsolaError);
    expect((caught as IsolaError & { statusCode?: number }).statusCode).toBeGreaterThanOrEqual(400);
  }, 30_000);

  it("filesystem read on a nonexistent sandbox raises IsolaError with status >= 400", async () => {
    const fs = new Filesystem(client._api, FAKE_SANDBOX_ID);
    let caught: unknown;
    try {
      await fs.read("/tmp/anything.txt");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(IsolaError);
    expect((caught as IsolaError & { statusCode?: number }).statusCode).toBeGreaterThanOrEqual(400);
  }, 30_000);

  it("zero timeoutSeconds is rejected by validation (minimum: 1)", async () => {
    let caught: unknown;
    try {
      await client.sandboxes.create({ image: "alpine:3.21", timeoutSeconds: 0 });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeDefined();
    expect(caught instanceof ValidationError || caught instanceof BadRequestError).toBe(true);
    const code = (caught as IsolaError & { statusCode?: number }).statusCode;
    expect([400, 422]).toContain(code);
  }, 30_000);

  it("more than 3 nameservers is rejected by validation (maxItems: 3)", async () => {
    let caught: unknown;
    try {
      await client.sandboxes.create({
        image: "alpine:3.21",
        network: { nameservers: ["1.1.1.1", "8.8.8.8", "9.9.9.9", "4.4.4.4"] },
      });
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeDefined();
    expect(caught instanceof ValidationError || caught instanceof BadRequestError).toBe(true);
    const code = (caught as IsolaError & { statusCode?: number }).statusCode;
    expect([400, 422]).toContain(code);
  }, 30_000);
});

// Skipped: test_invalid_image_sandbox_fails — Python marks this @pytest.mark.skip
// citing an upstream operator bug (Pod with ImagePullBackOff stays Pending forever).
// Mirroring that behavior here is not useful until the operator is fixed; see
// sdks/python/tests/e2e/test_error_handling.py for the rationale.

describe.sequential("e2e: error handling (with sandbox)", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  it("delete is idempotent on a real sandbox (delete twice succeeds)", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    await waitForRunning(client, sb.id);

    const fresh = await client.sandboxes.get(sb.id);
    await fresh.delete();
    // Second delete should succeed without raising.
    await fresh.delete();
  }, 90_000);

  it("commands on a deleted sandbox raise IsolaError", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);
    const commands = new Commands(client._api, running.id);

    await running.delete();

    // Wait until the sandbox status changes from Running, or it's deleted.
    const deadline = performance.now() + 30_000;
    while (performance.now() < deadline) {
      try {
        const current = await client.sandboxes.get(running.id);
        if (current.status !== "Running") break;
      } catch (err) {
        if (err instanceof NotFoundError) break;
        throw err;
      }
      await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS / 2));
    }

    await expect(commands.spawn(["echo", "should fail"])).rejects.toThrow(IsolaError);
  }, 90_000);

  it("invalid command produces a non-zero exit code (sidecar accepts, exec fails)", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const r = await running.commands.run(["/usr/bin/nonexistent_binary_xyz"]);
    expect(r.exitCode).not.toBe(0);
  }, 90_000);
});
