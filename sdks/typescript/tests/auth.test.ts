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

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Isola } from "../src/client";
import { HttpClient } from "../src/internal/http";
import { jsonResponse, makeStubFetch, sseResponse, sseResponseBody } from "./_helpers";

const URL_BASE = "http://localhost:8080";

beforeEach(() => {
  vi.unstubAllEnvs();
});

afterEach(() => {
  vi.unstubAllEnvs();
});

describe("API key authentication", () => {
  it("sends Authorization: Bearer on a JSON request via request()", async () => {
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, apiKey: "secret-key", fetch: stub.fetch });
    await client.sandboxes.list();
    expect(stub.calls[0]!.headers.get("authorization")).toBe("Bearer secret-key");
  });

  it("sends Authorization: Bearer on an octet-stream upload", async () => {
    const stub = makeStubFetch(jsonResponse({ ok: true }));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, apiKey: "secret-key", fetch: stub.fetch });
    await api.request({
      method: "POST",
      path: "/v1/sandboxes/sandbox-123/fs/write",
      body: new TextEncoder().encode("file contents"),
      headers: { "content-type": "application/octet-stream" },
    });
    // Auth header coexists with the upload's content-type (set after opts.headers).
    expect(stub.calls[0]!.headers.get("authorization")).toBe("Bearer secret-key");
    expect(stub.calls[0]!.headers.get("content-type")).toBe("application/octet-stream");
  });

  it("sends Authorization: Bearer on a fetchStream() request", async () => {
    const stub = makeStubFetch(sseResponse(sseResponseBody([{ data: "x", id: 1 }])));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, apiKey: "secret-key", fetch: stub.fetch });
    const response = await api.fetchStream("/v1/sandboxes/sandbox-123/commands/cmd-1/stdout");
    await response.body?.cancel();
    expect(stub.calls[0]!.headers.get("authorization")).toBe("Bearer secret-key");
  });

  it("falls back to the ISOLA_API_KEY env var", async () => {
    vi.stubEnv("ISOLA_API_KEY", "env-key");
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await client.sandboxes.list();
    expect(stub.calls[0]!.headers.get("authorization")).toBe("Bearer env-key");
  });

  it("explicit apiKey overrides the ISOLA_API_KEY env var", async () => {
    vi.stubEnv("ISOLA_API_KEY", "env-key");
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, apiKey: "explicit-key", fetch: stub.fetch });
    await client.sandboxes.list();
    expect(stub.calls[0]!.headers.get("authorization")).toBe("Bearer explicit-key");
  });

  it("sends no auth header on request() when no apiKey or env is set", async () => {
    vi.stubEnv("ISOLA_API_KEY", "");
    const stub = makeStubFetch(jsonResponse({ sandboxes: [] }));
    const client = new Isola({ url: URL_BASE, fetch: stub.fetch });
    await client.sandboxes.list();
    expect(stub.calls[0]!.headers.get("authorization")).toBeNull();
  });

  it("sends no auth header on fetchStream() when no apiKey or env is set", async () => {
    vi.stubEnv("ISOLA_API_KEY", "");
    const stub = makeStubFetch(sseResponse(sseResponseBody([{ data: "x", id: 1 }])));
    const api = new HttpClient({ url: URL_BASE, requestTimeoutMs: null, fetch: stub.fetch });
    const response = await api.fetchStream("/path");
    await response.body?.cancel();
    expect(stub.calls[0]!.headers.get("authorization")).toBeNull();
  });
});
