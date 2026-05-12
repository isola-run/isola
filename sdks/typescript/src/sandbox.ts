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

// Mirrors sdks/python/src/isola/_sandbox.py:AsyncSandbox and AsyncSandboxes.

import type { RequestOptions } from "./client";
import { Commands } from "./commands";
import { IsolaError, IsolaTimeoutError, NotFoundError } from "./errors";
import { Filesystem } from "./filesystem";
import { type HttpClient, sleep } from "./internal/http";
import { sandboxesPath, sandboxPath } from "./internal/url";
import {
  type Container,
  type ContainerInfo,
  type CreateSandboxPayload,
  CreateSandboxPayload as CreateSandboxPayloadModel,
  type ListSandboxesResponse,
  ListSandboxesResponse as ListSandboxesResponseModel,
  type Network,
  type ResourceList,
  type ResourceRequirements,
  type SandboxData,
  SandboxData as SandboxDataModel,
  type SandboxStatus,
  type SandboxSummary,
  type SnapshotRootfs,
} from "./models";

const POLL_INTERVAL_MS = 1_000;
const TERMINAL_STATUSES: ReadonlySet<SandboxStatus> = new Set(["Succeeded", "Failed"]);

interface CommonSandboxOptions {
  /**
   * Network policy. Sandboxes have no network access by default. See
   * {@link Network}.
   */
  network?: Network;
  /**
   * How long the sandbox runs before the server begins the
   * termination process, in seconds. Enforced server-side. If omitted,
   * the server defaults to no limit.
   */
  timeoutSeconds?: number;
  /**
   * Maximum time for the sandbox pod to become ready, in seconds.
   * Enforced server-side. If it expires, the sandbox is marked as
   * `Failed`. The server defaults to 90 seconds if not set.
   */
  startupTimeoutSeconds?: number;
  /**
   * Action to run before the sandbox pod is removed. Defaults to
   * immediate deletion if not set. Pass a {@link SnapshotRootfs} to
   * snapshot the container's rootfs changes before removal.
   *
   * The SDK wraps the value on the wire as
   * `{ type: "SnapshotRootfs", snapshotRootfs: <input> }`.
   */
  terminationPolicy?: SnapshotRootfs;
  /**
   * How long to wait for the sandbox to be ready, in milliseconds.
   * Client-side only; does not affect the sandbox on the server.
   * Defaults to 120_000ms. Set to 0 to return immediately without
   * waiting.
   */
  maxWaitMs?: number;
}

interface SingleContainerOptions extends CommonSandboxOptions {
  /** Container image to run (e.g. `"python:3.12"`). */
  image: string;
  /** Restore the container's filesystem from this named snapshot. */
  rootfsSnapshotName?: string;
  /**
   * Command and arguments to run in the container. If not set,
   * defaults to `sleep infinity`.
   */
  command?: string[];
  /** Environment variables as key-value pairs. */
  env?: Record<string, string>;
  /**
   * CPU limit in cores (e.g. `0.5`, `2.0`). Sets both the Kubernetes
   * request and limit. If omitted, no CPU limit is applied.
   */
  cpu?: number;
  /**
   * Memory limit in MiB (e.g. `256`, `1024`). Sets both the
   * Kubernetes request and limit. If omitted, no memory limit is
   * applied.
   */
  memory?: number;
  /**
   * Ephemeral storage limit in MiB. Sets both the Kubernetes request
   * and limit. If omitted, no ephemeral storage limit is applied.
   */
  ephemeralStorage?: number;
  containers?: never;
}

interface MultiContainerOptions extends CommonSandboxOptions {
  /**
   * List of {@link Container} specs for multi-container sandboxes.
   * Cannot be combined with `image` or per-container shorthand
   * options.
   */
  containers: Container[];
  image?: never;
  rootfsSnapshotName?: never;
  command?: never;
  env?: never;
  cpu?: never;
  memory?: never;
  ephemeralStorage?: never;
}

/**
 * Options for {@link Sandboxes.create}.
 *
 * Pass either `image` (single-container shorthand) or `containers`
 * (multi-container), never both.
 */
export type CreateSandboxOptions = SingleContainerOptions | MultiContainerOptions;

const DEFAULT_MAX_WAIT_MS = 120_000;

// Half-even (banker's) rounding, matching Python's built-in `round`. Math.round
// rounds halves toward +∞ (Math.round(2.5)===3, Math.round(-1.5)===-1); banker's
// goes to the nearest even, so 2.5→2 and -1.5→-2. When Math.round lands on an
// odd integer at a half-tie, subtract 1 (always a valid step to even on both
// signs because Math.round picked the +∞-side neighbour).
function roundHalfEven(n: number): number {
  const rounded = Math.round(n);
  if (Math.abs(n - Math.trunc(n)) !== 0.5) return rounded;
  return rounded % 2 === 0 ? rounded : rounded - 1;
}

function buildResources(
  cpu: number | undefined,
  memory: number | undefined,
  ephemeralStorage: number | undefined,
): ResourceRequirements | undefined {
  if (cpu === undefined && memory === undefined && ephemeralStorage === undefined) return undefined;
  const list: ResourceList = {};
  if (cpu !== undefined) list.cpu = `${roundHalfEven(cpu * 1000)}m`;
  if (memory !== undefined) list.memory = `${memory}Mi`;
  if (ephemeralStorage !== undefined) list.ephemeralStorage = `${ephemeralStorage}Mi`;
  return { limits: list, requests: list };
}

function buildContainers(opts: CreateSandboxOptions): Container[] {
  // Decide mode by property presence, then validate the shape — so a JS caller
  // passing `containers: <non-array>` gets a clear error rather than silently
  // dropping the value down the single-container path.
  const raw = opts as unknown as Record<string, unknown>;
  const hasContainers = raw.containers !== undefined;
  const hasImage = raw.image !== undefined;

  if (hasContainers && hasImage) {
    throw new Error("cannot specify both 'image' and 'containers'");
  }
  if (!hasContainers && !hasImage) {
    throw new Error("must specify either 'image' or 'containers'");
  }

  if (hasContainers) {
    if (!Array.isArray(raw.containers)) {
      throw new Error("'containers' must be an array");
    }
    if (raw.containers.length === 0) {
      throw new Error("containers must be a non-empty array");
    }
    const offending = (["command", "env", "cpu", "memory", "ephemeralStorage", "rootfsSnapshotName"] as const).filter(
      (k) => raw[k] !== undefined,
    );
    if (offending.length > 0) {
      throw new Error(
        `cannot specify ${offending.map((k) => `'${k}'`).join(", ")} when using 'containers'; ` +
          "set these on each Container instead",
      );
    }
    return raw.containers as Container[];
  }

  const single = opts as SingleContainerOptions;
  if (typeof single.image !== "string") {
    throw new Error("'image' must be a string");
  }
  const resources = buildResources(single.cpu, single.memory, single.ephemeralStorage);
  const container: Container = { image: single.image };
  if (single.command !== undefined) container.command = single.command;
  if (single.env !== undefined) container.env = single.env;
  if (single.rootfsSnapshotName !== undefined) container.rootfsSnapshotName = single.rootfsSnapshotName;
  if (resources !== undefined) container.resources = resources;
  return [container];
}

function checkTerminal(sandboxId: string, status: SandboxStatus): void {
  if (TERMINAL_STATUSES.has(status)) {
    throw new IsolaError(`sandbox ${sandboxId} reached terminal state: ${status}`);
  }
}

async function waitUntilRunning(
  api: HttpClient,
  sandboxId: string,
  maxWaitMs: number,
  signal: AbortSignal | undefined,
): Promise<SandboxData> {
  const deadline = performance.now() + maxWaitMs;
  while (true) {
    let data: SandboxData;
    try {
      data = await api.requestModel<SandboxData>(
        {
          method: "GET",
          path: sandboxPath(sandboxId),
          ...(signal ? { signal } : {}),
        },
        SandboxDataModel.fromWire,
      );
    } catch (err) {
      if (err instanceof NotFoundError) {
        if (performance.now() >= deadline) {
          throw new IsolaTimeoutError(`sandbox ${sandboxId} did not reach running state within ${maxWaitMs}ms`, {
            cause: err,
          });
        }
        await sleep(POLL_INTERVAL_MS, signal);
        continue;
      }
      throw err;
    }

    if (data.status === "Running") return data;
    checkTerminal(sandboxId, data.status);

    if (performance.now() >= deadline) {
      throw new IsolaTimeoutError(`sandbox ${sandboxId} did not reach running state within ${maxWaitMs}ms`);
    }
    await sleep(POLL_INTERVAL_MS, signal);
  }
}

/** Create, list, and retrieve sandboxes. */
export class Sandboxes {
  /** @internal */
  readonly _api: HttpClient;

  /** @internal */
  constructor(api: HttpClient) {
    this._api = api;
  }

  /**
   * Create a new sandbox and wait for it to be ready.
   *
   * There are two ways to specify what to run:
   *
   * - **Single container (common):** pass `image` and optionally
   *   `command`, `env`, `cpu`, `memory`, `ephemeralStorage`, and
   *   `rootfsSnapshotName`.
   * - **Multiple containers:** pass a `containers` list of
   *   {@link Container} objects. Per-container options go on each
   *   `Container`.
   *
   * The method blocks until the sandbox reaches the `Running` state,
   * up to `maxWaitMs`. Set `maxWaitMs: 0` to return immediately
   * without waiting.
   *
   * **Timeouts:**
   *
   * - `maxWaitMs` (client-side): How long this method polls before
   *   giving up. Does not affect the sandbox on the server.
   * - `startupTimeoutSeconds` (server-side): How long the server waits
   *   for the sandbox pod to become ready. If it expires, the sandbox
   *   is marked as `Failed`. The server defaults to 90 seconds if not
   *   set.
   * - `timeoutSeconds` (server-side): How long the sandbox runs before
   *   the server begins the termination process. If omitted, the
   *   server defaults to no limit.
   *
   * @example
   * ```ts
   * await using sandbox = await client.sandboxes.create({
   *   image: "alpine:3.21",
   * });
   * ```
   *
   * @param opts - Single-container or multi-container sandbox options.
   * @param req - Per-call request options (e.g. `signal`).
   * @returns A {@link Sandbox} instance. If `maxWaitMs` is 0, the
   * sandbox may not be ready yet (check `status`).
   * @throws {Error} If both `image` and `containers` are set, or if
   * per-container options are used with `containers`.
   * @throws {IsolaTimeoutError} If the sandbox is not ready within
   * `maxWaitMs`.
   * @throws {IsolaError} If the sandbox reaches a terminal failed
   * state.
   */
  async create(opts: CreateSandboxOptions, req: RequestOptions = {}): Promise<Sandbox> {
    const containerList = buildContainers(opts);
    const payload: CreateSandboxPayload = {
      podTemplate: { containers: containerList },
    };
    // `!= null` (not `!== undefined`) so a JS caller passing `null` for an
    // optional sub-model doesn't reach a downstream `Object.entries(null)`
    // inside the toWire pipeline (Python's `exclude_none=True` analogue).
    if (opts.network != null) payload.network = opts.network;
    if (opts.timeoutSeconds != null) payload.timeoutSeconds = opts.timeoutSeconds;
    if (opts.startupTimeoutSeconds != null) payload.startupTimeoutSeconds = opts.startupTimeoutSeconds;
    if (opts.terminationPolicy != null) payload.terminationPolicy = opts.terminationPolicy;

    const data = await this._api.requestModel<SandboxData>(
      {
        method: "POST",
        path: sandboxesPath(),
        jsonBody: CreateSandboxPayloadModel.toWire(payload),
        ...(req.signal ? { signal: req.signal } : {}),
      },
      SandboxDataModel.fromWire,
    );
    checkTerminal(data.id, data.status);

    const maxWaitMs = opts.maxWaitMs ?? DEFAULT_MAX_WAIT_MS;
    let finalData = data;
    if (data.status !== "Running" && maxWaitMs !== 0) {
      finalData = await waitUntilRunning(this._api, data.id, maxWaitMs, req.signal);
    }
    return new Sandbox(this._api, finalData);
  }

  /**
   * List sandboxes. Results are eventually consistent.
   *
   * @returns A list of {@link SandboxSummary} objects with `id`,
   * `status`, and `creationTimestamp`.
   */
  async list(req: RequestOptions = {}): Promise<SandboxSummary[]> {
    const response = await this._api.requestModel<ListSandboxesResponse>(
      {
        method: "GET",
        path: sandboxesPath(),
        ...(req.signal ? { signal: req.signal } : {}),
      },
      ListSandboxesResponseModel.fromWire,
    );
    return response.sandboxes;
  }

  /**
   * Get a sandbox by ID.
   *
   * @param sandboxId - The sandbox's unique identifier.
   * @returns A {@link Sandbox} instance with the current state.
   * @throws {NotFoundError} If no sandbox with that ID exists.
   */
  async get(sandboxId: string, req: RequestOptions = {}): Promise<Sandbox> {
    const data = await this._api.requestModel<SandboxData>(
      {
        method: "GET",
        path: sandboxPath(sandboxId),
        ...(req.signal ? { signal: req.signal } : {}),
      },
      SandboxDataModel.fromWire,
    );
    return new Sandbox(this._api, data);
  }
}

/**
 * A handle to a sandbox.
 *
 * Use {@link Sandbox.commands} to execute processes and
 * {@link Sandbox.filesystem} to read and write files. Sandboxes are
 * async-disposable: use `await using` to automatically delete the
 * sandbox when you are done.
 *
 * @example
 * ```ts
 * await using sandbox = await client.sandboxes.create({
 *   image: "alpine:3.21",
 * });
 * const result = await sandbox.commands.run(["echo", "hello"]);
 * console.log(result.stdout);
 * // sandbox is deleted here
 * ```
 */
export class Sandbox {
  /** @internal */
  readonly _api: HttpClient;
  /** @internal */
  readonly _data: SandboxData;
  readonly commands: Commands;
  readonly filesystem: Filesystem;

  /** @internal */
  constructor(api: HttpClient, data: SandboxData) {
    this._api = api;
    this._data = data;
    this.commands = new Commands(api, data.id);
    this.filesystem = new Filesystem(api, data.id);
  }

  /** Unique identifier of the sandbox. */
  get id(): string {
    return this._data.id;
  }

  /** Current lifecycle status. */
  get status(): SandboxStatus {
    return this._data.status;
  }

  /** When the sandbox was created. */
  get creationTimestamp(): Date {
    return this._data.creationTimestamp;
  }

  /** Network configuration, or `null` if using defaults. */
  get network(): Network | null {
    return this._data.network ?? null;
  }

  /**
   * How long the sandbox runs before the server begins the
   * termination process, in seconds. `null` means no limit.
   */
  get timeoutSeconds(): number | null {
    return this._data.timeoutSeconds ?? null;
  }

  /**
   * Maximum time for the sandbox pod to become ready, in seconds. If
   * exceeded, the sandbox is marked as `Failed`.
   */
  get startupTimeoutSeconds(): number {
    return this._data.startupTimeoutSeconds;
  }

  /**
   * The sandbox's containers. Does not include init or ephemeral
   * containers.
   */
  get containers(): ContainerInfo[] {
    return this._data.podTemplate.containers;
  }

  /**
   * Delete the sandbox.
   *
   * Returns as soon as the server accepts the request. The sandbox
   * enters `Terminating` state while the termination policy runs,
   * then the Sandbox resource is deleted and {@link Sandboxes.get}
   * would return `NotFoundError`.
   */
  async delete(req: RequestOptions = {}): Promise<void> {
    await this._api.requestNoContent({
      method: "DELETE",
      path: sandboxPath(this._data.id),
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }

  async [Symbol.asyncDispose](): Promise<void> {
    await this.delete();
  }
}
