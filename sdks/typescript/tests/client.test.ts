import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { APIClient } from "../src/client.js";
import { Isola } from "../src/isola.js";
import {
  APIConnectionError,
  BadRequestError,
  BadGatewayError,
} from "../src/errors.js";
import {
  mockFetch,
  installMockFetch,
  jsonResponse,
  noContentResponse,
  errorResponse,
} from "./helpers.js";

installMockFetch();

beforeEach(() => {
  mockFetch.mockReset();
});

// ---------------------------------------------------------------------------
// Isola client
// ---------------------------------------------------------------------------

describe("Isola", () => {
  it("creates from explicit baseURL", () => {
    const client = new Isola({ baseURL: "http://localhost:8080" });
    expect(client.sandboxes).toBeDefined();
    client.close();
  });

  it("creates from ISOLA_BASE_URL env var", () => {
    const original = process.env.ISOLA_BASE_URL;
    try {
      process.env.ISOLA_BASE_URL = "http://env-url:8080";
      const client = new Isola();
      expect(client.sandboxes).toBeDefined();
      client.close();
    } finally {
      if (original !== undefined) {
        process.env.ISOLA_BASE_URL = original;
      } else {
        delete process.env.ISOLA_BASE_URL;
      }
    }
  });

  it("throws when no baseURL is available", () => {
    const original = process.env.ISOLA_BASE_URL;
    try {
      delete process.env.ISOLA_BASE_URL;
      expect(() => new Isola()).toThrow("baseURL");
    } finally {
      if (original !== undefined) {
        process.env.ISOLA_BASE_URL = original;
      }
    }
  });
});

// ---------------------------------------------------------------------------
// APIClient — request
// ---------------------------------------------------------------------------

describe("APIClient.request", () => {
  const api = new APIClient({ baseURL: "http://localhost:8080" });

  it("sends JSON body and parses response", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({ id: "123" }));

    const result = await api.request<{ id: string }>("POST", "/v1/test", {
      body: { name: "foo" },
    });

    expect(result).toEqual({ id: "123" });
    expect(mockFetch).toHaveBeenCalledOnce();

    const [url, init] = mockFetch.mock.calls[0]!;
    expect(url).toBe("http://localhost:8080/v1/test");
    expect(init?.method).toBe("POST");
    expect(init?.headers).toEqual(
      expect.objectContaining({ "Content-Type": "application/json" }),
    );
    expect(init?.body).toBe(JSON.stringify({ name: "foo" }));
  });

  it("appends query params", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({}));

    await api.request("GET", "/v1/test", {
      params: { path: "/tmp/file", container: "main" },
    });

    const [url] = mockFetch.mock.calls[0]!;
    expect(url).toContain("path=%2Ftmp%2Ffile");
    expect(url).toContain("container=main");
  });

  it("strips trailing slashes from baseURL", async () => {
    const client = new APIClient({ baseURL: "http://localhost:8080///" });
    mockFetch.mockResolvedValueOnce(jsonResponse({}));

    await client.request("GET", "/v1/test");

    const [url] = mockFetch.mock.calls[0]!;
    expect(url).toBe("http://localhost:8080/v1/test");
  });

  it("throws APIError on non-OK status", async () => {
    mockFetch.mockResolvedValueOnce(errorResponse(400, "invalid"));

    await expect(api.request("POST", "/v1/test")).rejects.toThrow(
      BadRequestError,
    );
  });

  it("throws APIConnectionError on fetch failure", async () => {
    // Use persistent mock — all retry attempts also fail
    mockFetch.mockRejectedValue(new TypeError("fetch failed"));

    await expect(api.request("GET", "/v1/test")).rejects.toThrow(
      APIConnectionError,
    );
  }, 15_000);
});

// ---------------------------------------------------------------------------
// Retry logic
// ---------------------------------------------------------------------------

describe("APIClient retry", () => {
  const api = new APIClient({ baseURL: "http://localhost:8080" });

  it("retries on transient 502 errors", async () => {
    mockFetch
      .mockResolvedValueOnce(errorResponse(502, "bad gateway"))
      .mockResolvedValueOnce(jsonResponse({ ok: true }));

    const result = await api.request<{ ok: boolean }>("GET", "/v1/test");
    expect(result).toEqual({ ok: true });
    expect(mockFetch).toHaveBeenCalledTimes(2);
  });

  it("retries on connection errors", async () => {
    mockFetch
      .mockRejectedValueOnce(new TypeError("fetch failed"))
      .mockResolvedValueOnce(jsonResponse({ ok: true }));

    const result = await api.request<{ ok: boolean }>("GET", "/v1/test");
    expect(result).toEqual({ ok: true });
    expect(mockFetch).toHaveBeenCalledTimes(2);
  });

  it("does not retry on 4xx errors", async () => {
    mockFetch.mockResolvedValue(errorResponse(400, "bad request"));

    await expect(api.request("POST", "/v1/test")).rejects.toThrow(
      BadRequestError,
    );
    expect(mockFetch).toHaveBeenCalledOnce();
  });

  it("gives up after max retries", async () => {
    for (let i = 0; i < 6; i++) {
      mockFetch.mockResolvedValueOnce(errorResponse(502, "bad gateway"));
    }

    await expect(api.request("GET", "/v1/test")).rejects.toThrow(
      BadGatewayError,
    );
    expect(mockFetch).toHaveBeenCalledTimes(6);
  }, 15_000);
});

// ---------------------------------------------------------------------------
// Retry with fake timers
// ---------------------------------------------------------------------------

describe("APIClient retry (fake timers)", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  const api = new APIClient({ baseURL: "http://localhost:8080" });

  it("retries with delay between attempts", async () => {
    mockFetch
      .mockResolvedValueOnce(errorResponse(503, "unavailable"))
      .mockResolvedValueOnce(errorResponse(503, "unavailable"))
      .mockResolvedValueOnce(jsonResponse({ ok: true }));

    const promise = api.request<{ ok: boolean }>("GET", "/v1/test");

    // Advance through retry delays
    await vi.advanceTimersByTimeAsync(5_000);

    const result = await promise;
    expect(result).toEqual({ ok: true });
    expect(mockFetch).toHaveBeenCalledTimes(3);
  });
});

// ---------------------------------------------------------------------------
// requestNoContent / requestBytes
// ---------------------------------------------------------------------------

describe("APIClient.requestNoContent", () => {
  const api = new APIClient({ baseURL: "http://localhost:8080" });

  it("returns void on success", async () => {
    mockFetch.mockResolvedValueOnce(noContentResponse());
    await expect(
      api.requestNoContent("DELETE", "/v1/test"),
    ).resolves.toBeUndefined();
  });
});

describe("APIClient.requestBytes", () => {
  const api = new APIClient({ baseURL: "http://localhost:8080" });

  it("returns Uint8Array", async () => {
    const data = new TextEncoder().encode("file contents");
    mockFetch.mockResolvedValueOnce(
      new Response(data, { status: 200 }),
    );

    const result = await api.requestBytes("GET", "/v1/test");
    expect(result).toBeInstanceOf(Uint8Array);
    expect(new TextDecoder().decode(result)).toBe("file contents");
  });
});
