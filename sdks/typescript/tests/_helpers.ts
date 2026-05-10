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

// Fetch-injection helpers — the multi-runtime-safe alternative to MSW.
// Mirrors how respx is used in sdks/python/tests/.

export interface RecordedRequest {
  url: string;
  method: string;
  headers: Headers;
  body: Uint8Array | null;
  bodyText: string;
  signal: AbortSignal | undefined;
}

export type Responder = (req: RecordedRequest) => Response | Promise<Response> | Error | Promise<Error>;

export interface StubFetch {
  fetch: typeof fetch;
  calls: RecordedRequest[];
}

// Routing fetch — selects responses by `${METHOD} ${pathname}` matchers,
// since concurrent calls (e.g. inside `Commands.run`'s `Promise.all([...])`)
// have implementation-defined order. Use this when responder selection must
// be order-independent.
export function makeRoutingFetch(routes: Record<string, Responder | Response>): StubFetch {
  const calls: RecordedRequest[] = [];

  const stub = async (input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const method = (init.method ?? "GET").toUpperCase();
    let bodyBytes: Uint8Array | null = null;
    if (init.body !== undefined && init.body !== null) {
      if (typeof init.body === "string") {
        bodyBytes = new TextEncoder().encode(init.body);
      } else if (init.body instanceof Uint8Array) {
        bodyBytes = init.body;
      } else if (init.body instanceof ArrayBuffer) {
        bodyBytes = new Uint8Array(init.body);
      } else if (typeof Blob !== "undefined" && init.body instanceof Blob) {
        bodyBytes = new Uint8Array(await init.body.arrayBuffer());
      } else if (typeof URLSearchParams !== "undefined" && init.body instanceof URLSearchParams) {
        bodyBytes = new TextEncoder().encode(init.body.toString());
      } else if (typeof ReadableStream !== "undefined" && init.body instanceof ReadableStream) {
        const reader = init.body.getReader();
        const chunks: Uint8Array[] = [];
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          if (value) chunks.push(value);
        }
        const total = chunks.reduce((acc, c) => acc + c.length, 0);
        const buf = new Uint8Array(total);
        let off = 0;
        for (const c of chunks) {
          buf.set(c, off);
          off += c.length;
        }
        bodyBytes = buf;
      }
    }
    const bodyText = bodyBytes ? new TextDecoder("utf-8").decode(bodyBytes) : "";
    const recorded: RecordedRequest = {
      url,
      method,
      headers: new Headers(init.headers),
      body: bodyBytes,
      bodyText,
      signal: init.signal ?? undefined,
    };
    calls.push(recorded);

    const pathname = new URL(url).pathname;
    const key = `${method} ${pathname}`;
    const handler = routes[key];
    if (handler === undefined) {
      throw new Error(`no responder configured for ${method} ${pathname}`);
    }
    let result: Response | Error | Promise<Response | Error>;
    if (handler instanceof Response) {
      result = handler.clone();
    } else {
      result = handler(recorded);
    }
    const resolved = await result;
    if (resolved instanceof Error) throw resolved;
    return resolved;
  };

  return { fetch: stub as unknown as typeof fetch, calls };
}

export function makeStubFetch(...responders: Array<Responder | Response | Error>): StubFetch {
  const calls: RecordedRequest[] = [];
  let cursor = 0;

  const stub = async (input: RequestInfo | URL, init: RequestInit = {}): Promise<Response> => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    const method = (init.method ?? "GET").toUpperCase();
    let bodyBytes: Uint8Array | null = null;
    if (init.body !== undefined && init.body !== null) {
      if (typeof init.body === "string") {
        bodyBytes = new TextEncoder().encode(init.body);
      } else if (init.body instanceof Uint8Array) {
        bodyBytes = init.body;
      } else if (init.body instanceof ArrayBuffer) {
        bodyBytes = new Uint8Array(init.body);
      } else if (typeof Blob !== "undefined" && init.body instanceof Blob) {
        bodyBytes = new Uint8Array(await init.body.arrayBuffer());
      } else if (typeof URLSearchParams !== "undefined" && init.body instanceof URLSearchParams) {
        bodyBytes = new TextEncoder().encode(init.body.toString());
      } else if (typeof ReadableStream !== "undefined" && init.body instanceof ReadableStream) {
        const reader = init.body.getReader();
        const chunks: Uint8Array[] = [];
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          if (value) chunks.push(value);
        }
        const total = chunks.reduce((acc, c) => acc + c.length, 0);
        const buf = new Uint8Array(total);
        let off = 0;
        for (const c of chunks) {
          buf.set(c, off);
          off += c.length;
        }
        bodyBytes = buf;
      }
    }
    const bodyText = bodyBytes ? new TextDecoder("utf-8").decode(bodyBytes) : "";
    const recorded: RecordedRequest = {
      url,
      method,
      headers: new Headers(init.headers),
      body: bodyBytes,
      bodyText,
      signal: init.signal ?? undefined,
    };
    calls.push(recorded);

    if (cursor >= responders.length) {
      throw new Error(`unexpected fetch (no responder configured): ${method} ${url}`);
    }
    const handler = responders[cursor++];
    if (handler === undefined) {
      throw new Error(`unexpected fetch (no responder configured): ${method} ${url}`);
    }
    let result: Response | Error | Promise<Response | Error>;
    if (handler instanceof Response) {
      result = handler;
    } else if (handler instanceof Error) {
      result = handler;
    } else {
      result = handler(recorded);
    }
    const resolved = await result;
    if (resolved instanceof Error) throw resolved;
    return resolved;
  };

  return { fetch: stub as unknown as typeof fetch, calls };
}

// Convenience: build an SSE response body with optional id/retry/data fields.
export function sseResponseBody(events: Array<{ id?: string | number | null; data?: string; retry?: number }>): string {
  const parts: string[] = [];
  for (const ev of events) {
    if (ev.data !== undefined) {
      for (const line of ev.data.split("\n")) {
        parts.push(`data: ${line}`);
      }
    }
    if (ev.id !== undefined && ev.id !== null) parts.push(`id: ${ev.id}`);
    if (ev.retry !== undefined) parts.push(`retry: ${ev.retry}`);
    parts.push(""); // blank line dispatches the event
  }
  return `${parts.join("\n")}\n`;
}

export function sseResponse(body: string, init: ResponseInit = {}): Response {
  const headers = new Headers(init.headers);
  if (!headers.has("content-type")) headers.set("content-type", "text/event-stream");
  return new Response(body, { ...init, headers });
}

export function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    ...init,
    headers: { "content-type": "application/json", ...init.headers },
  });
}

export function emptyResponse(status = 204): Response {
  return new Response(null, { status });
}

export function noContentResponse(status = 204): Response {
  return emptyResponse(status);
}

export function networkError(message: string): TypeError {
  return new TypeError(message);
}

// Mirrors sdks/python/tests/conftest.py:sandbox_response.
export function sandboxResponseFixture(): Record<string, unknown> {
  return {
    id: "sandbox-123",
    status: "Running",
    creationTimestamp: "2026-02-18T00:00:00Z",
    podTemplate: {
      containers: [
        {
          name: "sandbox0",
          image: "python:3.12",
          command: ["sleep", "infinity"],
          resources: {
            limits: { cpu: "500m", memory: "1Gi", ephemeralStorage: "2Gi" },
            requests: { cpu: "500m", memory: "1Gi", ephemeralStorage: "2Gi" },
          },
        },
      ],
    },
    network: { allowInternetEgress: true },
    timeoutSeconds: 3600,
    startupTimeoutSeconds: 60,
  };
}

export function sandboxSummaryResponseFixture(): Record<string, unknown> {
  return {
    sandboxes: [
      { id: "sandbox-123", status: "Running", creationTimestamp: "2026-02-18T00:00:00Z" },
      { id: "sandbox-456", status: "Pending", creationTimestamp: "2026-02-18T00:01:00Z" },
    ],
  };
}

export function makeSandboxResponse(status: string, sandboxId = "sandbox-123"): Record<string, unknown> {
  return {
    id: sandboxId,
    status,
    creationTimestamp: "2026-02-18T00:00:00Z",
    podTemplate: { containers: [{ name: "sandbox0", image: "python:3.12" }] },
    startupTimeoutSeconds: 90,
  };
}

export function rootfsSnapshotResponseFixture(): Record<string, unknown> {
  return {
    id: "snapshot-123",
    sandboxId: "sandbox-123",
    snapshotName: "my-snapshot",
    containerName: "worker",
    timeoutSeconds: 300,
    ttlSecondsAfterFinished: 300,
    status: "Succeeded",
    creationTimestamp: "2026-02-18T00:00:00Z",
  };
}

export function makeRootfsSnapshotResponse(status: string, snapshotId = "snapshot-123"): Record<string, unknown> {
  return {
    id: snapshotId,
    sandboxId: "sandbox-123",
    snapshotName: "my-snapshot",
    timeoutSeconds: 300,
    ttlSecondsAfterFinished: 300,
    creationTimestamp: "2026-02-18T00:00:00Z",
    status,
  };
}

export function getSearchParam(url: string, key: string): string | null {
  return new URL(url).searchParams.get(key);
}
