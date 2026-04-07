# Copyright The Isola Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from __future__ import annotations

import json

import httpx


class IsolaError(Exception):
    """Base exception for all Isola SDK errors.

    Attributes:
        message: Human-readable error description.
    """

    def __init__(self, message: str) -> None:
        self.message = message
        super().__init__(message)


class APIError(IsolaError):
    """An error response from the Isola API.

    Attributes:
        status_code: HTTP status code from the API.
        message: Error detail from the response body.
    """

    def __init__(self, *, status_code: int, message: str) -> None:
        self.status_code = status_code
        super().__init__(f"{status_code}: {message}")


class BadRequestError(APIError):
    """The request was malformed or invalid."""

    pass


class NotFoundError(APIError):
    """The requested resource does not exist."""

    pass


class ConflictError(APIError):
    """The request conflicts with current state."""

    pass


class ValidationError(APIError):
    """The request body failed validation."""

    pass


class InternalError(APIError):
    """An unexpected error on the server."""

    pass


class BadGatewayError(APIError):
    """The server received an invalid response from upstream."""

    pass


class IsolaTimeoutError(IsolaError):
    """A timeout expired while waiting for an operation to complete."""

    pass


class APIConnectionError(IsolaError, ConnectionError):
    """An error occurred communicating with the Isola API."""

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


_TRANSIENT_HTTP_STATUSES: frozenset[int] = frozenset({502, 503, 504})


def is_transient(exc: IsolaError) -> bool:
    if isinstance(exc, APIConnectionError):
        return True
    return isinstance(exc, APIError) and exc.status_code in _TRANSIENT_HTTP_STATUSES


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
