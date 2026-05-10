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

// Mirrors sdks/python/src/isola/_filesystem.py:AsyncFilesystem.

import type { RequestOptions } from "./client";
import type { BodyKind, HttpClient } from "./internal/http";
import { filesystemPath } from "./internal/url";

export interface FileOptions {
  container?: string;
}

// ReadableStream uploads require duplex: "half" in Node fetch.
// Streams are non-replayable: transport errors on stream bodies are NOT retried.
// Replayable: string | Uint8Array | ArrayBuffer | Blob.
export type UploadBody = string | Uint8Array | ArrayBuffer | Blob | ReadableStream<Uint8Array>;

function classify(data: UploadBody): { body: BodyInit; bodyKind: BodyKind } {
  if (typeof data === "string") {
    return { body: new TextEncoder().encode(data), bodyKind: "replayable" };
  }
  if (typeof ReadableStream !== "undefined" && data instanceof ReadableStream) {
    return { body: data, bodyKind: "stream" };
  }
  // Uint8Array | ArrayBuffer | Blob
  return { body: data as BodyInit, bodyKind: "replayable" };
}

export class Filesystem {
  /** @internal */
  readonly _api: HttpClient;
  /** @internal */
  readonly _sandboxId: string;

  constructor(api: HttpClient, sandboxId: string) {
    this._api = api;
    this._sandboxId = sandboxId;
  }

  async write(path: string, data: UploadBody, opts: FileOptions = {}, req: RequestOptions = {}): Promise<void> {
    const { body, bodyKind } = classify(data);
    const params: Record<string, string> = { path };
    if (opts.container) params.container = opts.container;

    await this._api.requestNoContent({
      method: "POST",
      path: filesystemPath(this._sandboxId),
      params,
      body,
      bodyKind,
      headers: { "content-type": "application/octet-stream" },
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }

  async read(path: string, opts: FileOptions = {}, req: RequestOptions = {}): Promise<Uint8Array> {
    const params: Record<string, string> = { path };
    if (opts.container) params.container = opts.container;

    return this._api.requestBytes({
      method: "GET",
      path: filesystemPath(this._sandboxId),
      params,
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }
}
