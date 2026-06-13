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

// Mirrors sdks/python/tests/test_filesystem.py, same scenarios, adapted to
// the TypeScript SDK's UploadBody types (Uint8Array, ArrayBuffer, Blob,
// ReadableStream, string).

import { describe, expect, it } from "vitest";
import { Isola } from "../src/client";
import { InternalError, NotFoundError } from "../src/errors";
import { MAX_RETRIES } from "../src/internal/http";
import { emptyResponse, getSearchParam, jsonResponse, makeStubFetch, sandboxResponseFixture } from "./_helpers";

const URL_BASE = "http://localhost:8080";

function bytesResponse(body: Uint8Array, init: ResponseInit = {}): Response {
  // Strict TS types reject `new Response(uint8array<ArrayBufferLike>, ...)`
  // since lib.dom expects Uint8Array<ArrayBuffer>. Pass through the underlying
  // buffer to satisfy BodyInit.
  const buf = body.buffer.slice(body.byteOffset, body.byteOffset + body.byteLength) as ArrayBuffer;
  return new Response(buf, {
    ...init,
    headers: { "content-type": "application/octet-stream", ...init.headers },
  });
}

describe("Filesystem.write/read", () => {
  it("write(Uint8Array) with container then read", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      emptyResponse(204),
      bytesResponse(new TextEncoder().encode("content")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    await sandbox.filesystem.write("/workspace/file.txt", new TextEncoder().encode("content"), {
      container: "worker",
    });
    const downloaded = await sandbox.filesystem.read("/workspace/file.txt", { container: "worker" });
    expect(new TextDecoder().decode(downloaded)).toBe("content");

    expect(stub.calls).toHaveLength(3);
    const writeCall = stub.calls[1]!;
    expect(writeCall.method).toBe("POST");
    expect(writeCall.url.startsWith(`${URL_BASE}/v1/sandboxes/sandbox-123/filesystem`)).toBe(true);
    expect(getSearchParam(writeCall.url, "path")).toBe("/workspace/file.txt");
    expect(getSearchParam(writeCall.url, "container")).toBe("worker");
    expect(writeCall.headers.get("content-type")).toBe("application/octet-stream");
    expect(writeCall.bodyText).toBe("content");

    const readCall = stub.calls[2]!;
    expect(readCall.method).toBe("GET");
    expect(getSearchParam(readCall.url, "path")).toBe("/workspace/file.txt");
    expect(getSearchParam(readCall.url, "container")).toBe("worker");
  });

  it("write(string) is encoded as UTF-8 bytes", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), emptyResponse(204));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    await sandbox.filesystem.write("/workspace/hello.py", "print('hello')");

    const writeCall = stub.calls[1]!;
    expect(writeCall.bodyText).toBe("print('hello')");
    // Payload bytes should be the UTF-8 encoding of the string.
    expect(writeCall.body).toEqual(new TextEncoder().encode("print('hello')"));
  });

  it("write(Blob) sends the bytes inside the blob", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), emptyResponse(204));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const blob = new Blob([new TextEncoder().encode("blob-bytes")]);
    await sandbox.filesystem.write("/workspace/blob.bin", blob);

    const writeCall = stub.calls[1]!;
    expect(writeCall.bodyText).toBe("blob-bytes");
  });

  it("write(ArrayBuffer) sends the buffer's bytes", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), emptyResponse(204));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const buf = new TextEncoder().encode("buffer-bytes").buffer;
    await sandbox.filesystem.write("/workspace/ab.bin", buf);

    const writeCall = stub.calls[1]!;
    expect(writeCall.bodyText).toBe("buffer-bytes");
  });

  it("write(ReadableStream) consumes the stream and uses duplex: 'half'", async () => {
    let capturedDuplex: string | undefined;
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      // Wrap with a custom responder so we can inspect the init's duplex value.
      // makeStubFetch already records the bytes of the stream body.
      emptyResponse(204),
    );
    // We need to peek at the original init. Re-wrap stub.fetch to record duplex.
    const originalFetch = stub.fetch;
    const wrappedFetch: typeof fetch = async (input, init) => {
      const initWithDuplex = init as (RequestInit & { duplex?: string }) | undefined;
      if (initWithDuplex?.duplex !== undefined) {
        capturedDuplex = initWithDuplex.duplex;
      }
      return originalFetch(input, init);
    };
    const client = new Isola({ url: URL_BASE, fetch: wrappedFetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("stream-"));
        controller.enqueue(new TextEncoder().encode("bytes"));
        controller.close();
      },
    });
    await sandbox.filesystem.write("/workspace/stream.bin", stream);

    const writeCall = stub.calls[1]!;
    expect(writeCall.bodyText).toBe("stream-bytes");
    expect(capturedDuplex).toBe("half");
  });

  it("write(ReadableStream) does NOT retry on transient errors (non-replayable)", async () => {
    // First call: GET sandbox. Second call: transient network error mid-stream.
    // After 1 attempt, the request must throw (no retries allowed for streams).
    const responders: Array<Response | TypeError> = [
      jsonResponse(sandboxResponseFixture()),
      new TypeError("connect failed"),
    ];
    // If retries WERE happening despite the stream classification, the SDK
    // would request 1 + MAX_RETRIES additional fetches. Pre-load extra
    // failures so we can assert they were never consumed.
    for (let i = 0; i < MAX_RETRIES; i++) {
      responders.push(new TypeError("connect failed"));
    }
    const stub = makeStubFetch(...responders);
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode("payload"));
        controller.close();
      },
    });

    await expect(sandbox.filesystem.write("/workspace/stream.bin", stream)).rejects.toThrow();

    // Exactly 2 fetches: sandbox GET + 1 write attempt.
    expect(stub.calls).toHaveLength(2);
  }, 15_000);

  it("omits container query param when not provided", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      emptyResponse(204),
      bytesResponse(new TextEncoder().encode("data")),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    await sandbox.filesystem.write("/tmp/data.bin", new TextEncoder().encode("data"));
    await sandbox.filesystem.read("/tmp/data.bin");

    const writeCall = stub.calls[1]!;
    expect(getSearchParam(writeCall.url, "path")).toBe("/tmp/data.bin");
    expect(getSearchParam(writeCall.url, "container")).toBeNull();

    const readCall = stub.calls[2]!;
    expect(getSearchParam(readCall.url, "path")).toBe("/tmp/data.bin");
    expect(getSearchParam(readCall.url, "container")).toBeNull();
  });
});

describe("Filesystem error handling", () => {
  it("read() raises NotFoundError on 404", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "file not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    let caught: unknown;
    try {
      await sandbox.filesystem.read("/nonexistent/file.txt");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(NotFoundError);
    expect((caught as NotFoundError).statusCode).toBe(404);
    expect((caught as NotFoundError).message).toContain("file not found");
  });

  it("read() raises InternalError on 500", async () => {
    // 500 is NOT transient, single attempt, no retries.
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "internal server error" }, { status: 500 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    let caught: unknown;
    try {
      await sandbox.filesystem.read("/some/file.txt");
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(InternalError);
    expect((caught as InternalError).statusCode).toBe(500);
  });

  it("write() raises NotFoundError on 404", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "sandbox not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await expect(
      sandbox.filesystem.write("/workspace/file.txt", new TextEncoder().encode("data")),
    ).rejects.toBeInstanceOf(NotFoundError);
  });

  it("write() raises InternalError on 500", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "disk full" }, { status: 500 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    let caught: unknown;
    try {
      await sandbox.filesystem.write("/workspace/file.txt", new TextEncoder().encode("data"));
    } catch (err) {
      caught = err;
    }
    expect(caught).toBeInstanceOf(InternalError);
    expect((caught as InternalError).statusCode).toBe(500);
    expect((caught as InternalError).message).toContain("disk full");
  });
});

describe("Filesystem signal propagation", () => {
  it("write() forwards req.signal to the underlying request", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), emptyResponse(204));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await sandbox.filesystem.write(
      "/workspace/file.txt",
      new TextEncoder().encode("data"),
      {},
      { signal: ctrl.signal },
    );

    // The write call must have received an AbortSignal.
    expect(stub.calls[1]?.signal).toBeDefined();
  });

  it("read() forwards req.signal to the underlying request", async () => {
    const ctrl = new AbortController();
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), bytesResponse(new TextEncoder().encode("x")));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await sandbox.filesystem.read("/workspace/file.txt", {}, { signal: ctrl.signal });

    expect(stub.calls[1]?.signal).toBeDefined();
  });
});

const ENTRY_JSON = {
  name: "file.txt",
  path: "/workspace/file.txt",
  type: "file",
  size: 5,
  permissions: "0644",
  uid: 1000,
  gid: 1000,
  modifiedTime: "2026-06-13T00:00:00Z",
};

const SYMLINK_JSON = {
  name: "link",
  path: "/workspace/link",
  type: "symlink",
  size: 10,
  permissions: "0777",
  uid: 0,
  gid: 0,
  modifiedTime: "2026-06-13T00:00:00Z",
  symlinkTarget: "/workspace/file.txt",
};

describe("Filesystem.list", () => {
  it("lists entries with container and parses metadata", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ entries: [ENTRY_JSON, SYMLINK_JSON] }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const entries = await sandbox.filesystem.list("/workspace", { container: "worker" });

    const listCall = stub.calls[1]!;
    expect(listCall.method).toBe("GET");
    expect(listCall.url.startsWith(`${URL_BASE}/v1/sandboxes/sandbox-123/filesystem/entries`)).toBe(true);
    expect(getSearchParam(listCall.url, "path")).toBe("/workspace");
    expect(getSearchParam(listCall.url, "container")).toBe("worker");

    expect(entries).toHaveLength(2);
    expect(entries[0]!.name).toBe("file.txt");
    expect(entries[0]!.type).toBe("file");
    expect(entries[0]!.size).toBe(5);
    expect(entries[0]!.permissions).toBe("0644");
    expect(entries[0]!.uid).toBe(1000);
    expect(entries[0]!.gid).toBe(1000);
    expect(entries[0]!.modifiedTime).toBeInstanceOf(Date);
    expect(entries[0]!.modifiedTime.toISOString()).toBe("2026-06-13T00:00:00.000Z");
    expect(entries[0]!.symlinkTarget).toBeUndefined();
    expect(entries[1]!.type).toBe("symlink");
    expect(entries[1]!.symlinkTarget).toBe("/workspace/file.txt");
  });

  it("returns empty array when entries is missing", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), jsonResponse({}));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const entries = await sandbox.filesystem.list("/empty");

    expect(entries).toEqual([]);
    expect(getSearchParam(stub.calls[1]!.url, "container")).toBeNull();
  });

  it("raises NotFoundError on 404", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "directory not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await expect(sandbox.filesystem.list("/no-such")).rejects.toBeInstanceOf(NotFoundError);
  });
});

describe("Filesystem.stat/exists", () => {
  it("stat() parses a symlink entry", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), jsonResponse(SYMLINK_JSON));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");
    const entry = await sandbox.filesystem.stat("/workspace/link", { container: "worker" });

    const statCall = stub.calls[1]!;
    expect(statCall.url.startsWith(`${URL_BASE}/v1/sandboxes/sandbox-123/filesystem/stat`)).toBe(true);
    expect(getSearchParam(statCall.url, "path")).toBe("/workspace/link");
    expect(getSearchParam(statCall.url, "container")).toBe("worker");

    expect(entry.type).toBe("symlink");
    expect(entry.symlinkTarget).toBe("/workspace/file.txt");
  });

  it("exists() returns true when stat succeeds", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), jsonResponse(ENTRY_JSON));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await expect(sandbox.filesystem.exists("/workspace/file.txt")).resolves.toBe(true);
  });

  it("exists() returns false on 404", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ detail: "path not found" }, { status: 404 }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await expect(sandbox.filesystem.exists("/workspace/missing.txt")).resolves.toBe(false);
  });

  it("exists() rethrows non-404 errors", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      ...Array.from({ length: 1 + MAX_RETRIES }, () => jsonResponse({ detail: "boom" }, { status: 500 })),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await expect(sandbox.filesystem.exists("/workspace/file.txt")).rejects.toBeInstanceOf(InternalError);
  });
});

describe("FilesystemEntry wire decoding", () => {
  it("rejects a missing name", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ ...ENTRY_JSON, name: undefined }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await expect(sandbox.filesystem.stat("/workspace/file.txt")).rejects.toThrow(/invalid response payload/);
  });

  it("rejects a missing path", async () => {
    const stub = makeStubFetch(
      jsonResponse(sandboxResponseFixture()),
      jsonResponse({ ...ENTRY_JSON, path: undefined }),
    );
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await expect(sandbox.filesystem.stat("/workspace/file.txt")).rejects.toThrow(/invalid response payload/);
  });

  it("rejects a non-array entries field", async () => {
    const stub = makeStubFetch(jsonResponse(sandboxResponseFixture()), jsonResponse({ entries: "nope" }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch, requestTimeoutMs: null });
    const sandbox = await client.sandboxes.get("sandbox-123");

    await expect(sandbox.filesystem.list("/workspace")).rejects.toThrow(/invalid response payload/);
  });
});
