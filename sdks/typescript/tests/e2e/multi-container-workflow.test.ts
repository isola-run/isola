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

// Realistic multi-container workflow: a python http.server in container A logs
// to a file; container B curls it; we verify both the cross-container HTTP
// path AND that each container has its own filesystem view.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: multi-container — realistic server+client workflow", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  // python serves /server-data; alpine client wgets via 127.0.0.1 (shared
  // network namespace). Each container's /tmp is isolated; the server
  // writing to /tmp/marker_server must NOT show up in the client's /tmp.
  it("server emits HTTP from /tmp; client reads it cross-container", async () => {
    const sb = await client.sandboxes.create({
      containers: [
        {
          name: "server",
          image: "python:3.12-slim",
          // Serve from /srv. Pre-populate via filesystem.write below.
          command: ["python3", "-m", "http.server", "8000", "--directory", "/srv"],
        },
        {
          name: "client",
          image: "alpine:3.21",
        },
      ],
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    // Stage a payload in the server's /srv. The python http.server picks
    // it up dynamically because it lists the directory at every request.
    const PAYLOAD = "hello from server container\n";
    await running.filesystem.write("/srv/page.txt", PAYLOAD, { container: "server" });

    // Give the server a beat to bind.
    await new Promise((r) => setTimeout(r, 2_000));

    // Client curls (well, wgets) the server over 127.0.0.1 (shared netns).
    const wget = await running.commands.run(["wget", "-qO-", "http://127.0.0.1:8000/page.txt"], {
      container: "client",
      timeoutSeconds: 10,
    });
    expect(wget.exitCode).toBe(0);
    expect(wget.stdout).toBe(PAYLOAD);

    // Per-container filesystem isolation: write a marker into server's
    // /tmp; the client must NOT see it (different rootfs / mount ns).
    await running.filesystem.write("/tmp/marker_server", "server-only", { container: "server" });

    // server sees it.
    const serverSees = await running.commands.run(["cat", "/tmp/marker_server"], { container: "server" });
    expect(serverSees.exitCode).toBe(0);
    expect(serverSees.stdout).toBe("server-only");

    // client does NOT see it (file missing -> non-zero exit).
    const clientMisses = await running.commands.run(
      ["sh", "-c", "test -f /tmp/marker_server && echo present || echo absent"],
      { container: "client" },
    );
    expect(clientMisses.exitCode).toBe(0);
    expect(clientMisses.stdout.trim()).toBe("absent");
  }, 240_000);
});
