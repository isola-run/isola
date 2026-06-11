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

import { describe, expect, it } from "vitest";
import {
  CommandResult,
  CommandStatusResponse,
  Container,
  ContainerInfo,
  CreateCommandResponse,
  CreateSandboxPayload,
  EgressRateLimit,
  ListSandboxesResponse,
  Network,
  PodTemplateInfo,
  ResourceList,
  ResourceRequirements,
  RootfsSnapshotData,
  SandboxData,
  SandboxSummary,
  SnapshotRootfs,
  TerminationPolicy,
} from "../src/models";

describe("Network acronym aliases", () => {
  it("preserves allowClusterDNS on outgoing JSON", () => {
    const dumped = Network.toWire({ allowClusterDNS: true });
    expect(dumped).toHaveProperty("allowClusterDNS", true);
    expect(dumped).not.toHaveProperty("allowClusterDns");
  });

  it("preserves allowedEgressCIDRs", () => {
    const dumped = Network.toWire({ allowedEgressCIDRs: ["10.0.0.0/8"] });
    expect(dumped).toHaveProperty("allowedEgressCIDRs", ["10.0.0.0/8"]);
    expect(dumped).not.toHaveProperty("allowedEgressCidrs");
  });

  it("uses standard camelCase for allowInternetEgress", () => {
    expect(Network.toWire({ allowInternetEgress: true })).toHaveProperty("allowInternetEgress", true);
  });

  it("nameservers has no alias change", () => {
    expect(Network.toWire({ nameservers: ["8.8.8.8"] })).toHaveProperty("nameservers", ["8.8.8.8"]);
  });

  it("allowIPv6Egress preserves PEP-0 acronym", () => {
    expect(Network.toWire({ allowIPv6Egress: true })).toHaveProperty("allowIPv6Egress", true);
  });

  it("decodes camelCase response into typed Network", () => {
    const net = Network.fromWire({
      allowClusterDNS: true,
      allowedEgressCIDRs: ["10.0.0.0/8"],
      allowIPv6Egress: false,
      nameservers: ["8.8.8.8"],
    });
    expect(net.allowClusterDNS).toBe(true);
    expect(net.allowedEgressCIDRs).toEqual(["10.0.0.0/8"]);
    expect(net.allowIPv6Egress).toBe(false);
    expect(net.nameservers).toEqual(["8.8.8.8"]);
  });

  it("excludes undefined fields on wire payload", () => {
    const dumped = Network.toWire({ allowClusterDNS: true });
    expect(Object.keys(dumped)).toEqual(["allowClusterDNS"]);
  });

  it("excludes null fields too (JS callers passing null instead of undefined)", () => {
    // TS forbids null here; JS callers can pass it. dropUndefined matches
    // Python's exclude_none=True semantics on both, so the wire stays clean.
    const dumped = Network.toWire({ allowClusterDNS: true, nameservers: null as never });
    expect(Object.keys(dumped)).toEqual(["allowClusterDNS"]);
  });

  it("decodes nested egressRateLimit", () => {
    const net = Network.fromWire({
      egressRateLimit: { rateBytesPerSecond: 10_000_000 },
    });
    expect(net.egressRateLimit).toEqual({ rateBytesPerSecond: 10_000_000 });
  });

  it("encodes nested egressRateLimit through EgressRateLimit.toWire", () => {
    // rateBytesPerSecond: null pins that the nested object is re-encoded
    // rather than passed through dropNullish at the top level only.
    const dumped = Network.toWire({
      egressRateLimit: { rateBytesPerSecond: null as never },
    });
    expect(dumped).toEqual({ egressRateLimit: {} });
  });
});

describe("EgressRateLimit", () => {
  it("fromWire decodes rateBytesPerSecond when present", () => {
    const rl = EgressRateLimit.fromWire({ rateBytesPerSecond: 10_000_000 });
    expect(rl).toEqual({ rateBytesPerSecond: 10_000_000 });
  });

  it("fromWire omits rateBytesPerSecond when absent", () => {
    const rl = EgressRateLimit.fromWire({});
    expect(rl).toEqual({});
  });

  it("toWire drops nullish rateBytesPerSecond", () => {
    // TS forbids null here, JS callers can pass it (same convention as the Network tests).
    const dumped = EgressRateLimit.toWire({ rateBytesPerSecond: null as never });
    expect(Object.keys(dumped)).toEqual([]);
  });
});

describe("SandboxData round-trip", () => {
  it("decodes camelCase response", () => {
    const camel = {
      id: "sb-42",
      status: "Running",
      creationTimestamp: "2026-03-15T12:30:00Z",
      podTemplate: {
        containers: [
          {
            name: "sandbox0",
            image: "python:3.12",
            command: ["sleep", "infinity"],
            rootfsSnapshotName: "snap-1",
            resources: {
              limits: { cpu: "1", memory: "2Gi", ephemeralStorage: "5Gi" },
              requests: { cpu: "500m", memory: "1Gi" },
            },
          },
        ],
      },
      timeoutSeconds: 3600,
      startupTimeoutSeconds: 60,
      network: {
        allowInternetEgress: true,
        allowClusterDNS: false,
        allowedEgressCIDRs: ["10.0.0.0/8"],
        nameservers: ["8.8.8.8"],
      },
    };

    const model = SandboxData.fromWire(camel);

    expect(model.id).toBe("sb-42");
    expect(model.status).toBe("Running");
    expect(model.timeoutSeconds).toBe(3600);
    expect(model.network?.allowInternetEgress).toBe(true);
    expect(model.network?.allowClusterDNS).toBe(false);
    expect(model.network?.allowedEgressCIDRs).toEqual(["10.0.0.0/8"]);
    expect(model.podTemplate.containers[0]?.rootfsSnapshotName).toBe("snap-1");
    expect(model.creationTimestamp).toBeInstanceOf(Date);
  });

  it("passes an unknown status through (forward-compatible with new server statuses)", () => {
    // The decoder trusts the gateway: a status the SDK predates is carried
    // through instead of throwing, so an additive server change does not break
    // already-deployed clients.
    const data = SandboxData.fromWire({
      id: "x",
      status: "Restoring",
      creationTimestamp: "2026-03-15T12:30:00Z",
      podTemplate: { containers: [{ name: "x", image: "y" }] },
      startupTimeoutSeconds: 60,
    });
    expect(data.status).toBe("Restoring");
  });

  it("ignores unknown fields", () => {
    const data = SandboxData.fromWire({
      id: "x",
      status: "Pending",
      creationTimestamp: "2026-03-15T12:30:00Z",
      podTemplate: { containers: [{ name: "n", image: "i" }] },
      startupTimeoutSeconds: 90,
      bogus: "ignored",
    });
    expect(data.id).toBe("x");
  });
});

describe("ListSandboxesResponse", () => {
  it("decodes the sandboxes array", () => {
    const list = ListSandboxesResponse.fromWire({
      sandboxes: [
        { id: "sb-1", status: "Running", creationTimestamp: "2026-01-01T00:00:00Z" },
        { id: "sb-2", status: "Succeeded", creationTimestamp: "2026-01-02T00:00:00Z" },
      ],
    });
    expect(list.sandboxes).toHaveLength(2);
    expect(list.sandboxes[0]?.id).toBe("sb-1");
  });
  it("treats absent sandboxes as []", () => {
    expect(ListSandboxesResponse.fromWire({}).sandboxes).toEqual([]);
    expect(ListSandboxesResponse.fromWire({ sandboxes: null }).sandboxes).toEqual([]);
  });
});

describe("CommandResult", () => {
  it("decodes id/stdout/stderr/exitCode", () => {
    const r = CommandResult.fromWire({ id: "cmd-1", stdout: "hi", stderr: "", exitCode: 0 });
    expect(r).toEqual({ id: "cmd-1", stdout: "hi", stderr: "", exitCode: 0 });
  });
});

describe("SandboxSummary.fromWire", () => {
  it("decodes a summary record", () => {
    const s = SandboxSummary.fromWire({
      id: "x",
      status: "Pending",
      creationTimestamp: "2026-01-01T00:00:00Z",
    });
    expect(s.id).toBe("x");
    expect(s.creationTimestamp.toISOString()).toBe("2026-01-01T00:00:00.000Z");
  });
});

describe("CreateSandboxPayload.toWire", () => {
  it("serialises podTemplate.containers and acronym network", () => {
    const wire = CreateSandboxPayload.toWire({
      podTemplate: { containers: [{ image: "node:20" }] },
      network: { allowClusterDNS: true, allowedEgressCIDRs: ["10.0.0.0/8"] },
      timeoutSeconds: 30,
      startupTimeoutSeconds: 60,
    });
    expect(wire).toMatchObject({
      podTemplate: { containers: [{ image: "node:20" }] },
      network: { allowClusterDNS: true, allowedEgressCIDRs: ["10.0.0.0/8"] },
      timeoutSeconds: 30,
      startupTimeoutSeconds: 60,
    });
  });

  it("wraps terminationPolicy via TerminationPolicy.toWire", () => {
    const wire = CreateSandboxPayload.toWire({
      podTemplate: { containers: [{ image: "alpine:3.21" }] },
      terminationPolicy: { snapshotName: "snap-x" },
    });
    expect(wire).toHaveProperty("terminationPolicy");
    const tp = (wire as { terminationPolicy: { type: string; snapshotRootfs: { snapshotName: string } } })
      .terminationPolicy;
    expect(tp.type).toBe("SnapshotRootfs");
    expect(tp.snapshotRootfs.snapshotName).toBe("snap-x");
  });

  it("emits empty snapshotRootfs object when SnapshotRootfs() has no fields", () => {
    const wire = CreateSandboxPayload.toWire({
      podTemplate: { containers: [{ image: "alpine:3.21" }] },
      terminationPolicy: {},
    });
    const tp = (wire as { terminationPolicy: { type: string; snapshotRootfs: Record<string, unknown> } })
      .terminationPolicy;
    expect(tp.type).toBe("SnapshotRootfs");
    expect(tp.snapshotRootfs).toEqual({});
  });

  it("excludes undefined network and timeouts on wire", () => {
    const wire = CreateSandboxPayload.toWire({
      podTemplate: { containers: [{ image: "alpine:3.21" }] },
    });
    expect(wire).not.toHaveProperty("network");
    expect(wire).not.toHaveProperty("timeoutSeconds");
    expect(wire).not.toHaveProperty("startupTimeoutSeconds");
    expect(wire).not.toHaveProperty("terminationPolicy");
  });
});

describe("TerminationPolicy.toWire and fromWire", () => {
  it("wraps SnapshotRootfs into the discriminated wire shape", () => {
    const wire = TerminationPolicy.toWire({ snapshotName: "x", timeoutSeconds: 10 });
    expect(wire).toEqual({ type: "SnapshotRootfs", snapshotRootfs: { snapshotName: "x", timeoutSeconds: 10 } });
  });

  it("decodes the SnapshotRootfs shape", () => {
    const tp = TerminationPolicy.fromWire({
      type: "SnapshotRootfs",
      snapshotRootfs: { snapshotName: "x" },
    });
    expect(tp.type).toBe("SnapshotRootfs");
    expect(tp.snapshotRootfs?.snapshotName).toBe("x");
  });

  it("decodes the Delete shape, the gateway's enum default", () => {
    // api-gateway.yaml:567-573, `Delete` is the default; any sandbox created
    // without an explicit terminationPolicy round-trips with this shape.
    const tp = TerminationPolicy.fromWire({ type: "Delete" });
    expect(tp.type).toBe("Delete");
    expect(tp.snapshotRootfs).toBeUndefined();
  });

  it("rejects unknown discriminator", () => {
    expect(() => TerminationPolicy.fromWire({ type: "Bogus" })).toThrow();
  });
});

describe("SnapshotRootfs.toWire", () => {
  it("returns {} for an empty input", () => {
    expect(SnapshotRootfs.toWire({})).toEqual({});
  });
  it("preserves provided fields", () => {
    expect(SnapshotRootfs.toWire({ snapshotName: "x", timeoutSeconds: 30 })).toEqual({
      snapshotName: "x",
      timeoutSeconds: 30,
    });
  });
});

// ---------- Decoder validation: rejects malformed inputs ----------

// Type-only TypeErrors from internal helpers (`optionalString`, `optionalNumber`,
// `record`, etc.), the exact wording is a diagnostic, not a contract, so we
// pin only the class. Tests that assert specific messages (e.g. field-name
// "X.foo is required") live below and pin the named-field contract.
describe("decoder validation: top-level type guard", () => {
  it("Network.fromWire rejects non-object", () => {
    expect(() => Network.fromWire(null)).toThrow(TypeError);
    expect(() => Network.fromWire("string")).toThrow(TypeError);
    expect(() => Network.fromWire(42)).toThrow(TypeError);
    expect(() => Network.fromWire([])).toThrow(TypeError);
  });

  it("ResourceList.fromWire rejects non-object", () => {
    expect(() => ResourceList.fromWire(null)).toThrow(TypeError);
  });

  it("Container.fromWire rejects non-object", () => {
    expect(() => Container.fromWire(null)).toThrow(TypeError);
  });
});

describe("Network.fromWire", () => {
  it("treats null fields as undefined (no error)", () => {
    const n = Network.fromWire({ allowInternetEgress: null, allowedEgressCIDRs: null });
    expect(n).toEqual({});
  });
});

describe("ResourceList.fromWire", () => {
  it("populates all three fields when provided", () => {
    const r = ResourceList.fromWire({ cpu: "500m", memory: "1Gi", ephemeralStorage: "5Gi" });
    expect(r).toEqual({ cpu: "500m", memory: "1Gi", ephemeralStorage: "5Gi" });
  });
});

describe("ResourceRequirements.fromWire", () => {
  it("decodes both limits and requests", () => {
    const r = ResourceRequirements.fromWire({
      limits: { cpu: "1" },
      requests: { cpu: "500m" },
    });
    expect(r.limits?.cpu).toBe("1");
    expect(r.requests?.cpu).toBe("500m");
  });

  it("treats absent fields as undefined", () => {
    const r = ResourceRequirements.fromWire({});
    expect(r).toEqual({});
  });

  it("toWire emits only provided sub-objects", () => {
    expect(ResourceRequirements.toWire({})).toEqual({});
    expect(ResourceRequirements.toWire({ limits: { cpu: "1" } })).toEqual({
      limits: { cpu: "1" },
    });
  });
});

describe("Container.toWire", () => {
  it("emits 'name' before 'image' (byte-identical with Python pydantic dump)", () => {
    // models.ts header declares wire-byte-identical with Python; Python's
    // pydantic field declaration order is name -> image, so toWire keys must
    // match. JSON.stringify preserves insertion order.
    const wire = Container.toWire({ name: "worker", image: "alpine:3.21" });
    expect(JSON.stringify(wire)).toBe('{"name":"worker","image":"alpine:3.21"}');
  });

  it("emits 'image' first when 'name' is absent", () => {
    // No `name` provided, image is the lone required key, so it shows first.
    const wire = Container.toWire({ image: "alpine:3.21" });
    expect(Object.keys(wire as Record<string, unknown>)).toEqual(["image"]);
  });
});

describe("Container.fromWire optional fields", () => {
  it("decodes name, rootfsSnapshotName, and command when all present", () => {
    const c = Container.fromWire({
      image: "alpine:3.21",
      name: "worker",
      rootfsSnapshotName: "snap-1",
      command: ["sh", "-c", "exit 0"],
    });
    expect(c.image).toBe("alpine:3.21");
    expect(c.name).toBe("worker");
    expect(c.rootfsSnapshotName).toBe("snap-1");
    expect(c.command).toEqual(["sh", "-c", "exit 0"]);
  });
});

describe("SnapshotRootfs.fromWire optional fields", () => {
  it("decodes timeoutSeconds alongside snapshotName", () => {
    const s = SnapshotRootfs.fromWire({ snapshotName: "x", timeoutSeconds: 60 });
    expect(s.snapshotName).toBe("x");
    expect(s.timeoutSeconds).toBe(60);
  });

  it("decodes timeoutSeconds without snapshotName", () => {
    const s = SnapshotRootfs.fromWire({ timeoutSeconds: 120 });
    expect(s.snapshotName).toBeUndefined();
    expect(s.timeoutSeconds).toBe(120);
  });
});

describe("TerminationPolicy.fromWire without snapshotRootfs", () => {
  it("returns { type: 'SnapshotRootfs' } when snapshotRootfs is absent", () => {
    const tp = TerminationPolicy.fromWire({ type: "SnapshotRootfs" });
    expect(tp.type).toBe("SnapshotRootfs");
    expect(tp.snapshotRootfs).toBeUndefined();
  });

  it("treats explicit null snapshotRootfs as absent", () => {
    const tp = TerminationPolicy.fromWire({ type: "SnapshotRootfs", snapshotRootfs: null });
    expect(tp.snapshotRootfs).toBeUndefined();
  });
});

describe("Container.fromWire validation", () => {
  it("requires image", () => {
    expect(() => Container.fromWire({})).toThrow(/Container.image is required/);
    expect(() => Container.fromWire({ image: 42 })).toThrow(/Container.image is required/);
  });

  it("decodes resources sub-object", () => {
    const c = Container.fromWire({
      image: "alpine",
      resources: { limits: { cpu: "1" } },
    });
    expect(c.resources?.limits?.cpu).toBe("1");
  });

  it("decodes env record with valid string values", () => {
    // optionalStringRecord happy path: loop body populates the output object.
    const c = Container.fromWire({
      image: "alpine",
      env: { FOO: "1", BAR: "two" },
    });
    expect(c.env).toEqual({ FOO: "1", BAR: "two" });
  });
});

describe("ContainerInfo.fromWire validation", () => {
  it("requires name", () => {
    expect(() => ContainerInfo.fromWire({ image: "x" })).toThrow(/ContainerInfo.name is required/);
  });

  it("requires image", () => {
    expect(() => ContainerInfo.fromWire({ name: "x" })).toThrow(/ContainerInfo.image is required/);
  });

  it("decodes optional fields", () => {
    const c = ContainerInfo.fromWire({
      name: "n",
      image: "i",
      rootfsSnapshotName: "snap",
      command: ["sleep", "infinity"],
      resources: { limits: { cpu: "500m" } },
    });
    expect(c.rootfsSnapshotName).toBe("snap");
    expect(c.command).toEqual(["sleep", "infinity"]);
    expect(c.resources?.limits?.cpu).toBe("500m");
  });
});

describe("PodTemplateInfo.fromWire validation", () => {
  it("requires containers to be an array", () => {
    expect(() => PodTemplateInfo.fromWire({})).toThrow(/PodTemplateInfo.containers is required/);
    expect(() => PodTemplateInfo.fromWire({ containers: "not-an-array" })).toThrow(
      /PodTemplateInfo.containers is required/,
    );
  });
});

describe("ListSandboxesResponse validation", () => {
  it("rejects non-array sandboxes when value is provided", () => {
    expect(() => ListSandboxesResponse.fromWire({ sandboxes: "not array" })).toThrow(/sandboxes must be an array/);
  });
});

describe("SandboxSummary.fromWire validation", () => {
  it("requires id", () => {
    expect(() =>
      SandboxSummary.fromWire({
        status: "Running",
        creationTimestamp: "2026-01-01T00:00:00Z",
      }),
    ).toThrow(/SandboxSummary.id is required/);
  });

  it("passes an unknown status through", () => {
    const s = SandboxSummary.fromWire({
      id: "x",
      status: "Restoring",
      creationTimestamp: "2026-01-01T00:00:00Z",
    });
    expect(s.status).toBe("Restoring");
  });

  it("rejects invalid timestamp", () => {
    expect(() =>
      SandboxSummary.fromWire({
        id: "x",
        status: "Pending",
        creationTimestamp: "not a timestamp",
      }),
    ).toThrow(/invalid timestamp/);
  });

  it("rejects non-string timestamp", () => {
    expect(() =>
      SandboxSummary.fromWire({
        id: "x",
        status: "Pending",
        creationTimestamp: 123456,
      }),
    ).toThrow(TypeError);
  });
});

describe("SandboxData.fromWire validation", () => {
  it("requires id", () => {
    expect(() =>
      SandboxData.fromWire({
        status: "Running",
        creationTimestamp: "2026-01-01T00:00:00Z",
        podTemplate: { containers: [{ name: "n", image: "i" }] },
        startupTimeoutSeconds: 60,
      }),
    ).toThrow(/SandboxData.id is required/);
  });

  it("requires startupTimeoutSeconds", () => {
    expect(() =>
      SandboxData.fromWire({
        id: "x",
        status: "Running",
        creationTimestamp: "2026-01-01T00:00:00Z",
        podTemplate: { containers: [{ name: "n", image: "i" }] },
      }),
    ).toThrow(/SandboxData.startupTimeoutSeconds is required/);
  });

  it("decodes optional terminationPolicy", () => {
    const data = SandboxData.fromWire({
      id: "x",
      status: "Running",
      creationTimestamp: "2026-01-01T00:00:00Z",
      podTemplate: { containers: [{ name: "n", image: "i" }] },
      startupTimeoutSeconds: 60,
      terminationPolicy: { type: "SnapshotRootfs", snapshotRootfs: { snapshotName: "snap" } },
    });
    expect(data.terminationPolicy?.type).toBe("SnapshotRootfs");
    expect(data.terminationPolicy?.snapshotRootfs?.snapshotName).toBe("snap");
  });
});

describe("RootfsSnapshotData.fromWire validation", () => {
  const valid = {
    id: "snap-1",
    sandboxId: "sandbox-1",
    snapshotName: "name",
    timeoutSeconds: 300,
    ttlSecondsAfterFinished: 600,
    status: "Succeeded",
    creationTimestamp: "2026-01-01T00:00:00Z",
  };

  it("decodes a valid response", () => {
    const data = RootfsSnapshotData.fromWire(valid);
    expect(data.id).toBe("snap-1");
    expect(data.containerName).toBeNull();
  });

  it("decodes containerName when provided", () => {
    const data = RootfsSnapshotData.fromWire({ ...valid, containerName: "worker" });
    expect(data.containerName).toBe("worker");
  });

  it("requires id", () => {
    const { id, ...rest } = valid;
    void id;
    expect(() => RootfsSnapshotData.fromWire(rest)).toThrow(/RootfsSnapshotData.id is required/);
  });

  it("requires sandboxId", () => {
    const { sandboxId, ...rest } = valid;
    void sandboxId;
    expect(() => RootfsSnapshotData.fromWire(rest)).toThrow(/RootfsSnapshotData.sandboxId is required/);
  });

  it("requires snapshotName", () => {
    const { snapshotName, ...rest } = valid;
    void snapshotName;
    expect(() => RootfsSnapshotData.fromWire(rest)).toThrow(/RootfsSnapshotData.snapshotName is required/);
  });

  it("requires timeoutSeconds", () => {
    const { timeoutSeconds, ...rest } = valid;
    void timeoutSeconds;
    expect(() => RootfsSnapshotData.fromWire(rest)).toThrow(/RootfsSnapshotData.timeoutSeconds is required/);
  });

  it("requires ttlSecondsAfterFinished", () => {
    const { ttlSecondsAfterFinished, ...rest } = valid;
    void ttlSecondsAfterFinished;
    expect(() => RootfsSnapshotData.fromWire(rest)).toThrow(/RootfsSnapshotData.ttlSecondsAfterFinished is required/);
  });

  it("passes an unknown status through", () => {
    const data = RootfsSnapshotData.fromWire({ ...valid, status: "Restoring" });
    expect(data.status).toBe("Restoring");
  });
});

describe("CommandResult.fromWire", () => {
  it("requires id", () => {
    expect(() => CommandResult.fromWire({ stdout: "", stderr: "", exitCode: 0 })).toThrow(
      /CommandResult.id is required/,
    );
  });

  it("throws when exitCode is a string", () => {
    // We tightened the decoder to reject non-finite-number exitCodes, matching
    // CommandStatusResponse.fromWire and Python's strict typing.
    expect(() => CommandResult.fromWire({ id: "x", stdout: "", stderr: "", exitCode: "42" })).toThrow(
      /exitCode must be a finite number/,
    );
  });

  it("throws when exitCode is missing", () => {
    expect(() => CommandResult.fromWire({ id: "x", stdout: "", stderr: "" })).toThrow(
      /exitCode must be a finite number/,
    );
  });

  it("defaults stdout/stderr to empty strings when missing", () => {
    const r = CommandResult.fromWire({ id: "x", exitCode: 0 });
    expect(r.stdout).toBe("");
    expect(r.stderr).toBe("");
  });
});

describe("CreateCommandResponse.fromWire", () => {
  it("requires id", () => {
    expect(() => CreateCommandResponse.fromWire({})).toThrow(/CreateCommandResponse.id is required/);
  });
});

// Pin symmetry: every model that owns BOTH fromWire and toWire must reach a
// fixed point after one round-trip. Catches asymmetric renames (e.g. wire
// `allowClusterDNS` decoded as `allowClusterDns` and re-emitted as such).
describe("round-trip: fromWire(toWire(x)) deep-equals x", () => {
  it("Network round-trip preserves all acronym aliases", () => {
    const x: Parameters<typeof Network.toWire>[0] = {
      allowInternetEgress: true,
      allowClusterDNS: false,
      allowIPv6Egress: true,
      allowedEgressCIDRs: ["10.0.0.0/8", "2001:db8::/32"],
      nameservers: ["1.1.1.1", "8.8.8.8"],
    };
    expect(Network.fromWire(Network.toWire(x))).toEqual(x);
  });

  it("Container round-trip preserves name/image/command/env/resources/rootfsSnapshotName", () => {
    const x: Parameters<typeof Container.toWire>[0] = {
      name: "worker",
      image: "alpine:3.21",
      command: ["sh", "-c", "exit 0"],
      env: { FOO: "1", BAR: "two" },
      rootfsSnapshotName: "snap-1",
      resources: {
        limits: { cpu: "500m", memory: "1Gi", ephemeralStorage: "2Gi" },
        requests: { cpu: "250m", memory: "512Mi" },
      },
    };
    expect(Container.fromWire(Container.toWire(x))).toEqual(x);
  });

  it("SnapshotRootfs round-trip preserves provided fields and omits absent ones", () => {
    const x: Parameters<typeof SnapshotRootfs.toWire>[0] = {
      snapshotName: "my-snap",
      timeoutSeconds: 120,
    };
    expect(SnapshotRootfs.fromWire(SnapshotRootfs.toWire(x))).toEqual(x);
  });

  it("TerminationPolicy round-trip pins the SnapshotRootfs wrap+unwrap", () => {
    const inner: Parameters<typeof TerminationPolicy.toWire>[0] = { snapshotName: "x" };
    const out = TerminationPolicy.fromWire(TerminationPolicy.toWire(inner));
    expect(out.type).toBe("SnapshotRootfs");
    expect(out.snapshotRootfs).toEqual({ snapshotName: "x" });
  });
});

describe("CommandStatusResponse.fromWire", () => {
  it("returns null exitCode when missing", () => {
    expect(CommandStatusResponse.fromWire({})).toEqual({ exitCode: null });
    expect(CommandStatusResponse.fromWire({ exitCode: null })).toEqual({ exitCode: null });
  });

  it("returns numeric exitCode when set", () => {
    expect(CommandStatusResponse.fromWire({ exitCode: 0 })).toEqual({ exitCode: 0 });
    expect(CommandStatusResponse.fromWire({ exitCode: 137 })).toEqual({ exitCode: 137 });
  });

  it("rejects non-number exitCode", () => {
    expect(() => CommandStatusResponse.fromWire({ exitCode: "0" })).toThrow(
      /CommandStatusResponse.exitCode must be a number or null/,
    );
  });

  it("rejects non-finite exitCode", () => {
    expect(() => CommandStatusResponse.fromWire({ exitCode: Number.POSITIVE_INFINITY })).toThrow(
      /CommandStatusResponse.exitCode must be a number or null/,
    );
  });
});
