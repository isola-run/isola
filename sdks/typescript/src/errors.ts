/**
 * Error hierarchy for the Isola SDK.
 *
 * IsolaError
 * ├── APIError (status code from server)
 * │   ├── BadRequestError (400)
 * │   ├── NotFoundError (404)
 * │   ├── ConflictError (409)
 * │   ├── ValidationError (422)
 * │   ├── InternalError (500)
 * │   └── BadGatewayError (502)
 * └── APIConnectionError (transport / network)
 */

export class IsolaError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "IsolaError";
    Object.setPrototypeOf(this, new.target.prototype);
  }
}

export class APIError extends IsolaError {
  readonly status: number;
  readonly headers: Headers;

  constructor(status: number, message: string, headers: Headers) {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.headers = headers;
  }

  /** Build the appropriate error subclass from an HTTP response. */
  static fromResponse(
    status: number,
    body: string,
    headers: Headers,
    method: string,
    path: string,
  ): APIError {
    let detail: string;
    try {
      const parsed = JSON.parse(body) as Record<string, unknown>;
      detail =
        typeof parsed.detail === "string"
          ? parsed.detail
          : typeof parsed.message === "string"
            ? parsed.message
            : body;
    } catch {
      detail = body || `request failed with status ${status}`;
    }

    const message = `${method} ${path}: ${detail}`;

    switch (status) {
      case 400:
        return new BadRequestError(status, message, headers);
      case 404:
        return new NotFoundError(status, message, headers);
      case 409:
        return new ConflictError(status, message, headers);
      case 422:
        return new ValidationError(status, message, headers);
      case 500:
        return new InternalError(status, message, headers);
      case 502:
        return new BadGatewayError(status, message, headers);
      default:
        return new APIError(status, message, headers);
    }
  }
}

export class BadRequestError extends APIError {
  override readonly name = "BadRequestError";
}

export class NotFoundError extends APIError {
  override readonly name = "NotFoundError";
}

export class ConflictError extends APIError {
  override readonly name = "ConflictError";
}

export class ValidationError extends APIError {
  override readonly name = "ValidationError";
}

export class InternalError extends APIError {
  override readonly name = "InternalError";
}

export class BadGatewayError extends APIError {
  override readonly name = "BadGatewayError";
}

export class APIConnectionError extends IsolaError {
  override readonly cause: Error | undefined;

  constructor(message: string, cause?: Error) {
    super(message);
    this.name = "APIConnectionError";
    this.cause = cause;
  }

  /** Wrap a transport-level error with request context. */
  static fromError(
    error: unknown,
    method: string,
    path: string,
  ): APIConnectionError {
    const cause = error instanceof Error ? error : undefined;
    const detail = error instanceof Error ? error.message : String(error);
    return new APIConnectionError(
      `${method} ${path}: connection error: ${detail}`,
      cause,
    );
  }
}

/** Returns true for errors that are safe to retry (connection failures, 408/429/502/503/504). */
export function isTransient(error: IsolaError): boolean {
  if (error instanceof APIConnectionError) return true;
  if (error instanceof APIError) {
    return (
      error.status === 408 ||
      error.status === 429 ||
      error.status === 502 ||
      error.status === 503 ||
      error.status === 504
    );
  }
  return false;
}
