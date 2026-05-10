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
  network?: Network;
  /** Server-enforced sandbox lifetime, in seconds. */
  timeoutSeconds?: number;
  /** Server-enforced startup deadline, in seconds. */
  startupTimeoutSeconds?: number;
  /**
   * Termination policy. Pass a SnapshotRootfs to capture a rootfs snapshot
   * before deletion. The SDK wraps it on the wire as
   * `{ type: "SnapshotRootfs", snapshotRootfs: <input> }`.
   */
  terminationPolicy?: SnapshotRootfs;
  /** Client-side polling deadline; default 120_000ms. 0 = return immediately. */
  maxWaitMs?: number;
}

interface SingleContainerOptions extends CommonSandboxOptions {
  image: string;
  rootfsSnapshotName?: string;
  command?: string[];
  env?: Record<string, string>;
  cpu?: number;
  memory?: number;
  ephemeralStorage?: number;
  containers?: never;
}

interface MultiContainerOptions extends CommonSandboxOptions {
  containers: Container[];
  image?: never;
  rootfsSnapshotName?: never;
  command?: never;
  env?: never;
  cpu?: never;
  memory?: never;
  ephemeralStorage?: never;
}

export type CreateSandboxOptions = SingleContainerOptions | MultiContainerOptions;

const DEFAULT_MAX_WAIT_MS = 120_000;

function buildResources(
  cpu: number | undefined,
  memory: number | undefined,
  ephemeralStorage: number | undefined,
): ResourceRequirements | undefined {
  if (cpu === undefined && memory === undefined && ephemeralStorage === undefined) return undefined;
  const list: ResourceList = {};
  if (cpu !== undefined) list.cpu = `${Math.round(cpu * 1000)}m`;
  if (memory !== undefined) list.memory = `${memory}Mi`;
  if (ephemeralStorage !== undefined) list.ephemeralStorage = `${ephemeralStorage}Mi`;
  return { limits: list, requests: list };
}

function isMulti(opts: CreateSandboxOptions): opts is MultiContainerOptions {
  return Array.isArray((opts as { containers?: unknown }).containers);
}

function buildContainers(opts: CreateSandboxOptions): Container[] {
  if (isMulti(opts)) {
    if (opts.containers.length === 0) {
      throw new Error("containers must be a non-empty array");
    }
    // Compile-time invariants forbid these fields alongside `containers`, but
    // we still assert at runtime to match Python's _validate_create_args.
    if ((opts as unknown as Record<string, unknown>).image !== undefined) {
      throw new Error("cannot specify both 'image' and 'containers'");
    }
    const offending = (["command", "env", "cpu", "memory", "ephemeralStorage", "rootfsSnapshotName"] as const).filter(
      (k) => (opts as unknown as Record<string, unknown>)[k] !== undefined,
    );
    if (offending.length > 0) {
      throw new Error(
        `cannot specify ${offending.map((k) => `'${k}'`).join(", ")} when using 'containers'; ` +
          "set these on each Container instead",
      );
    }
    return opts.containers;
  }

  if (typeof (opts as SingleContainerOptions).image !== "string") {
    throw new Error("must specify either 'image' or 'containers'");
  }
  const single = opts as SingleContainerOptions;
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

export class Sandboxes {
  /** @internal */
  readonly _api: HttpClient;

  constructor(api: HttpClient) {
    this._api = api;
  }

  async create(opts: CreateSandboxOptions, req: RequestOptions = {}): Promise<Sandbox> {
    const containerList = buildContainers(opts);
    const payload: CreateSandboxPayload = {
      podTemplate: { containers: containerList },
    };
    if (opts.network !== undefined) payload.network = opts.network;
    if (opts.timeoutSeconds !== undefined) payload.timeoutSeconds = opts.timeoutSeconds;
    if (opts.startupTimeoutSeconds !== undefined) payload.startupTimeoutSeconds = opts.startupTimeoutSeconds;
    if (opts.terminationPolicy !== undefined) payload.terminationPolicy = opts.terminationPolicy;

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

export class Sandbox {
  /** @internal */
  readonly _api: HttpClient;
  /** @internal */
  readonly _data: SandboxData;
  readonly commands: Commands;
  readonly filesystem: Filesystem;

  constructor(api: HttpClient, data: SandboxData) {
    this._api = api;
    this._data = data;
    this.commands = new Commands(api, data.id);
    this.filesystem = new Filesystem(api, data.id);
  }

  get id(): string {
    return this._data.id;
  }

  get status(): SandboxStatus {
    return this._data.status;
  }

  get creationTimestamp(): Date {
    return this._data.creationTimestamp;
  }

  get network(): Network | null {
    return this._data.network ?? null;
  }

  get timeoutSeconds(): number | null {
    return this._data.timeoutSeconds ?? null;
  }

  get startupTimeoutSeconds(): number {
    return this._data.startupTimeoutSeconds;
  }

  get containers(): ContainerInfo[] {
    return this._data.podTemplate.containers;
  }

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
