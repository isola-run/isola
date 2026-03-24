import type { APIClient } from "./client.js";
import type { FileWriteResult } from "./models.js";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function filesystemPath(sandboxId: string): string {
  return `/v1/sandboxes/${encodeURIComponent(sandboxId)}/filesystem`;
}

// ---------------------------------------------------------------------------
// Filesystem
// ---------------------------------------------------------------------------

/**
 * File I/O operations on a sandbox's filesystem.
 *
 * Obtained via {@link Sandbox.filesystem}.
 */
export class Filesystem {
  /** @internal */
  constructor(
    private readonly api: APIClient,
    private readonly sandboxId: string,
  ) {}

  /**
   * Upload a file into the sandbox.
   *
   * @param path     - Absolute path inside the sandbox.
   * @param data     - File content. Strings are UTF-8 encoded.
   * @param options  - Optional container name for multi-container sandboxes.
   */
  async write(
    path: string,
    data: string | Uint8Array | Blob | ReadableStream<Uint8Array>,
    options?: { container?: string },
  ): Promise<FileWriteResult> {
    const params: Record<string, string> = { path };
    if (options?.container) {
      params.container = options.container;
    }

    const content =
      typeof data === "string" ? new TextEncoder().encode(data) : data;

    return this.api.request<FileWriteResult>("POST", filesystemPath(this.sandboxId), {
      content,
      headers: { "Content-Type": "application/octet-stream" },
      params,
    });
  }

  /**
   * Download a file from the sandbox.
   *
   * @param path     - Absolute path inside the sandbox.
   * @param options  - Optional container name for multi-container sandboxes.
   * @returns Raw file bytes.
   */
  async read(
    path: string,
    options?: { container?: string },
  ): Promise<Uint8Array> {
    const params: Record<string, string> = { path };
    if (options?.container) {
      params.container = options.container;
    }

    return this.api.requestBytes("GET", filesystemPath(this.sandboxId), {
      params,
    });
  }
}
