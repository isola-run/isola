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

// Mirrors sdks/python/tests/e2e/test_filesystem.py.

import { randomUUID } from "node:crypto";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { BadRequestError, Isola, IsolaError, NotFoundError, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

function uniquePath(prefix: string, ext = ".txt"): string {
  return `/tmp/${prefix}_${randomUUID().replaceAll("-", "")}${ext}`;
}

describe.sequential("e2e: filesystem", () => {
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

  it("write+read string round-trips", async () => {
    await sandbox.filesystem.write("/tmp/hello.txt", "hello world\n");
    const bytes = await sandbox.filesystem.read("/tmp/hello.txt");
    expect(new TextDecoder().decode(bytes)).toBe("hello world\n");
  }, 30_000);

  it("write+read bytes round-trips", async () => {
    const payload = new Uint8Array([1, 2, 3, 4, 5, 0, 255, 128]);
    await sandbox.filesystem.write("/tmp/bin.dat", payload);
    const bytes = await sandbox.filesystem.read("/tmp/bin.dat");
    expect(Array.from(bytes)).toEqual(Array.from(payload));
  }, 30_000);

  it("write creates parent directories implicitly", async () => {
    await sandbox.filesystem.write("/tmp/nested/dir/file.txt", "deep");
    const bytes = await sandbox.filesystem.read("/tmp/nested/dir/file.txt");
    expect(new TextDecoder().decode(bytes)).toBe("deep");
  }, 30_000);

  it("write overwrites existing files", async () => {
    await sandbox.filesystem.write("/tmp/over.txt", "first");
    await sandbox.filesystem.write("/tmp/over.txt", "second");
    const bytes = await sandbox.filesystem.read("/tmp/over.txt");
    expect(new TextDecoder().decode(bytes)).toBe("second");
  }, 30_000);

  it("read of missing file raises NotFoundError", async () => {
    await expect(sandbox.filesystem.read("/tmp/does-not-exist.txt")).rejects.toThrow(NotFoundError);
  }, 30_000);

  it("write+read large files", async () => {
    const payload = "x".repeat(200_000);
    await sandbox.filesystem.write("/tmp/big.txt", payload);
    const bytes = await sandbox.filesystem.read("/tmp/big.txt");
    expect(new TextDecoder().decode(bytes).length).toBe(200_000);
  }, 30_000);

  it("commands see files written via filesystem", async () => {
    await sandbox.filesystem.write("/tmp/seen.txt", "via filesystem\n");
    const r = await sandbox.commands.run(["cat", "/tmp/seen.txt"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toBe("via filesystem\n");
  }, 30_000);

  it("filesystem reads files written by commands", async () => {
    await sandbox.commands.run(["sh", "-c", "echo from-command > /tmp/cmd.txt"]);
    const bytes = await sandbox.filesystem.read("/tmp/cmd.txt");
    expect(new TextDecoder().decode(bytes)).toBe("from-command\n");
  }, 30_000);

  // Mirrors test_filesystem.py: write+read binary 256-byte payload.
  it("write+read binary preserves all 256 byte values", async () => {
    const path = uniquePath("write_binary", ".bin");
    const content = new Uint8Array(256 + 5);
    for (let i = 0; i < 256; i++) content[i] = i;
    content.set([0, 0xff, 0x80, 0xfe, 0x01], 256);

    await sandbox.filesystem.write(path, content);
    const result = await sandbox.filesystem.read(path);
    expect(Array.from(result)).toEqual(Array.from(content));
  }, 30_000);

  it("write 1 MiB file round-trips intact", async () => {
    const path = uniquePath("large", ".bin");
    const content = new Uint8Array(1024 * 1024).fill(0x78); // 'x'
    await sandbox.filesystem.write(path, content);
    const result = await sandbox.filesystem.read(path);
    expect(result.length).toBe(content.length);
    // Avoid huge equality check; sample a few bytes.
    expect(result[0]).toBe(0x78);
    expect(result[result.length - 1]).toBe(0x78);
  }, 60_000);

  it("special characters in filename round-trip", async () => {
    const unique = randomUUID().replaceAll("-", "");
    const path = `/tmp/test file (1) ${unique}.txt`;
    const content = "special chars in filename";
    await sandbox.filesystem.write(path, content);
    const result = await sandbox.filesystem.read(path);
    expect(new TextDecoder().decode(result)).toBe(content);
  }, 30_000);

  it("read of a directory returns BadRequest/IsolaError", async () => {
    let caught: unknown;
    try {
      await sandbox.filesystem.read("/tmp");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeDefined();
    expect(caught instanceof BadRequestError || caught instanceof IsolaError).toBe(true);
  }, 30_000);

  it("uploaded file is executable by commands (cross-subsystem)", async () => {
    const unique = randomUUID().replaceAll("-", "");
    const path = `/tmp/test_${unique}.sh`;
    await sandbox.filesystem.write(path, "#!/bin/sh\necho cross_subsystem_works\n");
    const r = await sandbox.commands.run(["sh", path]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("cross_subsystem_works");
  }, 30_000);

  it("relative path is resolved against the container's cwd", async () => {
    const unique = randomUUID().replaceAll("-", "");
    const filename = `relative_${unique}.txt`;
    const content = "relative path test";
    await sandbox.filesystem.write(filename, content);
    const result = await sandbox.filesystem.read(filename);
    expect(new TextDecoder().decode(result)).toBe(content);
  }, 30_000);

  it("writing a file under a path whose parent is a regular file errors", async () => {
    const unique = randomUUID().replaceAll("-", "");
    const blocker = `/tmp/${unique}/blocker`;
    await sandbox.filesystem.write(blocker, "I am a file, not a directory");
    await expect(sandbox.filesystem.write(`${blocker}/nested.txt`, "should fail")).rejects.toThrow(IsolaError);
  }, 30_000);

  it("empty (zero-byte) file write+read", async () => {
    const path = uniquePath("empty");
    await sandbox.filesystem.write(path, "");
    const result = await sandbox.filesystem.read(path);
    expect(result.length).toBe(0);
  }, 30_000);

  it("file ownership matches container UID/GID", async () => {
    // sidecar resolves uid/gid via /proc/<pid>/status, then chown's the file.
    const path = uniquePath("ownership");
    await sandbox.filesystem.write(path, "ownership test");

    const uidR = await sandbox.commands.run(["id", "-u"]);
    const gidR = await sandbox.commands.run(["id", "-g"]);
    const statR = await sandbox.commands.run(["stat", "-c", "%u %g", path]);

    const uid = uidR.stdout.trim();
    const gid = gidR.stdout.trim();
    const [fileUid, fileGid] = statR.stdout.trim().split(" ");
    expect(fileUid).toBe(uid);
    expect(fileGid).toBe(gid);
  }, 30_000);

  it("file written by a command is readable via the filesystem API (nsenter/proc consistency)", async () => {
    const unique = randomUUID().replaceAll("-", "");
    const path = `/tmp/cmd_written_${unique}.txt`;
    const expected = `written_${unique}`;
    const r = await sandbox.commands.run(["sh", "-c", `printf '%s' ${expected} > ${path}`]);
    expect(r.exitCode).toBe(0);
    const content = await sandbox.filesystem.read(path);
    expect(new TextDecoder().decode(content)).toBe(expected);
  }, 30_000);

  it("targeting the primary container by name on filesystem operations", async () => {
    const path = uniquePath("container_param");
    const content = "container param test";
    await sandbox.filesystem.write(path, content, { container: "sandbox0" });
    const result = await sandbox.filesystem.read(path, { container: "sandbox0" });
    expect(new TextDecoder().decode(result)).toBe(content);
  }, 30_000);
});
