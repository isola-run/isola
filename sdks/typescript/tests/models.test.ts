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

  it("rejects unknown SandboxStatus", () => {
    expect(() =>
      SandboxData.fromWire({
        id: "x",
        status: "Bogus",
        creationTimestamp: "2026-03-15T12:30:00Z",
        podTemplate: { containers: [{ name: "x", image: "y" }] },
        startupTimeoutSeconds: 60,
      }),
    ).toThrow();
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

  it("decodes the wire shape", () => {
    const tp = TerminationPolicy.fromWire({
      type: "SnapshotRootfs",
      snapshotRootfs: { snapshotName: "x" },
    });
    expect(tp.type).toBe("SnapshotRootfs");
    expect(tp.snapshotRootfs?.snapshotName).toBe("x");
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

describe("decoder validation: top-level type guard", () => {
  it("Network.fromWire rejects non-object", () => {
    expect(() => Network.fromWire(null)).toThrow(/expected object/);
    expect(() => Network.fromWire("string")).toThrow(/expected object/);
    expect(() => Network.fromWire(42)).toThrow(/expected object/);
    expect(() => Network.fromWire([])).toThrow(/expected object/);
  });

  it("ResourceList.fromWire rejects non-object", () => {
    expect(() => ResourceList.fromWire(null)).toThrow(/expected object/);
  });

  it("Container.fromWire rejects non-object", () => {
    expect(() => Container.fromWire(null)).toThrow(/expected object/);
  });
});

describe("Network.fromWire field type validation", () => {
  it("rejects non-boolean allowInternetEgress", () => {
    expect(() => Network.fromWire({ allowInternetEgress: "yes" })).toThrow(/expected boolean/);
  });

  it("rejects non-array allowedEgressCIDRs", () => {
    expect(() => Network.fromWire({ allowedEgressCIDRs: "10.0.0.0/8" })).toThrow(/expected array/);
  });

  it("rejects non-string entries in allowedEgressCIDRs", () => {
    expect(() => Network.fromWire({ allowedEgressCIDRs: [42] })).toThrow(/expected string/);
  });

  it("rejects non-array nameservers", () => {
    expect(() => Network.fromWire({ nameservers: { foo: "bar" } })).toThrow(/expected array/);
  });

  it("treats null fields as undefined (no error)", () => {
    const n = Network.fromWire({ allowInternetEgress: null, allowedEgressCIDRs: null });
    expect(n).toEqual({});
  });
});

describe("ResourceList.fromWire field type validation", () => {
  it("rejects non-string cpu", () => {
    expect(() => ResourceList.fromWire({ cpu: 500 })).toThrow(/expected string/);
  });

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

describe("Container.fromWire validation", () => {
  it("requires image", () => {
    expect(() => Container.fromWire({})).toThrow(/Container.image is required/);
    expect(() => Container.fromWire({ image: 42 })).toThrow(/Container.image is required/);
  });

  it("rejects non-string env values", () => {
    expect(() =>
      Container.fromWire({
        image: "x",
        env: { FOO: 123 },
      }),
    ).toThrow(/expected string value/);
  });

  it("decodes resources sub-object", () => {
    const c = Container.fromWire({
      image: "alpine",
      resources: { limits: { cpu: "1" } },
    });
    expect(c.resources?.limits?.cpu).toBe("1");
  });

  it("decodes env record with valid string values", () => {
    // Covers optionalStringRecord's happy path: the loop body that
    // populates the output object (models.ts:71).
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

  it("rejects invalid status", () => {
    expect(() =>
      SandboxSummary.fromWire({
        id: "x",
        status: "Bogus",
        creationTimestamp: "2026-01-01T00:00:00Z",
      }),
    ).toThrow(/invalid SandboxStatus/);
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
    ).toThrow(/expected timestamp string/);
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

  it("rejects non-finite startupTimeoutSeconds", () => {
    expect(() =>
      SandboxData.fromWire({
        id: "x",
        status: "Running",
        creationTimestamp: "2026-01-01T00:00:00Z",
        podTemplate: { containers: [{ name: "n", image: "i" }] },
        startupTimeoutSeconds: Number.POSITIVE_INFINITY,
      }),
    ).toThrow(/SandboxData.startupTimeoutSeconds is required/);
  });

  it("rejects non-finite optional timeoutSeconds", () => {
    expect(() =>
      SandboxData.fromWire({
        id: "x",
        status: "Running",
        creationTimestamp: "2026-01-01T00:00:00Z",
        podTemplate: { containers: [{ name: "n", image: "i" }] },
        startupTimeoutSeconds: 60,
        timeoutSeconds: Number.NaN,
      }),
    ).toThrow(/expected number/);
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

  it("rejects non-finite timeoutSeconds", () => {
    expect(() => RootfsSnapshotData.fromWire({ ...valid, timeoutSeconds: Number.POSITIVE_INFINITY })).toThrow(
      /RootfsSnapshotData.timeoutSeconds is required/,
    );
  });

  it("requires ttlSecondsAfterFinished", () => {
    const { ttlSecondsAfterFinished, ...rest } = valid;
    void ttlSecondsAfterFinished;
    expect(() => RootfsSnapshotData.fromWire(rest)).toThrow(/RootfsSnapshotData.ttlSecondsAfterFinished is required/);
  });

  it("rejects non-finite ttlSecondsAfterFinished", () => {
    expect(() => RootfsSnapshotData.fromWire({ ...valid, ttlSecondsAfterFinished: Number.NaN })).toThrow(
      /RootfsSnapshotData.ttlSecondsAfterFinished is required/,
    );
  });

  it("rejects unknown status", () => {
    expect(() => RootfsSnapshotData.fromWire({ ...valid, status: "Bogus" })).toThrow(/invalid RootfsSnapshotStatus/);
  });
});

describe("CommandResult.fromWire", () => {
  it("requires id", () => {
    expect(() => CommandResult.fromWire({ stdout: "", stderr: "", exitCode: 0 })).toThrow(
      /CommandResult.id is required/,
    );
  });

  it("coerces non-number exitCode via Number()", () => {
    const r = CommandResult.fromWire({ id: "x", stdout: "", stderr: "", exitCode: "42" });
    expect(r.exitCode).toBe(42);
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
