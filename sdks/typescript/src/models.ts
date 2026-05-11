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

// Mirrors sdks/python/src/isola/_models.py 1:1.
//
// Wire-format invariants:
// - camelCase keys, dropped undefined/null fields on outgoing JSON
//   (mirrors Python `model_dump(by_alias=True, exclude_none=True)`).
// - Acronym overrides (allowedEgressCIDRs, allowClusterDNS, allowIPv6Egress)
//   preserved as-is.
// - Unknown fields on responses ignored (mirrors Python `extra="ignore"`).
// - terminationPolicy: SnapshotRootfs argument is wrapped on the wire as
//   { type: "SnapshotRootfs", snapshotRootfs: <input> } by
//   TerminationPolicy.toWire (called from CreateSandboxPayload.toWire).

// ---------- Decoder helpers (private) ----------

type Wire = Record<string, unknown>;

function record(v: unknown): Wire {
  if (v === null || typeof v !== "object" || Array.isArray(v)) {
    throw new TypeError("expected object");
  }
  return v as Wire;
}

function optionalString(v: unknown): string | undefined {
  if (v == null) return undefined;
  if (typeof v !== "string") throw new TypeError("expected string");
  return v;
}

function optionalNumber(v: unknown): number | undefined {
  if (v == null) return undefined;
  if (typeof v !== "number" || !Number.isFinite(v)) throw new TypeError("expected number");
  return v;
}

function optionalBoolean(v: unknown): boolean | undefined {
  if (v == null) return undefined;
  if (typeof v !== "boolean") throw new TypeError("expected boolean");
  return v;
}

function optionalStringArray(v: unknown): string[] | undefined {
  if (v == null) return undefined;
  if (!Array.isArray(v)) throw new TypeError("expected array");
  return v.map((x) => {
    if (typeof x !== "string") throw new TypeError("expected string");
    return x;
  });
}

function optionalStringRecord(v: unknown): Record<string, string> | undefined {
  if (v == null) return undefined;
  const obj = record(v);
  const out: Record<string, string> = {};
  for (const [k, value] of Object.entries(obj)) {
    if (typeof value !== "string") throw new TypeError("expected string value");
    out[k] = value;
  }
  return out;
}

function requiredDate(v: unknown): Date {
  if (typeof v !== "string") throw new TypeError("expected timestamp string");
  const d = new Date(v);
  if (Number.isNaN(d.valueOf())) throw new TypeError("invalid timestamp");
  return d;
}

// Drops `undefined` properties so outgoing JSON matches Python's
// `model_dump(by_alias=True, exclude_none=True)`. Empty submodels are
// preserved to match SnapshotRootfs() round-tripping as `{}`.
function dropUndefined<T extends Record<string, unknown>>(obj: T): Wire {
  const out: Wire = {};
  for (const [k, v] of Object.entries(obj)) {
    if (v !== undefined) out[k] = v;
  }
  return out;
}

// ---------- Public types ----------

/** Lifecycle status of a sandbox. */
export type SandboxStatus = "Pending" | "Running" | "Terminating" | "Succeeded" | "Failed";

/** Lifecycle status of a rootfs snapshot. */
export type RootfsSnapshotStatus = "Pending" | "Running" | "Succeeded" | "Failed";

const SANDBOX_STATUSES: ReadonlySet<string> = new Set(["Pending", "Running", "Terminating", "Succeeded", "Failed"]);

const ROOTFS_SNAPSHOT_STATUSES: ReadonlySet<string> = new Set(["Pending", "Running", "Succeeded", "Failed"]);

function sandboxStatus(v: unknown): SandboxStatus {
  if (typeof v !== "string" || !SANDBOX_STATUSES.has(v)) {
    throw new TypeError(`invalid SandboxStatus: ${String(v)}`);
  }
  return v as SandboxStatus;
}

function rootfsSnapshotStatus(v: unknown): RootfsSnapshotStatus {
  if (typeof v !== "string" || !ROOTFS_SNAPSHOT_STATUSES.has(v)) {
    throw new TypeError(`invalid RootfsSnapshotStatus: ${String(v)}`);
  }
  return v as RootfsSnapshotStatus;
}

/**
 * Network configuration for a sandbox.
 *
 * Sandboxes have no network access by default. Use this to enable
 * internet access, cluster DNS, or fine-grained egress rules.
 *
 * When internet egress or custom CIDRs are enabled without cluster DNS,
 * the server automatically configures public nameservers (8.8.8.8, 1.1.1.1)
 * so DNS resolution works out of the box. Override this with the
 * `nameservers` field.
 */
export interface Network {
  /** Allow outbound traffic to the public internet. */
  allowInternetEgress?: boolean;
  /**
   * List of CIDR blocks the sandbox can reach (e.g. `["10.0.0.0/8"]`).
   * Use this for fine-grained control instead of allowing all internet
   * traffic.
   */
  allowedEgressCIDRs?: string[];
  /**
   * Allow DNS resolution through the cluster's DNS service. When
   * `false` and `allowInternetEgress` or `allowedEgressCIDRs` are
   * specified, the sandbox uses public nameservers or the ones you
   * provide in `nameservers`.
   */
  allowClusterDNS?: boolean;
  /**
   * Custom DNS nameservers. Overrides the automatic public
   * nameservers.
   */
  nameservers?: string[];
  /**
   * Enable IPv6 across egress configuration. Extends
   * `allowInternetEgress` to cover IPv6, and allows IPv6 addresses in
   * `allowedEgressCIDRs` and `nameservers`.
   */
  allowIPv6Egress?: boolean;
}

export const Network = {
  fromWire(json: unknown): Network {
    const o = record(json);
    const out: Network = {};
    const allowInternetEgress = optionalBoolean(o.allowInternetEgress);
    if (allowInternetEgress !== undefined) out.allowInternetEgress = allowInternetEgress;
    const allowedEgressCIDRs = optionalStringArray(o.allowedEgressCIDRs);
    if (allowedEgressCIDRs !== undefined) out.allowedEgressCIDRs = allowedEgressCIDRs;
    const allowClusterDNS = optionalBoolean(o.allowClusterDNS);
    if (allowClusterDNS !== undefined) out.allowClusterDNS = allowClusterDNS;
    const nameservers = optionalStringArray(o.nameservers);
    if (nameservers !== undefined) out.nameservers = nameservers;
    const allowIPv6Egress = optionalBoolean(o.allowIPv6Egress);
    if (allowIPv6Egress !== undefined) out.allowIPv6Egress = allowIPv6Egress;
    return out;
  },
  toWire(n: Network): Wire {
    return dropUndefined(n as Record<string, unknown>);
  },
};

/**
 * Kubernetes resource quantities for a single direction (limits or
 * requests). Strings use Kubernetes quantity syntax (e.g. `"500m"` for
 * CPU, `"256Mi"` for memory).
 */
export interface ResourceList {
  cpu?: string;
  memory?: string;
  ephemeralStorage?: string;
}

export const ResourceList = {
  fromWire(json: unknown): ResourceList {
    const o = record(json);
    const out: ResourceList = {};
    const cpu = optionalString(o.cpu);
    if (cpu !== undefined) out.cpu = cpu;
    const memory = optionalString(o.memory);
    if (memory !== undefined) out.memory = memory;
    const ephemeralStorage = optionalString(o.ephemeralStorage);
    if (ephemeralStorage !== undefined) out.ephemeralStorage = ephemeralStorage;
    return out;
  },
  toWire(r: ResourceList): Wire {
    return dropUndefined(r as Record<string, unknown>);
  },
};

/** CPU, memory, and ephemeral storage limits and requests for a container. */
export interface ResourceRequirements {
  limits?: ResourceList;
  requests?: ResourceList;
}

export const ResourceRequirements = {
  fromWire(json: unknown): ResourceRequirements {
    const o = record(json);
    const out: ResourceRequirements = {};
    if (o.limits != null) out.limits = ResourceList.fromWire(o.limits);
    if (o.requests != null) out.requests = ResourceList.fromWire(o.requests);
    return out;
  },
  toWire(r: ResourceRequirements): Wire {
    const out: Wire = {};
    if (r.limits !== undefined) out.limits = ResourceList.toWire(r.limits);
    if (r.requests !== undefined) out.requests = ResourceList.toWire(r.requests);
    return out;
  },
};

/**
 * Container specification for sandbox creation.
 *
 * Used with the `containers` parameter of {@link Sandboxes.create} when
 * running multi-container sandboxes or when specifying custom and
 * non-equal requests and limits resource requirements.
 */
export interface Container {
  /** Container name. Auto-generated if not set. */
  name?: string;
  /** Container image to run (e.g. `"python:3.12"`). */
  image: string;
  /** Name of a rootfs snapshot to restore into this container. */
  rootfsSnapshotName?: string;
  /**
   * Command and arguments to run in the container. If not set,
   * defaults to `sleep infinity`.
   */
  command?: string[];
  /** Environment variables as key-value pairs. */
  env?: Record<string, string>;
  /**
   * CPU, memory, and ephemeral storage k8s resource requirements.
   *
   * In multi-container sandboxes, set CPU and memory limits on every
   * container. gVisor runs a single sentry process inside the pod
   * cgroup, and Kubernetes sums container limits into the pod cgroup
   * only when every container declares one, so a missing CPU or memory
   * limit leaves the whole pod unbounded on that dimension. Ephemeral
   * storage is the opposite case: kubelet caps the pod at the sum of
   * declared container limits, treating a missing limit as 0 rather
   * than infinity. If only one container in a two-container sandbox
   * declares a 256Mi limit, the whole pod is capped at 256Mi.
   */
  resources?: ResourceRequirements;
}

export const Container = {
  fromWire(json: unknown): Container {
    const o = record(json);
    if (typeof o.image !== "string") throw new TypeError("Container.image is required");
    const out: Container = { image: o.image };
    const name = optionalString(o.name);
    if (name !== undefined) out.name = name;
    const rootfsSnapshotName = optionalString(o.rootfsSnapshotName);
    if (rootfsSnapshotName !== undefined) out.rootfsSnapshotName = rootfsSnapshotName;
    const command = optionalStringArray(o.command);
    if (command !== undefined) out.command = command;
    const env = optionalStringRecord(o.env);
    if (env !== undefined) out.env = env;
    if (o.resources != null) out.resources = ResourceRequirements.fromWire(o.resources);
    return out;
  },
  toWire(c: Container): Wire {
    const out: Wire = { image: c.image };
    if (c.name !== undefined) out.name = c.name;
    if (c.rootfsSnapshotName !== undefined) out.rootfsSnapshotName = c.rootfsSnapshotName;
    if (c.command !== undefined) out.command = c.command;
    if (c.env !== undefined) out.env = c.env;
    if (c.resources !== undefined) out.resources = ResourceRequirements.toWire(c.resources);
    return out;
  },
};

/**
 * Read-only container information returned by the API.
 *
 * `env` is intentionally omitted on response types to avoid leaking
 * secrets. It is write-only on {@link Container}.
 */
export interface ContainerInfo {
  /** Container name. */
  name: string;
  /** Container image. */
  image: string;
  /** Rootfs snapshot name, if restoring from one. */
  rootfsSnapshotName?: string;
  /** Command and arguments. */
  command?: string[];
  /** Resource limits and requests. */
  resources?: ResourceRequirements;
}

export const ContainerInfo = {
  fromWire(json: unknown): ContainerInfo {
    const o = record(json);
    if (typeof o.name !== "string") throw new TypeError("ContainerInfo.name is required");
    if (typeof o.image !== "string") throw new TypeError("ContainerInfo.image is required");
    const out: ContainerInfo = { name: o.name, image: o.image };
    const rootfsSnapshotName = optionalString(o.rootfsSnapshotName);
    if (rootfsSnapshotName !== undefined) out.rootfsSnapshotName = rootfsSnapshotName;
    const command = optionalStringArray(o.command);
    if (command !== undefined) out.command = command;
    if (o.resources != null) out.resources = ResourceRequirements.fromWire(o.resources);
    return out;
  },
};

/**
 * Termination policy that snapshots a container's root filesystem
 * changes on exit.
 *
 * Pass this as the `terminationPolicy` parameter of
 * {@link Sandboxes.create} to automatically capture a rootfs snapshot
 * of the container's root filesystem changes when the sandbox
 * terminates. Restore the snapshot later by passing its name as
 * `rootfsSnapshotName`.
 */
export interface SnapshotRootfs {
  /**
   * Name for the snapshot. If not set, defaults to the sandbox's ID
   * on the server.
   */
  snapshotName?: string;
  /**
   * Maximum time for the snapshot operation, in seconds. Enforced
   * server-side. The server cancels the snapshot if it takes longer
   * than this. The server defaults to 300 seconds if not set.
   */
  timeoutSeconds?: number;
}

export const SnapshotRootfs = {
  fromWire(json: unknown): SnapshotRootfs {
    const o = record(json);
    const out: SnapshotRootfs = {};
    const snapshotName = optionalString(o.snapshotName);
    if (snapshotName !== undefined) out.snapshotName = snapshotName;
    const timeoutSeconds = optionalNumber(o.timeoutSeconds);
    if (timeoutSeconds !== undefined) out.timeoutSeconds = timeoutSeconds;
    return out;
  },
  toWire(s: SnapshotRootfs): Wire {
    return dropUndefined(s as Record<string, unknown>);
  },
};

/** Lightweight sandbox summary returned by list operations. */
export interface SandboxSummary {
  /** Unique identifier of the sandbox. */
  id: string;
  /** Current lifecycle status. */
  status: SandboxStatus;
  /** When the sandbox was created. */
  creationTimestamp: Date;
}

export const SandboxSummary = {
  fromWire(json: unknown): SandboxSummary {
    const o = record(json);
    if (typeof o.id !== "string") throw new TypeError("SandboxSummary.id is required");
    return {
      id: o.id,
      status: sandboxStatus(o.status),
      creationTimestamp: requiredDate(o.creationTimestamp),
    };
  },
};

/** Result of a completed command execution. */
export interface CommandResult {
  /** Unique identifier of the command. */
  readonly id: string;
  /** Complete standard output as a string. */
  readonly stdout: string;
  /** Complete standard error as a string. */
  readonly stderr: string;
  /** Process exit code. 0 indicates success. */
  readonly exitCode: number;
}

export const CommandResult = {
  fromWire(json: unknown): CommandResult {
    const o = record(json);
    if (typeof o.id !== "string") throw new TypeError("CommandResult.id is required");
    if (typeof o.exitCode !== "number" || !Number.isFinite(o.exitCode)) {
      throw new TypeError("CommandResult.exitCode must be a finite number");
    }
    return {
      id: o.id,
      stdout: typeof o.stdout === "string" ? o.stdout : "",
      stderr: typeof o.stderr === "string" ? o.stderr : "",
      exitCode: o.exitCode,
    };
  },
};

// ---------- Internal payload/data types ----------

export interface PodTemplate {
  containers: Container[];
}

export const PodTemplate = {
  toWire(t: PodTemplate): Wire {
    return { containers: t.containers.map((c) => Container.toWire(c)) };
  },
};

export interface PodTemplateInfo {
  containers: ContainerInfo[];
}

export const PodTemplateInfo = {
  fromWire(json: unknown): PodTemplateInfo {
    const o = record(json);
    if (!Array.isArray(o.containers)) throw new TypeError("PodTemplateInfo.containers is required");
    return { containers: o.containers.map((c) => ContainerInfo.fromWire(c)) };
  },
};

export interface TerminationPolicy {
  type: "SnapshotRootfs";
  snapshotRootfs?: SnapshotRootfs;
}

export const TerminationPolicy = {
  fromWire(json: unknown): TerminationPolicy {
    const o = record(json);
    if (o.type !== "SnapshotRootfs") {
      throw new TypeError(`invalid TerminationPolicy.type: ${String(o.type)}`);
    }
    const out: TerminationPolicy = { type: "SnapshotRootfs" };
    if (o.snapshotRootfs != null) out.snapshotRootfs = SnapshotRootfs.fromWire(o.snapshotRootfs);
    return out;
  },
  // Wraps a user-supplied SnapshotRootfs into the discriminated wire shape.
  // CreateSandboxPayload.toWire delegates here so the wrapping rule lives in
  // exactly one place. Mirrors Python _sandbox.py:303-309.
  toWire(input: SnapshotRootfs): Wire {
    return { type: "SnapshotRootfs", snapshotRootfs: SnapshotRootfs.toWire(input) };
  },
};

export interface CreateSandboxPayload {
  podTemplate: PodTemplate;
  timeoutSeconds?: number;
  startupTimeoutSeconds?: number;
  network?: Network;
  terminationPolicy?: SnapshotRootfs;
}

export const CreateSandboxPayload = {
  toWire(p: CreateSandboxPayload): Wire {
    const out: Wire = { podTemplate: PodTemplate.toWire(p.podTemplate) };
    if (p.timeoutSeconds !== undefined) out.timeoutSeconds = p.timeoutSeconds;
    if (p.startupTimeoutSeconds !== undefined) out.startupTimeoutSeconds = p.startupTimeoutSeconds;
    if (p.network !== undefined) out.network = Network.toWire(p.network);
    if (p.terminationPolicy !== undefined) out.terminationPolicy = TerminationPolicy.toWire(p.terminationPolicy);
    return out;
  },
};

export interface ListSandboxesResponse {
  sandboxes: SandboxSummary[];
}

export const ListSandboxesResponse = {
  fromWire(json: unknown): ListSandboxesResponse {
    const o = record(json);
    if (o.sandboxes == null) return { sandboxes: [] };
    if (!Array.isArray(o.sandboxes)) throw new TypeError("sandboxes must be an array");
    return { sandboxes: o.sandboxes.map((s) => SandboxSummary.fromWire(s)) };
  },
};

export interface SandboxData {
  id: string;
  podTemplate: PodTemplateInfo;
  status: SandboxStatus;
  creationTimestamp: Date;
  timeoutSeconds?: number;
  startupTimeoutSeconds: number;
  network?: Network;
  terminationPolicy?: TerminationPolicy;
}

export const SandboxData = {
  fromWire(json: unknown): SandboxData {
    const o = record(json);
    if (typeof o.id !== "string") throw new TypeError("SandboxData.id is required");
    if (typeof o.startupTimeoutSeconds !== "number" || !Number.isFinite(o.startupTimeoutSeconds)) {
      throw new TypeError("SandboxData.startupTimeoutSeconds is required");
    }
    const out: SandboxData = {
      id: o.id,
      podTemplate: PodTemplateInfo.fromWire(o.podTemplate),
      status: sandboxStatus(o.status),
      creationTimestamp: requiredDate(o.creationTimestamp),
      startupTimeoutSeconds: o.startupTimeoutSeconds,
    };
    const timeoutSeconds = optionalNumber(o.timeoutSeconds);
    if (timeoutSeconds !== undefined) out.timeoutSeconds = timeoutSeconds;
    if (o.network != null) out.network = Network.fromWire(o.network);
    if (o.terminationPolicy != null) out.terminationPolicy = TerminationPolicy.fromWire(o.terminationPolicy);
    return out;
  },
};

export interface CreateRootfsSnapshotPayload {
  sandboxId: string;
  snapshotName?: string;
  containerName?: string;
  timeoutSeconds?: number;
  ttlSecondsAfterFinished?: number;
}

export const CreateRootfsSnapshotPayload = {
  toWire(p: CreateRootfsSnapshotPayload): Wire {
    const out: Wire = { sandboxId: p.sandboxId };
    if (p.snapshotName !== undefined) out.snapshotName = p.snapshotName;
    if (p.containerName !== undefined) out.containerName = p.containerName;
    if (p.timeoutSeconds !== undefined) out.timeoutSeconds = p.timeoutSeconds;
    if (p.ttlSecondsAfterFinished !== undefined) out.ttlSecondsAfterFinished = p.ttlSecondsAfterFinished;
    return out;
  },
};

export interface RootfsSnapshotData {
  id: string;
  sandboxId: string;
  snapshotName: string;
  containerName: string | null;
  timeoutSeconds: number;
  ttlSecondsAfterFinished: number;
  status: RootfsSnapshotStatus;
  creationTimestamp: Date;
}

export const RootfsSnapshotData = {
  fromWire(json: unknown): RootfsSnapshotData {
    const o = record(json);
    if (typeof o.id !== "string") throw new TypeError("RootfsSnapshotData.id is required");
    if (typeof o.sandboxId !== "string") throw new TypeError("RootfsSnapshotData.sandboxId is required");
    if (typeof o.snapshotName !== "string") throw new TypeError("RootfsSnapshotData.snapshotName is required");
    if (typeof o.timeoutSeconds !== "number" || !Number.isFinite(o.timeoutSeconds)) {
      throw new TypeError("RootfsSnapshotData.timeoutSeconds is required");
    }
    if (typeof o.ttlSecondsAfterFinished !== "number" || !Number.isFinite(o.ttlSecondsAfterFinished)) {
      throw new TypeError("RootfsSnapshotData.ttlSecondsAfterFinished is required");
    }
    return {
      id: o.id,
      sandboxId: o.sandboxId,
      snapshotName: o.snapshotName,
      containerName: optionalString(o.containerName) ?? null,
      timeoutSeconds: o.timeoutSeconds,
      ttlSecondsAfterFinished: o.ttlSecondsAfterFinished,
      status: rootfsSnapshotStatus(o.status),
      creationTimestamp: requiredDate(o.creationTimestamp),
    };
  },
};

export interface CreateCommandPayload {
  args: string[];
  env?: Record<string, string>;
  cwd?: string;
  timeoutSeconds?: number;
}

export const CreateCommandPayload = {
  toWire(p: CreateCommandPayload): Wire {
    const out: Wire = { args: p.args };
    if (p.env !== undefined) out.env = p.env;
    if (p.cwd !== undefined) out.cwd = p.cwd;
    if (p.timeoutSeconds !== undefined) out.timeoutSeconds = p.timeoutSeconds;
    return out;
  },
};

export interface CreateCommandResponse {
  id: string;
}

export const CreateCommandResponse = {
  fromWire(json: unknown): CreateCommandResponse {
    const o = record(json);
    if (typeof o.id !== "string") throw new TypeError("CreateCommandResponse.id is required");
    return { id: o.id };
  },
};

export interface CommandStatusResponse {
  exitCode: number | null;
}

export const CommandStatusResponse = {
  fromWire(json: unknown): CommandStatusResponse {
    const o = record(json);
    if (o.exitCode == null) return { exitCode: null };
    if (typeof o.exitCode !== "number" || !Number.isFinite(o.exitCode)) {
      throw new TypeError("CommandStatusResponse.exitCode must be a number or null");
    }
    return { exitCode: o.exitCode };
  },
};
