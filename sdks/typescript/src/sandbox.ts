import type { APIClient } from "./client.js";
import type {
  CreateSandboxPayload,
  ListSandboxesResponse,
  NetworkSpec,
  ResourceList,
  ResourcesSpec,
  RootfsSnapshotSource,
  SandboxData,
  SandboxStatus,
  SandboxSummary,
} from "./models.js";
import { Commands } from "./commands.js";
import { Filesystem } from "./filesystem.js";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function sandboxPath(sandboxId: string): string {
  return `/v1/sandboxes/${encodeURIComponent(sandboxId)}`;
}

function buildResources(
  cpu?: string,
  memory?: string,
  ephemeralStorage?: string,
): ResourcesSpec | undefined {
  if (cpu === undefined && memory === undefined && ephemeralStorage === undefined) {
    return undefined;
  }
  const list: ResourceList = { cpu, memory, ephemeralStorage };
  return { limits: list, requests: list };
}

function buildRootfsSnapshotSources(
  source?: string,
): RootfsSnapshotSource[] | undefined {
  if (source === undefined) return undefined;
  return [{ snapshotName: source }];
}

// ---------------------------------------------------------------------------
// CreateSandboxOptions
// ---------------------------------------------------------------------------

export interface CreateSandboxOptions {
  /** Container image (required). */
  image: string;
  /** Override the container entrypoint. */
  command?: string[];
  /** Environment variables injected into the container. */
  env?: Record<string, string>;
  /** CPU resource request/limit (e.g. `"1"`, `"500m"`). */
  cpu?: string;
  /** Memory resource request/limit (e.g. `"512Mi"`, `"1Gi"`). */
  memory?: string;
  /** Ephemeral storage request/limit (e.g. `"1Gi"`). */
  ephemeralStorage?: string;
  /** Maximum lifetime in seconds (`activeDeadlineSeconds`). */
  timeout?: number;
  /** Network isolation rules. */
  network?: NetworkSpec;
  /** Restore from a prior rootfs snapshot by name. */
  rootfsSnapshotSource?: string;
}

// ---------------------------------------------------------------------------
// Sandboxes (resource collection)
// ---------------------------------------------------------------------------

/**
 * CRUD operations on sandboxes.
 *
 * Obtained via {@link Isola.sandboxes}.
 */
export class Sandboxes {
  /** @internal */
  constructor(private readonly api: APIClient) {}

  /** Create a new sandbox and return a {@link Sandbox} handle. */
  async create(options: CreateSandboxOptions): Promise<Sandbox> {
    const payload: CreateSandboxPayload = {
      podTemplate: {
        container: {
          image: options.image,
          command: options.command,
          env: options.env,
          resources: buildResources(
            options.cpu,
            options.memory,
            options.ephemeralStorage,
          ),
        },
      },
      activeDeadlineSeconds: options.timeout,
      network: options.network,
      rootfsSnapshotSources: buildRootfsSnapshotSources(
        options.rootfsSnapshotSource,
      ),
    };

    const data = await this.api.request<SandboxData>(
      "POST",
      "/v1/sandboxes",
      { body: payload },
    );

    return new Sandbox(this.api, data);
  }

  /** List all sandboxes. */
  async list(): Promise<SandboxSummary[]> {
    const resp = await this.api.request<ListSandboxesResponse>(
      "GET",
      "/v1/sandboxes",
    );
    return resp.sandboxes ?? [];
  }

  /** Get a sandbox by ID. */
  async get(sandboxId: string): Promise<Sandbox> {
    const data = await this.api.request<SandboxData>(
      "GET",
      sandboxPath(sandboxId),
    );
    return new Sandbox(this.api, data);
  }
}

// ---------------------------------------------------------------------------
// Sandbox
// ---------------------------------------------------------------------------

/**
 * Handle to a single sandbox instance.
 *
 * Provides access to {@link commands} and {@link filesystem} sub-resources.
 */
export class Sandbox {
  readonly id: string;
  readonly status: SandboxStatus;
  readonly creationTimestamp: Date;
  readonly network?: NetworkSpec;
  readonly timeout?: number;
  readonly rootfsSnapshotSources?: RootfsSnapshotSource[];
  readonly commands: Commands;
  readonly filesystem: Filesystem;

  /** @internal */
  constructor(
    private readonly api: APIClient,
    data: SandboxData,
  ) {
    this.id = data.id;
    this.status = data.status;
    this.creationTimestamp = new Date(data.creationTimestamp);
    this.network = data.network;
    this.timeout = data.activeDeadlineSeconds;
    this.rootfsSnapshotSources = data.rootfsSnapshotSources;
    this.commands = new Commands(api, data.id);
    this.filesystem = new Filesystem(api, data.id);
  }

  /** Delete this sandbox (idempotent). */
  async delete(): Promise<void> {
    await this.api.requestNoContent("DELETE", sandboxPath(this.id));
  }

  /**
   * Support `await using sandbox = ...` (TC39 Explicit Resource Management).
   * Calls {@link delete} on disposal.
   */
  async [Symbol.asyncDispose](): Promise<void> {
    await this.delete();
  }
}
