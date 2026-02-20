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
    def __init__(self, *, status: int, detail: str) -> None:
        self.status = status
        self.detail = detail
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
    def __init__(self, detail: str) -> None:
        super().__init__(status=0, detail=detail)


class StreamTimeoutError(IsolaError, TimeoutError):
    def __init__(self, detail: str) -> None:
        super().__init__(status=0, detail=detail)


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

    if body:
        try:
            payload = json.loads(body)
        except (json.JSONDecodeError, UnicodeDecodeError):
            payload = None

        if isinstance(payload, dict):
            maybe_detail = payload.get("detail")
            if isinstance(maybe_detail, str) and maybe_detail:
                detail = maybe_detail

    exc_type = _STATUS_TO_EXCEPTION.get(status, IsolaError)
    return exc_type(status=status, detail=detail)


def connection_error_from_request(exc: httpx.RequestError) -> APIConnectionError:
    detail = str(exc) or "failed to reach Isola API"
    return APIConnectionError(detail=detail)
