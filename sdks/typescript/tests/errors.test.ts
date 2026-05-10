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

import { describe, expect, it } from "vitest";
import {
  APIConnectionError,
  APIError,
  BadGatewayError,
  BadRequestError,
  ConflictError,
  connectionErrorFromError,
  errorFromHttp,
  InternalError,
  IsolaError,
  IsolaTimeoutError,
  isAbortError,
  isTransient,
  NotFoundError,
  ValidationError,
} from "../src/errors";

describe("error class names", () => {
  it("each subclass reports its own name on instances", () => {
    expect(new IsolaError("x").name).toBe("IsolaError");
    expect(new APIError({ statusCode: 400, message: "x" }).name).toBe("APIError");
    expect(new BadRequestError({ statusCode: 400, message: "x" }).name).toBe("BadRequestError");
    expect(new NotFoundError({ statusCode: 404, message: "x" }).name).toBe("NotFoundError");
    expect(new ConflictError({ statusCode: 409, message: "x" }).name).toBe("ConflictError");
    expect(new ValidationError({ statusCode: 422, message: "x" }).name).toBe("ValidationError");
    expect(new InternalError({ statusCode: 500, message: "x" }).name).toBe("InternalError");
    expect(new BadGatewayError({ statusCode: 502, message: "x" }).name).toBe("BadGatewayError");
    expect(new IsolaTimeoutError("x").name).toBe("IsolaTimeoutError");
    expect(new APIConnectionError("x").name).toBe("APIConnectionError");
  });

  it("subclasses extend the right parents", () => {
    const e = new BadGatewayError({ statusCode: 502, message: "x" });
    expect(e).toBeInstanceOf(IsolaError);
    expect(e).toBeInstanceOf(APIError);
    expect(e).toBeInstanceOf(BadGatewayError);
  });

  it("APIError formats statusCode prefix in message", () => {
    expect(new APIError({ statusCode: 503, message: "boom" }).message).toBe("503: boom");
  });

  it("IsolaTimeoutError is an IsolaError", () => {
    expect(new IsolaTimeoutError("x")).toBeInstanceOf(IsolaError);
  });

  it("APIError preserves cause when provided", () => {
    // Covers errors.ts:46 — the ternary's truthy branch passing { cause }.
    const inner = new Error("inner");
    const e = new APIError({ statusCode: 500, message: "boom", cause: inner });
    expect(e.cause).toBe(inner);
  });

  it("APIError has undefined cause when not provided", () => {
    const e = new APIError({ statusCode: 500, message: "boom" });
    expect(e.cause).toBeUndefined();
  });
});

describe("isTransient", () => {
  const cases: Array<[unknown, boolean, string]> = [
    [new APIConnectionError("conn"), true, "APIConnectionError"],
    [new BadGatewayError({ statusCode: 502, message: "x" }), true, "502"],
    [new APIError({ statusCode: 503, message: "x" }), true, "503"],
    [new APIError({ statusCode: 504, message: "x" }), true, "504"],
    [new BadRequestError({ statusCode: 400, message: "x" }), false, "400"],
    [new NotFoundError({ statusCode: 404, message: "x" }), false, "404"],
    [new InternalError({ statusCode: 500, message: "x" }), false, "500"],
    [new IsolaError("x"), false, "IsolaError"],
    ["plain string", false, "non-error"],
  ];
  for (const [err, expected, label] of cases) {
    it(`${label} -> ${expected}`, () => {
      expect(isTransient(err)).toBe(expected);
    });
  }
});

describe("isAbortError", () => {
  it("matches AbortError name", () => {
    const e = new DOMException("aborted", "AbortError");
    expect(isAbortError(e)).toBe(true);
  });
  it("matches TimeoutError name", () => {
    const e = new DOMException("timed out", "TimeoutError");
    expect(isAbortError(e)).toBe(true);
  });
  it("matches legacy ABORT_ERR code", () => {
    const e = Object.assign(new Error(), { code: "ABORT_ERR" });
    expect(isAbortError(e)).toBe(true);
  });
  it("rejects unrelated errors", () => {
    expect(isAbortError(new Error("oops"))).toBe(false);
    expect(isAbortError(null)).toBe(false);
    expect(isAbortError("hi")).toBe(false);
  });
});

describe("errorFromHttp", () => {
  it("maps known statuses", () => {
    const cases: Array<[number, new (...args: never[]) => Error]> = [
      [400, BadRequestError],
      [404, NotFoundError],
      [409, ConflictError],
      [422, ValidationError],
      [500, InternalError],
      [502, BadGatewayError],
    ];
    for (const [status, Ctor] of cases) {
      const e = errorFromHttp({ status, reason: null, body: null });
      expect(e).toBeInstanceOf(Ctor);
      expect(e.statusCode).toBe(status);
    }
  });

  it("falls through to plain APIError for 401/403/503/504", () => {
    for (const status of [401, 403, 503, 504]) {
      const e = errorFromHttp({ status, reason: null, body: null });
      expect(e.constructor).toBe(APIError);
      expect(e.statusCode).toBe(status);
    }
  });

  it("decodes detail field from JSON body", () => {
    const body = new TextEncoder().encode(JSON.stringify({ detail: "sandbox not found" }));
    const e = errorFromHttp({ status: 404, reason: "Not Found", body });
    expect(e.message).toContain("sandbox not found");
  });

  it("includes method/path prefix when both provided", () => {
    const body = new TextEncoder().encode(JSON.stringify({ detail: "boom" }));
    const e = errorFromHttp({ status: 500, reason: null, body, method: "GET", path: "/v1/x" });
    expect(e.message).toContain("GET /v1/x:");
    expect(e.message).toContain("boom");
  });

  it("uses HTTP {status} when no reason and no body", () => {
    const e = errorFromHttp({ status: 599, reason: null, body: null });
    expect(e.message).toContain("HTTP 599");
  });

  it("ignores invalid JSON body", () => {
    const body = new TextEncoder().encode("<html>not json</html>");
    const e = errorFromHttp({ status: 500, reason: "Internal Server Error", body });
    expect(e.message).toContain("Internal Server Error");
  });
});

describe("connectionErrorFromError", () => {
  it("uses Error.message when available", () => {
    const cause = new Error("connect failed");
    const e = connectionErrorFromError(cause, { method: "GET", path: "/v1/sandboxes" });
    expect(e.message).toBe("GET /v1/sandboxes: connect failed");
    expect(e.cause).toBe(cause);
  });
  it("falls back to default message when source has none", () => {
    const e = connectionErrorFromError(null);
    expect(e.message).toBe("failed to reach Isola API");
  });

  it("uses string err verbatim as detail when err is a non-empty string", () => {
    const e = connectionErrorFromError("plain string");
    expect(e.message).toBe("plain string");
    expect(e.cause).toBe("plain string");
  });

  it("uses string err with method/path prefix", () => {
    const e = connectionErrorFromError("dns lookup failed", { method: "POST", path: "/v1/x" });
    expect(e.message).toBe("POST /v1/x: dns lookup failed");
  });

  it("falls back to default message when string err is empty", () => {
    const e = connectionErrorFromError("");
    expect(e.message).toBe("failed to reach Isola API");
  });
});

describe("errorFromHttp body decoding edge cases", () => {
  it("treats null body as no detail (uses reason)", () => {
    const e = errorFromHttp({ status: 500, reason: "Server Error", body: null });
    expect(e.message).toContain("Server Error");
    expect(e.message).not.toContain("undefined");
  });

  it("treats empty body as no detail (uses reason)", () => {
    const e = errorFromHttp({ status: 500, reason: "Server Error", body: new Uint8Array() });
    expect(e.message).toContain("Server Error");
  });

  it("ignores JSON body that is an array (not an object detail)", () => {
    const body = new TextEncoder().encode(JSON.stringify(["one", "two"]));
    const e = errorFromHttp({ status: 500, reason: "Server Error", body });
    expect(e.message).toContain("Server Error");
  });

  it("ignores JSON body where detail is not a string", () => {
    const body = new TextEncoder().encode(JSON.stringify({ detail: 42 }));
    const e = errorFromHttp({ status: 500, reason: "Server Error", body });
    expect(e.message).toContain("Server Error");
  });

  it("ignores JSON body where detail is an empty string", () => {
    const body = new TextEncoder().encode(JSON.stringify({ detail: "" }));
    const e = errorFromHttp({ status: 500, reason: "Server Error", body });
    expect(e.message).toContain("Server Error");
  });

  it("ignores JSON body that is null", () => {
    const body = new TextEncoder().encode("null");
    const e = errorFromHttp({ status: 500, reason: "Server Error", body });
    expect(e.message).toContain("Server Error");
  });
});
