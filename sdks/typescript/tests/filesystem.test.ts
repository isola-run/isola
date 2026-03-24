import { describe, it, expect, beforeEach } from "vitest";
import { Isola } from "../src/isola.js";
import type { FileWriteResult } from "../src/models.js";
import {
  mockFetch,
  installMockFetch,
  jsonResponse,
  SANDBOX_DATA,
} from "./helpers.js";

installMockFetch();

beforeEach(() => {
  mockFetch.mockReset();
});

const client = new Isola({ baseURL: "http://localhost:8080" });

async function getSandbox() {
  mockFetch.mockResolvedValueOnce(jsonResponse(SANDBOX_DATA));
  return client.sandboxes.get("sbx-test-123");
}

describe("Filesystem.write", () => {
  it("uploads string data", async () => {
    const sandbox = await getSandbox();

    const writeResult: FileWriteResult = {
      absolutePath: "/tmp/hello.txt",
      bytesWritten: 13,
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(writeResult));

    const result = await sandbox.filesystem.write(
      "/tmp/hello.txt",
      "hello, world!",
    );

    expect(result.absolutePath).toBe("/tmp/hello.txt");
    expect(result.bytesWritten).toBe(13);

    const [url, init] = mockFetch.mock.calls[1]!;
    expect(url).toContain("/filesystem");
    expect(url).toContain("path=%2Ftmp%2Fhello.txt");
    expect(init?.method).toBe("POST");
    expect(init?.headers).toEqual(
      expect.objectContaining({ "Content-Type": "application/octet-stream" }),
    );
  });

  it("uploads Uint8Array data", async () => {
    const sandbox = await getSandbox();

    const writeResult: FileWriteResult = {
      absolutePath: "/tmp/data.bin",
      bytesWritten: 4,
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(writeResult));

    const bytes = new Uint8Array([0x00, 0x01, 0x02, 0x03]);
    const result = await sandbox.filesystem.write("/tmp/data.bin", bytes);

    expect(result.bytesWritten).toBe(4);

    const init = mockFetch.mock.calls[1]![1]!;
    expect(init.body).toBeInstanceOf(Uint8Array);
  });

  it("sends container as query param", async () => {
    const sandbox = await getSandbox();

    const writeResult: FileWriteResult = {
      absolutePath: "/tmp/test",
      bytesWritten: 5,
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(writeResult));

    await sandbox.filesystem.write("/tmp/test", "hello", {
      container: "sidecar",
    });

    const [url] = mockFetch.mock.calls[1]!;
    expect(url).toContain("container=sidecar");
  });
});

describe("Filesystem.read", () => {
  it("downloads file as bytes", async () => {
    const sandbox = await getSandbox();

    const data = new TextEncoder().encode("file contents");
    mockFetch.mockResolvedValueOnce(new Response(data, { status: 200 }));

    const result = await sandbox.filesystem.read("/tmp/hello.txt");

    expect(result).toBeInstanceOf(Uint8Array);
    expect(new TextDecoder().decode(result)).toBe("file contents");

    const [url] = mockFetch.mock.calls[1]!;
    expect(url).toContain("/filesystem");
    expect(url).toContain("path=%2Ftmp%2Fhello.txt");
  });

  it("sends container as query param", async () => {
    const sandbox = await getSandbox();

    mockFetch.mockResolvedValueOnce(
      new Response(new Uint8Array(), { status: 200 }),
    );

    await sandbox.filesystem.read("/tmp/test", { container: "main" });

    const [url] = mockFetch.mock.calls[1]!;
    expect(url).toContain("container=main");
  });
});
