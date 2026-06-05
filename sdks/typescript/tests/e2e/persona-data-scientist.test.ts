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

// Persona: data scientist.
// Pattern: one long-lived sandbox, upload a CSV, run analysis in Python, read
// the results back. The "with pandas" variant requires internet egress for
// pip install; we probe egress first and skip cleanly if it's not available.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola, type Sandbox } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

async function clusterHasEgress(client: Isola): Promise<boolean> {
  try {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      network: { allowInternetEgress: true, nameservers: ["8.8.8.8", "1.1.1.1"] },
      timeoutSeconds: 60,
    });
    try {
      const running = await waitForRunning(client, sb.id);
      // wget against a well-known stable IP; -T 5 = 5s timeout.
      const r = await running.commands.run(["wget", "-q", "-O-", "--timeout=5", "-T", "5", "http://1.1.1.1"], {
        timeoutSeconds: 10,
      });
      return r.exitCode === 0;
    } finally {
      await safeDelete(client, sb.id);
    }
  } catch {
    return false;
  }
}

describe.sequential("e2e: persona, data scientist (long-lived sandbox, CSV pipeline)", () => {
  let client: Isola;
  let egress = false;
  const created: string[] = [];

  beforeAll(async () => {
    client = new Isola({ url: ISOLA_URL });
    egress = await clusterHasEgress(client);
  }, 180_000);

  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
  });

  // Pure-stdlib analysis: no pip needed, works on any cluster.
  it("stdlib CSV analysis: upload, run, read results", async () => {
    const sb: Sandbox = await client.sandboxes.create({ image: "python:3.12-alpine" });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    // Upload the dataset.
    const csv = `${["name,score", "alice,90", "bob,80", "carol,70"].join("\n")}\n`;
    await running.filesystem.write("/data/scores.csv", csv);

    // Upload a stdlib-only analysis script.
    const script = [
      "import csv, json",
      "with open('/data/scores.csv') as f:",
      "    reader = csv.DictReader(f)",
      "    rows = list(reader)",
      "scores = [int(r['score']) for r in rows]",
      "avg = sum(scores) / len(scores)",
      "result = {'count': len(scores), 'avg': avg, 'max': max(scores)}",
      "with open('/data/result.json', 'w') as f:",
      "    json.dump(result, f)",
      "print(json.dumps(result))",
    ].join("\n");
    await running.filesystem.write("/data/analyze.py", script);

    const r = await running.commands.run(["python3", "/data/analyze.py"]);
    expect(r.exitCode).toBe(0);

    const result: { count: number; avg: number; max: number } = JSON.parse(r.stdout);
    expect(result.count).toBe(3);
    expect(result.avg).toBe(80);
    expect(result.max).toBe(90);

    // Read the result file back via filesystem API (round-trip).
    const fileBytes = await running.filesystem.read("/data/result.json");
    const parsed: { count: number; avg: number; max: number } = JSON.parse(new TextDecoder().decode(fileBytes));
    expect(parsed.avg).toBe(80);
  }, 180_000);

  // pandas variant: needs egress for pip install. Skip cleanly if the
  // cluster doesn't allow it. Even when egress is available, pip on Kind
  // can be slow, we give it a generous timeout.
  it("pandas analysis (pip install): conditional on egress", async (ctx) => {
    if (!egress) {
      ctx.skip();
      return;
    }
    const sb: Sandbox = await client.sandboxes.create({
      image: "python:3.12-slim",
      network: { allowInternetEgress: true, nameservers: ["8.8.8.8", "1.1.1.1"] },
      timeoutSeconds: 300,
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const csv = `${["x,y", "1,2", "3,4", "5,6"].join("\n")}\n`;
    await running.filesystem.write("/data/xy.csv", csv);

    // Install pandas. May take a while on Kind, pip + Pypi over the
    // single-node cluster's NAT.
    const install = await running.commands.run(["pip", "install", "--quiet", "--disable-pip-version-check", "pandas"], {
      timeoutSeconds: 240,
    });
    if (install.exitCode !== 0) {
      // pip can flake on test infra; surface a clear message but don't
      // hard-fail the suite, this is the "soft" requirement variant.
      console.warn(
        `[persona-data-scientist] pip install pandas failed (exit ${install.exitCode}): ${install.stderr.slice(0, 500)}`,
      );
      ctx.skip();
      return;
    }

    const r = await running.commands.run([
      "python3",
      "-c",
      "import pandas as pd; df = pd.read_csv('/data/xy.csv'); print(df['y'].sum())",
    ]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout.trim()).toBe("12");
  }, 360_000);
});
