import { vi } from "vitest";
import type { SandboxData, SandboxSummary } from "../src/models.js";

// ---------------------------------------------------------------------------
// Mock fetch helper
// ---------------------------------------------------------------------------

export const mockFetch = vi.fn<(input: string | URL | Request, init?: RequestInit) => Promise<Response>>();

export function installMockFetch(): void {
  vi.stubGlobal("fetch", mockFetch);
}

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

export function noContentResponse(): Response {
  return new Response(null, { status: 204 });
}

export function errorResponse(
  status: number,
  detail: string,
): Response {
  return new Response(JSON.stringify({ detail }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

export function sseResponse(body: string): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode(body));
      controller.close();
    },
  });
  return new Response(stream, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });
}

export function sseResponseChunked(chunks: string[]): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk));
      }
      controller.close();
    },
  });
  return new Response(stream, {
    status: 200,
    headers: { "Content-Type": "text/event-stream" },
  });
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

export const SANDBOX_DATA: SandboxData = {
  id: "sbx-test-123",
  podTemplate: {
    container: {
      image: "ubuntu:24.04",
      command: ["sleep", "infinity"],
      resources: {
        limits: { cpu: "1", memory: "512Mi" },
        requests: { cpu: "1", memory: "512Mi" },
      },
    },
  },
  status: "running",
  creationTimestamp: "2025-01-01T00:00:00Z",
  activeDeadlineSeconds: 3600,
  network: {
    allowInternetEgress: false,
    allowClusterDNS: true,
  },
};

export const SANDBOX_SUMMARIES: SandboxSummary[] = [
  { id: "sbx-1", status: "running", creationTimestamp: "2025-01-01T00:00:00Z" },
  { id: "sbx-2", status: "creating", creationTimestamp: "2025-01-01T00:01:00Z" },
];
