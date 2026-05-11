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

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

// Verifies the gateway accepts query strings encoded the way Python httpx
// produces them: spaces as `+`, !'()* percent-encoded.
describe("query parameter encoding (gateway interop)", () => {
  let client: Isola;
  let sandbox: Sandbox;

  beforeAll(async () => {
    client = new Isola({ url: ISOLA_URL });
    const created = await client.sandboxes.create({ image: "alpine:3.21" });
    sandbox = await waitForRunning(client, created.id);
  }, 120_000);

  afterAll(async () => {
    if (sandbox) await safeDelete(client, sandbox.id);
    await client.close();
  });

  it("filesystem.write accepts a path containing spaces", async () => {
    const path = "/tmp/hello world.txt";
    const content = "spaces in path";
    await sandbox.filesystem.write(path, content);
    const readBack = new TextDecoder().decode(await sandbox.filesystem.read(path));
    expect(readBack).toBe(content);
  }, 60_000);

  it("filesystem.write accepts a path with percent-encoded specials", async () => {
    const path = "/tmp/a!b'c(d)e*f.txt";
    const content = "specials";
    await sandbox.filesystem.write(path, content);
    const readBack = new TextDecoder().decode(await sandbox.filesystem.read(path));
    expect(readBack).toBe(content);
  }, 60_000);

  it("filesystem.write accepts a path with `+` character", async () => {
    const path = "/tmp/a+b.txt";
    const content = "literal plus";
    await sandbox.filesystem.write(path, content);
    const readBack = new TextDecoder().decode(await sandbox.filesystem.read(path));
    expect(readBack).toBe(content);
  }, 60_000);
});
