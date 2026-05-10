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

export interface CreateSnapshotOptions {
  sandboxId: string;
  snapshotName?: string;
  containerName?: string;
  timeoutSeconds?: number;
  ttlSecondsAfterFinished?: number;
  /** Client-side polling deadline; default 310_000ms. */
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

export class RootfsSnapshots {
  /** @internal */
  readonly _api: HttpClient;

  constructor(api: HttpClient) {
    this._api = api;
  }

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

export class RootfsSnapshot {
  /** @internal */
  readonly _data: RootfsSnapshotData;

  constructor(data: RootfsSnapshotData) {
    this._data = data;
  }

  get id(): string {
    return this._data.id;
  }

  get status(): RootfsSnapshotStatus {
    return this._data.status;
  }

  get creationTimestamp(): Date {
    return this._data.creationTimestamp;
  }

  get snapshotName(): string {
    return this._data.snapshotName;
  }

  get sandboxId(): string {
    return this._data.sandboxId;
  }

  get containerName(): string | null {
    return this._data.containerName;
  }

  get timeoutSeconds(): number {
    return this._data.timeoutSeconds;
  }

  get ttlSecondsAfterFinished(): number {
    return this._data.ttlSecondsAfterFinished;
  }
}
