import { describe, it, expect } from "vitest";
import {
  IsolaError,
  APIError,
  BadRequestError,
  NotFoundError,
  ConflictError,
  ValidationError,
  InternalError,
  BadGatewayError,
  APIConnectionError,
  isTransient,
} from "../src/errors.js";

const emptyHeaders = new Headers();

describe("APIError.fromResponse", () => {
  it("parses detail from JSON body", () => {
    const error = APIError.fromResponse(
      400,
      JSON.stringify({ detail: "bad input" }),
      emptyHeaders,
      "POST",
      "/v1/sandboxes",
    );
    expect(error).toBeInstanceOf(BadRequestError);
    expect(error.status).toBe(400);
    expect(error.message).toContain("bad input");
    expect(error.headers).toBe(emptyHeaders);
  });

  it("parses message field when detail is absent", () => {
    const error = APIError.fromResponse(
      500,
      JSON.stringify({ message: "oops" }),
      emptyHeaders,
      "GET",
      "/v1/sandboxes",
    );
    expect(error).toBeInstanceOf(InternalError);
    expect(error.message).toContain("oops");
  });

  it("uses raw body when JSON parsing fails", () => {
    const error = APIError.fromResponse(
      404,
      "not found",
      emptyHeaders,
      "GET",
      "/v1/sandboxes/x",
    );
    expect(error).toBeInstanceOf(NotFoundError);
    expect(error.message).toContain("not found");
  });

  it("maps status codes to the correct subclass", () => {
    const cases: Array<[number, new (...args: never[]) => APIError]> = [
      [400, BadRequestError],
      [404, NotFoundError],
      [409, ConflictError],
      [422, ValidationError],
      [500, InternalError],
      [502, BadGatewayError],
    ];

    for (const [status, ErrorClass] of cases) {
      const error = APIError.fromResponse(
        status,
        "{}",
        emptyHeaders,
        "GET",
        "/test",
      );
      expect(error).toBeInstanceOf(ErrorClass);
      expect(error.status).toBe(status);
    }
  });

  it("returns base APIError for unmapped status codes", () => {
    const error = APIError.fromResponse(
      418,
      "{}",
      emptyHeaders,
      "GET",
      "/test",
    );
    expect(error).toBeInstanceOf(APIError);
    expect(error).not.toBeInstanceOf(BadRequestError);
    expect(error.status).toBe(418);
  });
});

describe("APIConnectionError.fromError", () => {
  it("wraps an Error instance and preserves cause", () => {
    const cause = new Error("ECONNREFUSED");
    const error = APIConnectionError.fromError(cause, "GET", "/test");
    expect(error).toBeInstanceOf(APIConnectionError);
    expect(error.message).toContain("ECONNREFUSED");
    expect(error.message).toContain("GET /test");
    expect(error.cause).toBe(cause);
  });

  it("wraps a non-Error value", () => {
    const error = APIConnectionError.fromError("timeout", "POST", "/test");
    expect(error).toBeInstanceOf(APIConnectionError);
    expect(error.message).toContain("timeout");
    expect(error.cause).toBeUndefined();
  });
});

describe("isTransient", () => {
  it("returns true for APIConnectionError", () => {
    expect(isTransient(new APIConnectionError("fail"))).toBe(true);
  });

  it("returns true for 408, 429, 502, 503, 504", () => {
    for (const status of [408, 429, 502, 503, 504]) {
      expect(
        isTransient(new APIError(status, "err", emptyHeaders)),
      ).toBe(true);
    }
  });

  it("returns false for non-transient 4xx errors", () => {
    for (const status of [400, 404, 409, 422]) {
      expect(
        isTransient(new APIError(status, "err", emptyHeaders)),
      ).toBe(false);
    }
  });

  it("returns false for base IsolaError", () => {
    expect(isTransient(new IsolaError("err"))).toBe(false);
  });
});

describe("error instanceof chains", () => {
  it("BadRequestError is an APIError, IsolaError, and Error", () => {
    const err = new BadRequestError(400, "bad", emptyHeaders);
    expect(err).toBeInstanceOf(BadRequestError);
    expect(err).toBeInstanceOf(APIError);
    expect(err).toBeInstanceOf(IsolaError);
    expect(err).toBeInstanceOf(Error);
  });

  it("APIConnectionError is an IsolaError and Error", () => {
    const err = new APIConnectionError("fail");
    expect(err).toBeInstanceOf(APIConnectionError);
    expect(err).toBeInstanceOf(IsolaError);
    expect(err).toBeInstanceOf(Error);
  });
});
