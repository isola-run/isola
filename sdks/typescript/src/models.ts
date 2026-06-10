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
//
// Response decoding trusts the gateway's typed contract: `record` asserts the
// payload is an object and views it as the expected model, required fields the
// SDK dereferences are presence-checked, and timestamps are coerced to Date.
// Optional fields are read straight off the wire without re-validating each
// one's type, matching how first-party SDKs (openai, anthropic) decode.

type Wire = Record<string, unknown>;

function record<T = Wire>(v: unknown): T {
  if (v === null || typeof v !== "object" || Array.isArray(v)) {
    throw new TypeError("expected object");
  }
  return v as T;
}

// `new Date` parses the gateway's RFC 3339 output; the ECMAScript Date Time
// String Format is parsed identically across engines, so we only reject a
// non-string or unparseable value.
function requiredDate(v: unknown): Date {
  if (typeof v !== "string") throw new TypeError("expected timestamp string");
  const d = new Date(v);
  if (Number.isNaN(d.valueOf())) throw new TypeError("invalid timestamp");
  return d;
}

// Drops `undefined` AND `null` properties so outgoing JSON matches Python's
// `model_dump(by_alias=True, exclude_none=True)`. Empty submodels are
// preserved to match SnapshotRootfs() round-tripping as `{}`.
function dropNullish<T extends Record<string, unknown>>(obj: T): Wire {
  const out: Wire = {};
  for (const [k, v] of Object.entries(obj)) {
    if (v != null) out[k] = v;
  }
  return out;
}

// ---------- Public types ----------

/** Lifecycle status of a sandbox. */
export type SandboxStatus = "Pending" | "Running" | "Terminating" | "Succeeded" | "Failed";

/** Lifecycle status of a rootfs snapshot. */
export type RootfsSnapshotStatus = "Pending" | "Running" | "Succeeded" | "Failed";

/**
 * Egress traffic shaping (token bucket) for a sandbox.
 *
 * Limits the sandbox's sustained outbound bandwidth. Enforced by gVisor
 * inside the sandbox, independent of the egress policy: it shapes whatever
 * egress is allowed, including none. Requires gVisor release-20260601.0 or
 * later on cluster nodes.
 */
export interface EgressRateLimit {
  /** Sustained egress rate in bytes per second. */
  rateBytesPerSecond: number;
  /**
   * Token bucket depth in bytes (min 131072, max 2^32 - 1). When omitted,
   * the server derives `min(max(rate / 10, 131072), 2^32 - 1)`.
   */
  burstBytes?: number;
}

/** @internal */
export const EgressRateLimit = {
  fromWire(json: unknown): EgressRateLimit {
    const o = record<EgressRateLimit>(json);
    if (typeof o.rateBytesPerSecond !== "number") {
      throw new TypeError("EgressRateLimit.rateBytesPerSecond is required");
    }
    const out: EgressRateLimit = { rateBytesPerSecond: o.rateBytesPerSecond };
    if (o.burstBytes != null) out.burstBytes = o.burstBytes;
    return out;
  },
  toWire(e: EgressRateLimit): Wire {
    return dropNullish(e as unknown as Record<string, unknown>);
  },
};

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
  /**
   * Egress traffic shaping (token bucket). Requires gVisor
   * release-20260601.0 or later on cluster nodes.
   */
  egressRateLimit?: EgressRateLimit;
}

/** @internal */
export const Network = {
  fromWire(json: unknown): Network {
    const o = record<Network>(json);
    const out: Network = {};
    if (o.allowInternetEgress != null) out.allowInternetEgress = o.allowInternetEgress;
    if (o.allowedEgressCIDRs != null) out.allowedEgressCIDRs = o.allowedEgressCIDRs;
    if (o.allowClusterDNS != null) out.allowClusterDNS = o.allowClusterDNS;
    if (o.nameservers != null) out.nameservers = o.nameservers;
    if (o.allowIPv6Egress != null) out.allowIPv6Egress = o.allowIPv6Egress;
    if (o.egressRateLimit != null) out.egressRateLimit = EgressRateLimit.fromWire(o.egressRateLimit);
    return out;
  },
  toWire(n: Network): Wire {
    const out = dropNullish(n as Record<string, unknown>);
    if (n.egressRateLimit != null) out.egressRateLimit = EgressRateLimit.toWire(n.egressRateLimit);
    return out;
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

/** @internal */
export const ResourceList = {
  fromWire(json: unknown): ResourceList {
    const o = record<ResourceList>(json);
    const out: ResourceList = {};
    if (o.cpu != null) out.cpu = o.cpu;
    if (o.memory != null) out.memory = o.memory;
    if (o.ephemeralStorage != null) out.ephemeralStorage = o.ephemeralStorage;
    return out;
  },
  toWire(r: ResourceList): Wire {
    return dropNullish(r as Record<string, unknown>);
  },
};

/** CPU, memory, and ephemeral storage limits and requests for a container. */
export interface ResourceRequirements {
  limits?: ResourceList;
  requests?: ResourceList;
}

/** @internal */
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
    if (r.limits != null) out.limits = ResourceList.toWire(r.limits);
    if (r.requests != null) out.requests = ResourceList.toWire(r.requests);
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

/** @internal */
export const Container = {
  fromWire(json: unknown): Container {
    const o = record<Container>(json);
    if (typeof o.image !== "string") throw new TypeError("Container.image is required");
    const out: Container = { image: o.image };
    if (o.name != null) out.name = o.name;
    if (o.rootfsSnapshotName != null) out.rootfsSnapshotName = o.rootfsSnapshotName;
    if (o.command != null) out.command = o.command;
    if (o.env != null) out.env = o.env;
    if (o.resources != null) out.resources = ResourceRequirements.fromWire(o.resources);
    return out;
  },
  toWire(c: Container): Wire {
    // Field order mirrors Python's pydantic field declaration in _models.py so
    // wire bytes are identical across SDKs. `!= null` so a JS caller passing
    // `null` for an optional field doesn't crash the toWire pipeline.
    const out: Wire = {};
    if (c.name != null) out.name = c.name;
    out.image = c.image;
    if (c.rootfsSnapshotName != null) out.rootfsSnapshotName = c.rootfsSnapshotName;
    if (c.command != null) out.command = c.command;
    if (c.env != null) out.env = c.env;
    if (c.resources != null) out.resources = ResourceRequirements.toWire(c.resources);
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

/** @internal */
export const ContainerInfo = {
  fromWire(json: unknown): ContainerInfo {
    const o = record<ContainerInfo>(json);
    if (typeof o.name !== "string") throw new TypeError("ContainerInfo.name is required");
    if (typeof o.image !== "string") throw new TypeError("ContainerInfo.image is required");
    const out: ContainerInfo = { name: o.name, image: o.image };
    if (o.rootfsSnapshotName != null) out.rootfsSnapshotName = o.rootfsSnapshotName;
    if (o.command != null) out.command = o.command;
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

/** @internal */
export const SnapshotRootfs = {
  fromWire(json: unknown): SnapshotRootfs {
    const o = record<SnapshotRootfs>(json);
    const out: SnapshotRootfs = {};
    if (o.snapshotName != null) out.snapshotName = o.snapshotName;
    if (o.timeoutSeconds != null) out.timeoutSeconds = o.timeoutSeconds;
    return out;
  },
  toWire(s: SnapshotRootfs): Wire {
    return dropNullish(s as Record<string, unknown>);
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

/** @internal */
export const SandboxSummary = {
  fromWire(json: unknown): SandboxSummary {
    const o = record<SandboxSummary>(json);
    if (typeof o.id !== "string") throw new TypeError("SandboxSummary.id is required");
    return {
      id: o.id,
      status: o.status,
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

/** @internal */
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
//
// Every type below is @internal: used cross-file inside the package but not
// re-exported from index.ts. stripInternal removes them from dist/index.d.ts.

/** @internal */
export interface PodTemplate {
  containers: Container[];
}

/** @internal */
export const PodTemplate = {
  toWire(t: PodTemplate): Wire {
    return { containers: t.containers.map((c) => Container.toWire(c)) };
  },
};

/** @internal */
export interface PodTemplateInfo {
  containers: ContainerInfo[];
}

/** @internal */
export const PodTemplateInfo = {
  fromWire(json: unknown): PodTemplateInfo {
    const o = record(json);
    if (!Array.isArray(o.containers)) throw new TypeError("PodTemplateInfo.containers is required");
    return { containers: o.containers.map((c) => ContainerInfo.fromWire(c)) };
  },
};

/** @internal */
export interface TerminationPolicy {
  type: "SnapshotRootfs" | "Delete";
  snapshotRootfs?: SnapshotRootfs;
}

/** @internal */
export const TerminationPolicy = {
  fromWire(json: unknown): TerminationPolicy {
    const o = record(json);
    // "Delete" is the gateway's default; a sandbox created without an
    // explicit terminationPolicy comes back as {type: "Delete"}.
    if (o.type !== "SnapshotRootfs" && o.type !== "Delete") {
      throw new TypeError(`invalid TerminationPolicy.type: ${String(o.type)}`);
    }
    const out: TerminationPolicy = { type: o.type };
    if (o.snapshotRootfs != null) out.snapshotRootfs = SnapshotRootfs.fromWire(o.snapshotRootfs);
    return out;
  },
  // Wraps a user-supplied SnapshotRootfs into the discriminated wire shape.
  // CreateSandboxPayload.toWire delegates here so the wrapping rule lives
  // in exactly one place.
  toWire(input: SnapshotRootfs): Wire {
    return { type: "SnapshotRootfs", snapshotRootfs: SnapshotRootfs.toWire(input) };
  },
};

/** @internal */
export interface CreateSandboxPayload {
  podTemplate: PodTemplate;
  timeoutSeconds?: number;
  startupTimeoutSeconds?: number;
  network?: Network;
  terminationPolicy?: SnapshotRootfs;
}

/** @internal */
export const CreateSandboxPayload = {
  toWire(p: CreateSandboxPayload): Wire {
    // `!= null` for optional sub-models so a JS caller passing `null` doesn't
    // crash the toWire pipeline (matches Python's `exclude_none=True`).
    const out: Wire = { podTemplate: PodTemplate.toWire(p.podTemplate) };
    if (p.timeoutSeconds != null) out.timeoutSeconds = p.timeoutSeconds;
    if (p.startupTimeoutSeconds != null) out.startupTimeoutSeconds = p.startupTimeoutSeconds;
    if (p.network != null) out.network = Network.toWire(p.network);
    if (p.terminationPolicy != null) out.terminationPolicy = TerminationPolicy.toWire(p.terminationPolicy);
    return out;
  },
};

/** @internal */
export interface ListSandboxesResponse {
  sandboxes: SandboxSummary[];
}

/** @internal */
export const ListSandboxesResponse = {
  fromWire(json: unknown): ListSandboxesResponse {
    const o = record(json);
    if (o.sandboxes == null) return { sandboxes: [] };
    if (!Array.isArray(o.sandboxes)) throw new TypeError("sandboxes must be an array");
    return { sandboxes: o.sandboxes.map((s) => SandboxSummary.fromWire(s)) };
  },
};

/** @internal */
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

/** @internal */
export const SandboxData = {
  fromWire(json: unknown): SandboxData {
    const o = record<SandboxData>(json);
    if (typeof o.id !== "string") throw new TypeError("SandboxData.id is required");
    if (typeof o.startupTimeoutSeconds !== "number") {
      throw new TypeError("SandboxData.startupTimeoutSeconds is required");
    }
    const out: SandboxData = {
      id: o.id,
      podTemplate: PodTemplateInfo.fromWire(o.podTemplate),
      status: o.status,
      creationTimestamp: requiredDate(o.creationTimestamp),
      startupTimeoutSeconds: o.startupTimeoutSeconds,
    };
    if (o.timeoutSeconds != null) out.timeoutSeconds = o.timeoutSeconds;
    if (o.network != null) out.network = Network.fromWire(o.network);
    if (o.terminationPolicy != null) out.terminationPolicy = TerminationPolicy.fromWire(o.terminationPolicy);
    return out;
  },
};

/** @internal */
export interface CreateRootfsSnapshotPayload {
  sandboxId: string;
  snapshotName?: string;
  containerName?: string;
  timeoutSeconds?: number;
  ttlSecondsAfterFinished?: number;
}

/** @internal */
export const CreateRootfsSnapshotPayload = {
  toWire(p: CreateRootfsSnapshotPayload): Wire {
    const out: Wire = { sandboxId: p.sandboxId };
    if (p.snapshotName != null) out.snapshotName = p.snapshotName;
    if (p.containerName != null) out.containerName = p.containerName;
    if (p.timeoutSeconds != null) out.timeoutSeconds = p.timeoutSeconds;
    if (p.ttlSecondsAfterFinished != null) out.ttlSecondsAfterFinished = p.ttlSecondsAfterFinished;
    return out;
  },
};

/** @internal */
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

/** @internal */
export const RootfsSnapshotData = {
  fromWire(json: unknown): RootfsSnapshotData {
    const o = record<RootfsSnapshotData>(json);
    if (typeof o.id !== "string") throw new TypeError("RootfsSnapshotData.id is required");
    if (typeof o.sandboxId !== "string") throw new TypeError("RootfsSnapshotData.sandboxId is required");
    if (typeof o.snapshotName !== "string") throw new TypeError("RootfsSnapshotData.snapshotName is required");
    if (typeof o.timeoutSeconds !== "number") throw new TypeError("RootfsSnapshotData.timeoutSeconds is required");
    if (typeof o.ttlSecondsAfterFinished !== "number") {
      throw new TypeError("RootfsSnapshotData.ttlSecondsAfterFinished is required");
    }
    return {
      id: o.id,
      sandboxId: o.sandboxId,
      snapshotName: o.snapshotName,
      containerName: o.containerName ?? null,
      timeoutSeconds: o.timeoutSeconds,
      ttlSecondsAfterFinished: o.ttlSecondsAfterFinished,
      status: o.status,
      creationTimestamp: requiredDate(o.creationTimestamp),
    };
  },
};

/** @internal */
export interface CreateCommandPayload {
  args: string[];
  env?: Record<string, string>;
  cwd?: string;
  timeoutSeconds?: number;
}

/** @internal */
export const CreateCommandPayload = {
  toWire(p: CreateCommandPayload): Wire {
    const out: Wire = { args: p.args };
    if (p.env != null) out.env = p.env;
    if (p.cwd != null) out.cwd = p.cwd;
    if (p.timeoutSeconds != null) out.timeoutSeconds = p.timeoutSeconds;
    return out;
  },
};

/** @internal */
export interface CreateCommandResponse {
  id: string;
}

/** @internal */
export const CreateCommandResponse = {
  fromWire(json: unknown): CreateCommandResponse {
    const o = record(json);
    if (typeof o.id !== "string") throw new TypeError("CreateCommandResponse.id is required");
    return { id: o.id };
  },
};

/** @internal */
export interface CommandStatusResponse {
  exitCode: number | null;
}

/** @internal */
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
