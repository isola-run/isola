import { describe, it, expect, beforeEach } from "vitest";
import { Isola } from "../src/isola.js";
import type { SandboxData } from "../src/models.js";
import {
  mockFetch,
  installMockFetch,
  jsonResponse,
  noContentResponse,
  SANDBOX_DATA,
  SANDBOX_SUMMARIES,
} from "./helpers.js";

installMockFetch();

beforeEach(() => {
  mockFetch.mockReset();
});

const client = new Isola({ baseURL: "http://localhost:8080" });

describe("Sandboxes.create", () => {
  it("sends correct payload with resources", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse(SANDBOX_DATA));

    const sandbox = await client.sandboxes.create({
      image: "ubuntu:24.04",
      command: ["bash"],
      env: { FOO: "bar" },
      cpu: "1",
      memory: "512Mi",
      ephemeralStorage: "1Gi",
      timeout: 3600,
      network: { allowInternetEgress: false, allowClusterDNS: true },
    });

    expect(sandbox.id).toBe("sbx-test-123");
    expect(sandbox.status).toBe("running");

    const [url, init] = mockFetch.mock.calls[0]!;
    expect(url).toBe("http://localhost:8080/v1/sandboxes");
    expect(init?.method).toBe("POST");

    const body = JSON.parse(init?.body as string) as Record<string, unknown>;
    expect(body).toEqual({
      podTemplate: {
        container: {
          image: "ubuntu:24.04",
          command: ["bash"],
          env: { FOO: "bar" },
          resources: {
            limits: { cpu: "1", memory: "512Mi", ephemeralStorage: "1Gi" },
            requests: { cpu: "1", memory: "512Mi", ephemeralStorage: "1Gi" },
          },
        },
      },
      activeDeadlineSeconds: 3600,
      network: { allowInternetEgress: false, allowClusterDNS: true },
    });
  });

  it("omits optional fields when not provided", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse(SANDBOX_DATA));

    await client.sandboxes.create({ image: "alpine" });

    const body = JSON.parse(
      mockFetch.mock.calls[0]![1]?.body as string,
    ) as Record<string, unknown>;

    expect(body).toEqual({
      podTemplate: { container: { image: "alpine" } },
    });
  });

  it("serializes rootfsSnapshotSource as array", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse(SANDBOX_DATA));

    await client.sandboxes.create({
      image: "alpine",
      rootfsSnapshotSource: "my-snapshot",
    });

    const body = JSON.parse(
      mockFetch.mock.calls[0]![1]?.body as string,
    ) as Record<string, unknown>;

    expect(body).toEqual({
      podTemplate: { container: { image: "alpine" } },
      rootfsSnapshotSources: [{ snapshotName: "my-snapshot" }],
    });
  });
});

describe("Sandboxes.list", () => {
  it("returns sandbox summaries", async () => {
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ sandboxes: SANDBOX_SUMMARIES }),
    );

    const list = await client.sandboxes.list();
    expect(list).toHaveLength(2);
    expect(list[0]!.id).toBe("sbx-1");
    expect(list[1]!.status).toBe("creating");
  });

  it("returns empty array when sandboxes is null", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({ sandboxes: null }));

    const list = await client.sandboxes.list();
    expect(list).toEqual([]);
  });
});

describe("Sandboxes.get", () => {
  it("returns a Sandbox instance", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse(SANDBOX_DATA));

    const sandbox = await client.sandboxes.get("sbx-test-123");
    expect(sandbox.id).toBe("sbx-test-123");
    expect(sandbox.status).toBe("running");
    expect(sandbox.creationTimestamp).toBe("2025-01-01T00:00:00Z");
    expect(sandbox.timeout).toBe(3600);
    expect(sandbox.network).toEqual({
      allowInternetEgress: false,
      allowClusterDNS: true,
    });
  });
});

describe("Sandbox.delete", () => {
  it("sends DELETE request", async () => {
    mockFetch
      .mockResolvedValueOnce(jsonResponse(SANDBOX_DATA))
      .mockResolvedValueOnce(noContentResponse());

    const sandbox = await client.sandboxes.get("sbx-test-123");
    await sandbox.delete();

    const [url, init] = mockFetch.mock.calls[1]!;
    expect(url).toBe("http://localhost:8080/v1/sandboxes/sbx-test-123");
    expect(init?.method).toBe("DELETE");
  });
});

describe("Sandbox properties", () => {
  it("exposes commands and filesystem sub-resources", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse(SANDBOX_DATA));

    const sandbox = await client.sandboxes.get("sbx-test-123");
    expect(sandbox.commands).toBeDefined();
    expect(sandbox.filesystem).toBeDefined();
  });

  it("exposes rootfsSnapshotSources when present", async () => {
    const dataWithSnapshots: SandboxData = {
      ...SANDBOX_DATA,
      rootfsSnapshotSources: [{ snapshotName: "snap1" }],
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(dataWithSnapshots));

    const sandbox = await client.sandboxes.get("sbx-test-123");
    expect(sandbox.rootfsSnapshotSources).toEqual([
      { snapshotName: "snap1" },
    ]);
  });
});

describe("NetworkSpec acronym aliases", () => {
  it("handles allowClusterDNS and allowedEgressCIDRs round-trip", async () => {
    const data: SandboxData = {
      ...SANDBOX_DATA,
      network: {
        allowInternetEgress: true,
        allowClusterDNS: true,
        allowedEgressCIDRs: ["10.0.0.0/8"],
        nameservers: ["8.8.8.8"],
      },
    };
    mockFetch.mockResolvedValueOnce(jsonResponse(data));

    const sandbox = await client.sandboxes.get("sbx-test-123");
    expect(sandbox.network?.allowClusterDNS).toBe(true);
    expect(sandbox.network?.allowedEgressCIDRs).toEqual(["10.0.0.0/8"]);
    expect(sandbox.network?.nameservers).toEqual(["8.8.8.8"]);
  });
});
