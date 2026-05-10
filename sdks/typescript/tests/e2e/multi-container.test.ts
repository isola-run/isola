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

// Mirrors sdks/python/tests/e2e/test_multi_container.py.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: multi-container sandbox", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  it("creates a multi-container sandbox", async () => {
    const sb: Sandbox = await client.sandboxes.create({
      containers: [
        {
          name: "primary",
          image: "alpine:3.21",
          command: ["sleep", "infinity"],
          resources: {
            limits: { cpu: "100m", memory: "128Mi" },
            requests: { cpu: "100m", memory: "128Mi" },
          },
        },
        {
          name: "sidecar",
          image: "alpine:3.21",
          command: ["sleep", "infinity"],
          resources: {
            limits: { cpu: "100m", memory: "128Mi" },
            requests: { cpu: "100m", memory: "128Mi" },
          },
        },
      ],
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);
    expect(running.containers).toHaveLength(2);
    expect(running.containers.map((c) => c.name).sort()).toEqual(["primary", "sidecar"]);
  }, 90_000);

  it("commands target a specific container by name", async () => {
    const sb: Sandbox = await client.sandboxes.create({
      containers: [
        {
          name: "primary",
          image: "alpine:3.21",
          command: ["sleep", "infinity"],
          env: { FROM: "primary" },
          resources: {
            limits: { cpu: "100m", memory: "128Mi" },
            requests: { cpu: "100m", memory: "128Mi" },
          },
        },
        {
          name: "secondary",
          image: "alpine:3.21",
          command: ["sleep", "infinity"],
          env: { FROM: "secondary" },
          resources: {
            limits: { cpu: "100m", memory: "128Mi" },
            requests: { cpu: "100m", memory: "128Mi" },
          },
        },
      ],
    });
    created.push(sb.id);
    await waitForRunning(client, sb.id);

    const r1 = await sb.commands.run(["sh", "-c", "echo $FROM"], { container: "primary" });
    expect(r1.stdout).toBe("primary\n");
    const r2 = await sb.commands.run(["sh", "-c", "echo $FROM"], { container: "secondary" });
    expect(r2.stdout).toBe("secondary\n");
  }, 120_000);

  it("targeting a container with command runs in the right one (python only in server)", async () => {
    const sb = await client.sandboxes.create({
      containers: [
        {
          name: "server",
          image: "python:3.12-slim",
          command: ["python3", "-m", "http.server", "8080"],
        },
        { name: "client", image: "alpine:3.21" },
      ],
    });
    created.push(sb.id);
    await waitForRunning(client, sb.id);

    const serverR = await sb.commands.run(["python3", "--version"], { container: "server" });
    expect(serverR.exitCode).toBe(0);

    // Python is missing in alpine; expect a non-zero exit.
    const clientR = await sb.commands.run(["python3", "--version"], { container: "client" });
    expect(clientR.exitCode).not.toBe(0);
  }, 180_000);

  it("containers in the same sandbox share a network namespace (HTTP over 127.0.0.1)", async () => {
    const sb = await client.sandboxes.create({
      containers: [
        {
          name: "server",
          image: "python:3.12-slim",
          command: ["python3", "-m", "http.server", "8080"],
        },
        { name: "client", image: "alpine:3.21" },
      ],
    });
    created.push(sb.id);
    await waitForRunning(client, sb.id);
    // Give the HTTP server a moment to bind.
    await new Promise((r) => setTimeout(r, 2_000));

    const r = await sb.commands.run(["wget", "-qO-", "http://127.0.0.1:8080"], {
      container: "client",
      timeoutSeconds: 10,
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("Directory listing");
  }, 180_000);
});
