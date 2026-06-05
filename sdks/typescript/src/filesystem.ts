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
import type { HttpClient } from "./internal/http";
import { filesystemPath } from "./internal/url";

/** Options for {@link Filesystem.read} and {@link Filesystem.write}. */
export interface FileOptions {
  /**
   * Target container name. Only needed for multi-container sandboxes.
   */
  container?: string;
}

/**
 * Body types accepted by {@link Filesystem.write}.
 *
 * `string` is encoded as UTF-8. A `ReadableStream` body is sent once;
 * unlike the in-memory body types it is not retried on transport errors.
 */
export type UploadBody = string | Uint8Array | ArrayBuffer | Blob | ReadableStream<Uint8Array>;

/** Read and write files inside a sandbox. */
export class Filesystem {
  /** @internal */
  readonly _api: HttpClient;
  /** @internal */
  readonly _sandboxId: string;

  /** @internal */
  constructor(api: HttpClient, sandboxId: string) {
    this._api = api;
    this._sandboxId = sandboxId;
  }

  /**
   * Write a file to the sandbox.
   *
   * Creates the file if it does not exist, overwrites it if it does.
   * Parent directories are created automatically.
   *
   * @example
   * ```ts
   * await sandbox.filesystem.write("/tmp/hello.txt", "hi");
   * ```
   *
   * @param path - Absolute path inside the sandbox (e.g.
   * `"/tmp/hello.txt"`).
   * @param data - Content to write. Strings are encoded as UTF-8.
   * Pass binary data as `Uint8Array`, `ArrayBuffer`, `Blob`, or a
   * `ReadableStream<Uint8Array>` (streams are non-replayable; see
   * {@link UploadBody}).
   * @param opts - File options (e.g. `container` for multi-container
   * sandboxes).
   * @throws {APIError} If the API returns a non-2xx response.
   * @throws {APIConnectionError} If the request cannot reach the API.
   */
  async write(path: string, data: UploadBody, opts: FileOptions = {}, req: RequestOptions = {}): Promise<void> {
    const body = typeof data === "string" ? new TextEncoder().encode(data) : data;
    const params: Record<string, string> = { path };
    if (opts.container) params.container = opts.container;

    await this._api.requestNoContent({
      method: "POST",
      path: filesystemPath(this._sandboxId),
      params,
      body,
      headers: { "content-type": "application/octet-stream" },
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }

  /**
   * Read a file from the sandbox.
   *
   * @example
   * ```ts
   * const bytes = await sandbox.filesystem.read("/tmp/hello.txt");
   * const text = new TextDecoder().decode(bytes);
   * ```
   *
   * @param path - Absolute path inside the sandbox.
   * @param opts - File options (e.g. `container` for multi-container
   * sandboxes).
   * @returns File contents as bytes. Decode with `TextDecoder` for
   * text.
   * @throws {NotFoundError} If the file does not exist.
   * @throws {APIError} If the API returns a non-2xx response.
   * @throws {APIConnectionError} If the request cannot reach the API.
   */
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
