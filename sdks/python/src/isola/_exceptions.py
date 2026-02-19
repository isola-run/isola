from __future__ import annotations

import json
from typing import Any

import httpx
from pydantic import ValidationError as PydanticValidationError

from ._models import ErrorDetail, ErrorResponse


class IsolaError(Exception):
    def __init__(
        self,
        *,
        status: int,
        detail: str,
        errors: list[ErrorDetail] | None = None,
        raw: dict[str, Any] | None = None,
    ) -> None:
        self.status = status
        self.detail = detail
        self.errors = errors
        self.raw = raw
        super().__init__(f"{status}: {detail}")


class BadRequestError(IsolaError):
    pass


class NotFoundError(IsolaError):
    pass


class ConflictError(IsolaError):
    pass


class ValidationError(IsolaError):
    pass


class InternalError(IsolaError):
    pass


class BadGatewayError(IsolaError):
    pass


class APIConnectionError(IsolaError):
    def __init__(self, detail: str, *, raw: dict[str, Any] | None = None) -> None:
        super().__init__(status=0, detail=detail, errors=None, raw=raw)


class StreamTimeoutError(IsolaError, TimeoutError):
    def __init__(self, detail: str) -> None:
        super().__init__(status=0, detail=detail, errors=None, raw=None)


_STATUS_TO_EXCEPTION: dict[int, type[IsolaError]] = {
    400: BadRequestError,
    404: NotFoundError,
    409: ConflictError,
    422: ValidationError,
    500: InternalError,
    502: BadGatewayError,
}


def error_from_http(status: int, reason: str | None, body: bytes | None = None) -> IsolaError:
    detail = reason or f"HTTP {status}"
    errors: list[ErrorDetail] | None = None
    raw: dict[str, Any] | None = None

    if body:
        try:
            payload = json.loads(body)
        except (json.JSONDecodeError, UnicodeDecodeError):
            payload = None

        if isinstance(payload, dict):
            raw = payload
            try:
                parsed = ErrorResponse.model_validate(payload)
            except PydanticValidationError:
                maybe_detail = payload.get("detail")
                if isinstance(maybe_detail, str) and maybe_detail:
                    detail = maybe_detail
                maybe_errors = payload.get("errors")
                if isinstance(maybe_errors, list):
                    parsed_errors: list[ErrorDetail] = []
                    for item in maybe_errors:
                        if not isinstance(item, dict):
                            continue
                        try:
                            parsed_errors.append(ErrorDetail.model_validate(item))
                        except PydanticValidationError:
                            continue
                    if parsed_errors:
                        errors = parsed_errors
            else:
                if parsed.detail:
                    detail = parsed.detail
                errors = parsed.errors

    exc_type = _STATUS_TO_EXCEPTION.get(status, IsolaError)
    return exc_type(status=status, detail=detail, errors=errors, raw=raw)


def connection_error_from_request(exc: httpx.RequestError) -> APIConnectionError:
    try:
        request_url = str(exc.request.url)
    except RuntimeError:
        request_url = None
    detail = str(exc) or "failed to reach Isola API"
    raw: dict[str, Any] = {"type": type(exc).__name__}
    if request_url:
        raw["url"] = request_url
    return APIConnectionError(detail=detail, raw=raw)
