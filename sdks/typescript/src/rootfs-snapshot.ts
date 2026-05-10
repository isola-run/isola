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

// Mirrors sdks/python/src/isola/_rootfs_snapshot.py:AsyncRootfsSnapshots and
// AsyncRootfsSnapshot.

import type { RequestOptions } from "./client";
import { IsolaError, IsolaTimeoutError, NotFoundError } from "./errors";
import { type HttpClient, sleep } from "./internal/http";
import { rootfsSnapshotsPath, snapshotPath } from "./internal/url";
import {
  type CreateRootfsSnapshotPayload,
  CreateRootfsSnapshotPayload as CreateRootfsSnapshotPayloadModel,
  type RootfsSnapshotData,
  RootfsSnapshotData as RootfsSnapshotDataModel,
  type RootfsSnapshotStatus,
} from "./models";

const POLL_INTERVAL_MS = 1_000;
const DEFAULT_MAX_WAIT_MS = 310_000;

/** Options for {@link RootfsSnapshots.create}. */
export interface CreateSnapshotOptions {
  /** ID of the sandbox to snapshot. */
  sandboxId: string;
  /**
   * Name for the snapshot. Defaults to the sandbox's ID on the
   * server. Use this name later as `rootfsSnapshotName` when creating
   * a new sandbox.
   */
  snapshotName?: string;
  /**
   * Which container to snapshot, for multi-container sandboxes.
   * Defaults to the first container.
   */
  containerName?: string;
  /**
   * Maximum time for the snapshot operation, in seconds. Enforced
   * server-side. The server defaults to 300 seconds if not set.
   */
  timeoutSeconds?: number;
  /**
   * How long the Kubernetes resource is retained after the snapshot
   * completes, in seconds. The server defaults to 300 seconds if not
   * set.
   */
  ttlSecondsAfterFinished?: number;
  /**
   * How long this method polls for completion, in milliseconds.
   * Client-side only. Defaults to 310_000ms.
   */
  maxWaitMs?: number;
}

function checkFailed(snapshotId: string, status: RootfsSnapshotStatus): void {
  if (status === "Failed") {
    throw new IsolaError(`rootfs snapshot ${snapshotId} reached terminal state: ${status}`);
  }
}

async function waitUntilComplete(
  api: HttpClient,
  snapshotId: string,
  maxWaitMs: number,
  signal: AbortSignal | undefined,
): Promise<RootfsSnapshotData> {
  const deadline = performance.now() + maxWaitMs;
  while (true) {
    let data: RootfsSnapshotData;
    try {
      data = await api.requestModel<RootfsSnapshotData>(
        {
          method: "GET",
          path: snapshotPath(snapshotId),
          ...(signal ? { signal } : {}),
        },
        RootfsSnapshotDataModel.fromWire,
      );
    } catch (err) {
      if (err instanceof NotFoundError) {
        if (performance.now() >= deadline) {
          throw new IsolaTimeoutError(
            `rootfs snapshot ${snapshotId} did not reach complete state within ${maxWaitMs}ms`,
            { cause: err },
          );
        }
        await sleep(POLL_INTERVAL_MS, signal);
        continue;
      }
      throw err;
    }

    if (data.status === "Succeeded") return data;
    checkFailed(snapshotId, data.status);
    if (performance.now() >= deadline) {
      throw new IsolaTimeoutError(`rootfs snapshot ${snapshotId} did not reach complete state within ${maxWaitMs}ms`);
    }
    await sleep(POLL_INTERVAL_MS, signal);
  }
}

/**
 * Manage rootfs snapshots.
 *
 * Rootfs snapshots capture one container's root filesystem changes at
 * a point in time. Other mounts (e.g. tmpfs like `/tmp`) are not
 * included. Restore a snapshot when creating a new sandbox to pick up
 * where you left off.
 */
export class RootfsSnapshots {
  /** @internal */
  readonly _api: HttpClient;

  constructor(api: HttpClient) {
    this._api = api;
  }

  /**
   * Create a rootfs snapshot from a running sandbox.
   *
   * Blocks until the snapshot completes, up to `maxWaitMs`. Set
   * `maxWaitMs: 0` to return immediately.
   *
   * @example
   * ```ts
   * const snapshot = await client.rootfsSnapshots.create({
   *   sandboxId: sandbox.id,
   *   snapshotName: "my-snapshot",
   * });
   * ```
   *
   * @param opts - Snapshot options.
   * @returns A {@link RootfsSnapshot} with the snapshot metadata and
   * status.
   * @throws {IsolaError} If the snapshot fails.
   * @throws {IsolaTimeoutError} If the snapshot does not complete
   * within `maxWaitMs`.
   */
  async create(opts: CreateSnapshotOptions, req: RequestOptions = {}): Promise<RootfsSnapshot> {
    const payload: CreateRootfsSnapshotPayload = { sandboxId: opts.sandboxId };
    if (opts.snapshotName !== undefined) payload.snapshotName = opts.snapshotName;
    if (opts.containerName !== undefined) payload.containerName = opts.containerName;
    if (opts.timeoutSeconds !== undefined) payload.timeoutSeconds = opts.timeoutSeconds;
    if (opts.ttlSecondsAfterFinished !== undefined) payload.ttlSecondsAfterFinished = opts.ttlSecondsAfterFinished;

    const data = await this._api.requestModel<RootfsSnapshotData>(
      {
        method: "POST",
        path: rootfsSnapshotsPath(),
        jsonBody: CreateRootfsSnapshotPayloadModel.toWire(payload),
        ...(req.signal ? { signal: req.signal } : {}),
      },
      RootfsSnapshotDataModel.fromWire,
    );
    checkFailed(data.id, data.status);

    const maxWaitMs = opts.maxWaitMs ?? DEFAULT_MAX_WAIT_MS;
    let finalData = data;
    if (data.status !== "Succeeded" && maxWaitMs !== 0) {
      finalData = await waitUntilComplete(this._api, data.id, maxWaitMs, req.signal);
    }
    return new RootfsSnapshot(finalData);
  }

  /**
   * Get a rootfs snapshot by ID.
   *
   * @param snapshotId - The snapshot's unique identifier.
   * @returns A {@link RootfsSnapshot} with the current state.
   * @throws {NotFoundError} If the snapshot's Kubernetes resource no
   * longer exists. A completed snapshot's data remains in storage
   * even after `NotFoundError` on the K8s resource and can still be
   * restored by `snapshotName`.
   */
  async get(snapshotId: string, req: RequestOptions = {}): Promise<RootfsSnapshot> {
    const data = await this._api.requestModel<RootfsSnapshotData>(
      {
        method: "GET",
        path: snapshotPath(snapshotId),
        ...(req.signal ? { signal: req.signal } : {}),
      },
      RootfsSnapshotDataModel.fromWire,
    );
    return new RootfsSnapshot(data);
  }
}

/**
 * A rootfs snapshot.
 *
 * Inspect the snapshot's status and metadata. To restore from this
 * snapshot, pass its `snapshotName` as `rootfsSnapshotName` when
 * creating a new sandbox.
 */
export class RootfsSnapshot {
  /** @internal */
  readonly _data: RootfsSnapshotData;

  constructor(data: RootfsSnapshotData) {
    this._data = data;
  }

  /** Unique identifier of the snapshot. */
  get id(): string {
    return this._data.id;
  }

  /** Current lifecycle status of the snapshot. */
  get status(): RootfsSnapshotStatus {
    return this._data.status;
  }

  /** When the snapshot was created. */
  get creationTimestamp(): Date {
    return this._data.creationTimestamp;
  }

  /** Name of the snapshot. Use this to restore from it. */
  get snapshotName(): string {
    return this._data.snapshotName;
  }

  /** ID of the sandbox this snapshot was taken from. */
  get sandboxId(): string {
    return this._data.sandboxId;
  }

  /** Container that was snapshotted, or `null` for the default. */
  get containerName(): string | null {
    return this._data.containerName;
  }

  /** Server-side timeout for the snapshot operation, in seconds. */
  get timeoutSeconds(): number {
    return this._data.timeoutSeconds;
  }

  /**
   * How long the Kubernetes resource is retained after the snapshot
   * completes, in seconds.
   */
  get ttlSecondsAfterFinished(): number {
    return this._data.ttlSecondsAfterFinished;
  }
}
