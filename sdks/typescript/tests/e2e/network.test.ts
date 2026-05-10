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

// Mirrors sdks/python/tests/e2e/test_network.py.
// Exercises Network spec end-to-end: default deny, internet egress,
// custom nameservers, allowedEgressCIDRs, DNS sink, cluster DNS.

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { Isola } from "../../src";
import { ISOLA_URL, safeDelete, waitForRunning } from "./_helpers";

describe.sequential("e2e: network", () => {
  let client: Isola;
  const created: string[] = [];

  beforeAll(() => {
    client = new Isola({ url: ISOLA_URL });
  });
  afterAll(async () => {
    for (const id of created) await safeDelete(client, id);
    await client.close();
  });

  it("default network: outbound connections fail (deny-all egress)", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const r = await running.commands.run(["wget", "-q", "-O-", "--timeout=3", "http://1.1.1.1"], {
      timeoutSeconds: 5,
    });
    expect(r.exitCode).not.toBe(0);
  }, 90_000);

  it("allowInternetEgress=true: sandbox can reach the internet", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      network: { allowInternetEgress: true, nameservers: ["8.8.8.8", "1.1.1.1"] },
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const r = await running.commands.run(["wget", "-q", "-O-", "--timeout=5", "http://1.1.1.1"], {
      timeoutSeconds: 10,
    });
    expect(r.exitCode).toBe(0);
    expect(r.stdout.length).toBeGreaterThan(0);
  }, 90_000);

  it("custom nameservers are written into /etc/resolv.conf", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      network: { allowInternetEgress: true, nameservers: ["8.8.8.8"] },
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const r = await running.commands.run(["cat", "/etc/resolv.conf"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("8.8.8.8");
  }, 90_000);

  it("allowedEgressCIDRs allowlist works (TCP to 1.1.1.1:53)", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      network: { allowedEgressCIDRs: ["1.1.1.1/32"], nameservers: ["1.1.1.1"] },
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    // Raw TCP avoids HTTP redirects to 1.0.0.1 (outside the CIDR).
    const r = await running.commands.run(["sh", "-c", "echo | nc -w 5 1.1.1.1 53"], {
      timeoutSeconds: 10,
    });
    expect(r.exitCode).toBe(0);
  }, 90_000);

  it("default network: DNS sink (127.0.0.1) appears in /etc/resolv.conf", async () => {
    const sb = await client.sandboxes.create({ image: "alpine:3.21" });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const r = await running.commands.run(["cat", "/etc/resolv.conf"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("127.0.0.1");
  }, 90_000);

  it("allowedEgressCIDRs with no DNS config auto-defaults nameservers", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      network: { allowedEgressCIDRs: ["1.1.1.1/32"] },
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const r = await running.commands.run(["cat", "/etc/resolv.conf"]);
    expect(r.exitCode).toBe(0);
    expect(r.stdout).toContain("8.8.8.8");
    expect(r.stdout).not.toContain("127.0.0.1");
  }, 90_000);

  it("network spec is reflected on get()", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      network: { allowInternetEgress: true, nameservers: ["8.8.8.8", "1.1.1.1"] },
    });
    created.push(sb.id);
    await waitForRunning(client, sb.id);

    const fetched = await client.sandboxes.get(sb.id);
    expect(fetched.network).toBeTruthy();
    expect(fetched.network?.allowInternetEgress).toBe(true);
    expect(fetched.network?.nameservers).toContain("8.8.8.8");
    expect(fetched.network?.nameservers).toContain("1.1.1.1");
  }, 90_000);

  it("allowClusterDNS=true: resolv.conf does not contain the sink", async () => {
    const sb = await client.sandboxes.create({
      image: "alpine:3.21",
      network: { allowClusterDNS: true },
    });
    created.push(sb.id);
    const running = await waitForRunning(client, sb.id);

    const r = await running.commands.run(["cat", "/etc/resolv.conf"]);
    expect(r.exitCode).toBe(0);
    // ClusterFirst DNS policy uses the kube-dns service IP, not the sink.
    expect(r.stdout).not.toContain("127.0.0.1");
  }, 90_000);
});
