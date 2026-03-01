from __future__ import annotations

import json

import httpx


class IsolaError(Exception):
    def __init__(self, message: str) -> None:
        self.message = message
        super().__init__(message)


class APIError(IsolaError):
    def __init__(self, *, status_code: int, message: str) -> None:
        self.status_code = status_code
        super().__init__(f"{status_code}: {message}")


class BadRequestError(APIError):
    pass


class NotFoundError(APIError):
    pass


class ConflictError(APIError):
    pass


class ValidationError(APIError):
    pass


class InternalError(APIError):
    pass


class BadGatewayError(APIError):
    pass


class APIConnectionError(IsolaError, ConnectionError):
    pass


_STATUS_TO_EXCEPTION: dict[int, type[APIError]] = {
    400: BadRequestError,
    404: NotFoundError,
    409: ConflictError,
    422: ValidationError,
    500: InternalError,
    502: BadGatewayError,
}


def error_from_http(
    status: int,
    reason: str | None,
    body: bytes | None = None,
    *,
    method: str | None = None,
    path: str | None = None,
) -> APIError:
    message = reason or f"HTTP {status}"

    if body:
        try:
            payload = json.loads(body)
        except (json.JSONDecodeError, UnicodeDecodeError):
            payload = None

        if isinstance(payload, dict):
            maybe_detail = payload.get("detail")
            if isinstance(maybe_detail, str) and maybe_detail:
                message = maybe_detail

    if method and path:
        message = f"{method} {path}: {message}"

    exc_type = _STATUS_TO_EXCEPTION.get(status, APIError)
    return exc_type(status_code=status, message=message)


TRANSIENT_HTTP_STATUSES: frozenset[int] = frozenset({502, 503, 504})


def is_transient(exc: IsolaError) -> bool:
    if isinstance(exc, APIConnectionError):
        return True
    return isinstance(exc, APIError) and exc.status_code in TRANSIENT_HTTP_STATUSES


def connection_error_from_request(
    exc: httpx.RequestError,
    *,
    method: str | None = None,
    path: str | None = None,
) -> APIConnectionError:
    message = str(exc) or "failed to reach Isola API"
    if method and path:
        message = f"{method} {path}: {message}"
    return APIConnectionError(message)
