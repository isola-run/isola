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

// Mirrors sdks/python/src/isola/_exceptions.py.

function setErrorName(err: Error, name: string): void {
  Object.defineProperty(err, "name", {
    value: name,
    configurable: true,
    writable: true,
  });
}

/** Base exception for all Isola SDK errors. */
export class IsolaError extends Error {
  /** @internal Minifier-safe error name; survives class-name mangling. */
  protected static readonly errorName: string = "IsolaError";

  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options);
    // Restore prototype chain for ES5/ES2015 transpile targets.
    Object.setPrototypeOf(this, new.target.prototype);
    const ctor = new.target as typeof IsolaError;
    setErrorName(this, ctor.errorName);
  }
}

export interface APIErrorOptions {
  statusCode: number;
  message: string;
  cause?: unknown;
}

/** An error response from the Isola API. */
export class APIError extends IsolaError {
  protected static override readonly errorName: string = "APIError";
  /** HTTP status code from the API. */
  readonly statusCode: number;

  constructor(opts: APIErrorOptions) {
    super(`${opts.statusCode}: ${opts.message}`, opts.cause !== undefined ? { cause: opts.cause } : undefined);
    this.statusCode = opts.statusCode;
  }
}

/** The request was malformed or invalid. */
export class BadRequestError extends APIError {
  protected static override readonly errorName: string = "BadRequestError";
}

/** The requested resource does not exist. */
export class NotFoundError extends APIError {
  protected static override readonly errorName: string = "NotFoundError";
}

/** The request conflicts with current state. */
export class ConflictError extends APIError {
  protected static override readonly errorName: string = "ConflictError";
}

/** The request body failed validation. */
export class ValidationError extends APIError {
  protected static override readonly errorName: string = "ValidationError";
}

/** An unexpected error on the server. */
export class InternalError extends APIError {
  protected static override readonly errorName: string = "InternalError";
}

/** The server received an invalid response from upstream. */
export class BadGatewayError extends APIError {
  protected static override readonly errorName: string = "BadGatewayError";
}

// Status codes 401/403/503/504 intentionally fall through to base APIError
// (no dedicated subclass) for parity with the Python SDK.

/** A timeout expired while waiting for an operation to complete. */
export class IsolaTimeoutError extends IsolaError {
  protected static override readonly errorName: string = "IsolaTimeoutError";
}

/** An error occurred communicating with the Isola API. */
export class APIConnectionError extends IsolaError {
  protected static override readonly errorName: string = "APIConnectionError";
}

const STATUS_TO_EXCEPTION: Record<number, new (opts: APIErrorOptions) => APIError> = {
  400: BadRequestError,
  404: NotFoundError,
  409: ConflictError,
  422: ValidationError,
  500: InternalError,
  502: BadGatewayError,
};

const TRANSIENT_HTTP_STATUSES: ReadonlySet<number> = new Set([502, 503, 504]);

export function isTransient(err: unknown): boolean {
  if (err instanceof APIConnectionError) return true;
  if (err instanceof APIError) return TRANSIENT_HTTP_STATUSES.has(err.statusCode);
  return false;
}

// Treats the spec-compliant runtime cases (`AbortError` / `TimeoutError`)
// and legacy Node (`code === "ABORT_ERR"`).
export function isAbortError(err: unknown): boolean {
  if (err == null || typeof err !== "object") return false;
  const e = err as { name?: unknown; code?: unknown };
  if (typeof e.name === "string" && (e.name === "AbortError" || e.name === "TimeoutError")) return true;
  if (typeof e.code === "string" && e.code === "ABORT_ERR") return true;
  return false;
}

function decodeBodyDetail(body: Uint8Array | null): string | null {
  if (!body || body.length === 0) return null;
  // fatal: false → invalid bytes become U+FFFD, decode never throws.
  const text = new TextDecoder("utf-8", { fatal: false }).decode(body);
  let payload: unknown;
  try {
    payload = JSON.parse(text);
  } catch {
    return null;
  }
  if (payload && typeof payload === "object" && !Array.isArray(payload)) {
    const detail = (payload as { detail?: unknown }).detail;
    if (typeof detail === "string" && detail.length > 0) return detail;
  }
  return null;
}

export function errorFromHttp(opts: {
  status: number;
  reason: string | null;
  body: Uint8Array | null;
  method?: string;
  path?: string;
  cause?: unknown;
}): APIError {
  let message = opts.reason && opts.reason.length > 0 ? opts.reason : `HTTP ${opts.status}`;
  const detail = decodeBodyDetail(opts.body);
  if (detail) message = detail;

  if (opts.method && opts.path) {
    message = `${opts.method} ${opts.path}: ${message}`;
  }

  const Ctor = STATUS_TO_EXCEPTION[opts.status] ?? APIError;
  return new Ctor({
    statusCode: opts.status,
    message,
    ...(opts.cause !== undefined ? { cause: opts.cause } : {}),
  });
}

export function connectionErrorFromError(err: unknown, opts?: { method?: string; path?: string }): APIConnectionError {
  const prefix = opts?.method && opts?.path ? `${opts.method} ${opts.path}: ` : "";
  let detail = "failed to reach Isola API";
  if (err != null && typeof err === "object") {
    const message = (err as { message?: unknown }).message;
    if (typeof message === "string" && message.length > 0) detail = message;
  } else if (typeof err === "string" && err.length > 0) {
    detail = err;
  }
  return new APIConnectionError(`${prefix}${detail}`, { cause: err });
}
