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

/** Options for the {@link Filesystem} read and write methods. */
export interface FileOptions {
  /**
   * Target container name. Only needed for multi-container sandboxes.
   */
  container?: string;
}

/**
 * Binary body types accepted by {@link Filesystem.writeBytes}.
 *
 * A `ReadableStream` body is sent once. Unlike the in-memory body types, it is
 * not retried on transport errors.
 */
export type BinaryBody = Uint8Array | ArrayBuffer | Blob | ReadableStream<Uint8Array>;

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
   * Read a file from the sandbox as UTF-8 text.
   *
   * For binary or non-UTF-8 content, use {@link Filesystem.readBytes}
   * instead.
   *
   * @example
   * ```ts
   * const text = await sandbox.filesystem.readText("/tmp/hello.txt");
   * ```
   *
   * @param path - Absolute path inside the sandbox.
   * @param opts - File options (e.g. `container` for multi-container
   * sandboxes).
   * @returns The file's contents decoded as UTF-8.
   * @throws {NotFoundError} If the file does not exist.
   * @throws {TypeError} If the contents are not valid UTF-8.
   * @throws {APIError} If the API returns a non-2xx response.
   * @throws {APIConnectionError} If the request cannot reach the API.
   */
  async readText(path: string, opts: FileOptions = {}, req: RequestOptions = {}): Promise<string> {
    // fatal: true so non-UTF-8 content throws instead of silently decoding to
    // replacement characters, matching the strict behavior of the Python SDK's
    // bytes.decode().
    return new TextDecoder("utf-8", { fatal: true }).decode(await this.readBytes(path, opts, req));
  }

  /**
   * Read a file from the sandbox as raw bytes.
   *
   * @example
   * ```ts
   * const bytes = await sandbox.filesystem.readBytes("/tmp/data.bin");
   * ```
   *
   * @param path - Absolute path inside the sandbox.
   * @param opts - File options (e.g. `container` for multi-container
   * sandboxes).
   * @returns The file's contents as bytes.
   * @throws {NotFoundError} If the file does not exist.
   * @throws {APIError} If the API returns a non-2xx response.
   * @throws {APIConnectionError} If the request cannot reach the API.
   */
  async readBytes(path: string, opts: FileOptions = {}, req: RequestOptions = {}): Promise<Uint8Array> {
    const params: Record<string, string> = { path };
    if (opts.container) params.container = opts.container;

    return this._api.requestBytes({
      method: "GET",
      path: filesystemPath(this._sandboxId),
      params,
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }

  /**
   * Write UTF-8 text to a file in the sandbox.
   *
   * Creates the file if it does not exist, overwrites it if it does.
   * Parent directories are created automatically.
   *
   * @example
   * ```ts
   * await sandbox.filesystem.writeText("/tmp/hello.txt", "hi");
   * ```
   *
   * @param path - Absolute path inside the sandbox (e.g.
   * `"/tmp/hello.txt"`).
   * @param data - Text to write, encoded as UTF-8.
   * @param opts - File options (e.g. `container` for multi-container
   * sandboxes).
   * @throws {APIError} If the API returns a non-2xx response.
   * @throws {APIConnectionError} If the request cannot reach the API.
   */
  async writeText(path: string, data: string, opts: FileOptions = {}, req: RequestOptions = {}): Promise<void> {
    await this.writeBytes(path, new TextEncoder().encode(data), opts, req);
  }

  /**
   * Write raw bytes to a file in the sandbox.
   *
   * Creates the file if it does not exist, overwrites it if it does.
   * Parent directories are created automatically.
   *
   * @example
   * ```ts
   * await sandbox.filesystem.writeBytes("/tmp/data.bin", new Uint8Array([0, 1, 2]));
   * ```
   *
   * @param path - Absolute path inside the sandbox.
   * @param data - Binary content as `Uint8Array`, `ArrayBuffer`, `Blob`, or a
   * non-replayable `ReadableStream<Uint8Array>` (see {@link BinaryBody}).
   * @param opts - File options (e.g. `container` for multi-container
   * sandboxes).
   * @throws {APIError} If the API returns a non-2xx response.
   * @throws {APIConnectionError} If the request cannot reach the API.
   */
  async writeBytes(path: string, data: BinaryBody, opts: FileOptions = {}, req: RequestOptions = {}): Promise<void> {
    const params: Record<string, string> = { path };
    if (opts.container) params.container = opts.container;

    await this._api.requestNoContent({
      method: "POST",
      path: filesystemPath(this._sandboxId),
      params,
      body: data,
      headers: { "content-type": "application/octet-stream" },
      ...(req.signal ? { signal: req.signal } : {}),
    });
  }
}
