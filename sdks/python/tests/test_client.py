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

import io
from unittest.mock import patch

import httpx
import pytest
import respx

from isola import (
    APIConnectionError,
    APIError,
    AsyncIsola,
    BadGatewayError,
    BadRequestError,
    ConflictError,
    InternalError,
    Isola,
    NotFoundError,
    ValidationError,
)
from isola._client import _NOT_SEEKABLE, MAX_RETRIES, _stream_initial_position, _try_rewind


@respx.mock
def test_sync_client_uses_base_url_with_trailing_slash() -> None:
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

    with Isola(base_url="http://localhost:8080/") as client:
        assert client.sandboxes.list() == []

    assert route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_client_list() -> None:
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandboxes = await client.sandboxes.list()

    assert sandboxes == []
    assert route.called


@respx.mock
def test_error_mapping_for_problem_details() -> None:
    respx.get("http://localhost:8080/v1/sandboxes/missing").mock(
        return_value=httpx.Response(
            404,
            json={
                "status": 404,
                "title": "Not Found",
                "detail": "sandbox not found",
            },
        )
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(NotFoundError) as exc_info:
        client.sandboxes.get("missing")

    assert exc_info.value.status_code == 404
    assert "sandbox not found" in exc_info.value.message


@respx.mock
def test_transport_error_mapping(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    route = respx.get("http://localhost:8080/v1/sandboxes")
    route.mock(side_effect=httpx.ConnectError("connect failed"))

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIConnectionError) as exc_info:
        client.sandboxes.list()

    assert "connect failed" in exc_info.value.message
    assert "GET /v1/sandboxes" in exc_info.value.message


@pytest.mark.parametrize(
    ("status_code", "exc_type"),
    [
        (400, BadRequestError),
        (404, NotFoundError),
        (409, ConflictError),
        (422, ValidationError),
        (500, InternalError),
        (502, BadGatewayError),
    ],
    ids=["400", "404", "409", "422", "500", "502"],
)
@respx.mock
def test_error_status_code_mapping(status_code: int, exc_type: type[APIError], monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/v1/sandboxes/bad").mock(
        return_value=httpx.Response(
            status_code,
            json={"status": status_code, "detail": "test error"},
        )
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(exc_type) as exc_info:
        client.sandboxes.get("bad")

    assert exc_info.value.status_code == status_code
    assert "test error" in exc_info.value.message


@respx.mock
def test_unknown_status_code_raises_base_api_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/v1/sandboxes/bad").mock(
        return_value=httpx.Response(503, json={"detail": "service unavailable"})
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIError) as exc_info:
        client.sandboxes.get("bad")

    assert type(exc_info.value) is APIError
    assert exc_info.value.status_code == 503


@respx.mock
def test_invalid_json_response_raises_api_error() -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, content=b"<html>not json</html>")
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIError) as exc_info:
        client.sandboxes.get("sandbox-123")

    assert exc_info.value.message == "200: invalid response payload"


@respx.mock
def test_json_schema_mismatch_raises_api_error() -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json={"unexpected": "schema"})
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIError) as exc_info:
        client.sandboxes.get("sandbox-123")

    assert exc_info.value.message == "200: invalid response payload"


def test_empty_base_url_raises_value_error() -> None:
    with pytest.raises(ValueError, match="ISOLA_BASE_URL"):
        Isola(base_url="")


def test_no_base_url_raises_value_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("ISOLA_BASE_URL", raising=False)
    with pytest.raises(ValueError, match="ISOLA_BASE_URL"):
        Isola()


def test_base_url_from_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("ISOLA_BASE_URL", "http://from-env:9090")
    client = Isola()
    assert client._api.base_url == "http://from-env:9090"
    client.close()


def test_explicit_base_url_overrides_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("ISOLA_BASE_URL", "http://from-env:9090")
    client = Isola(base_url="http://explicit:8080")
    assert client._api.base_url == "http://explicit:8080"
    client.close()


@respx.mock
def test_unexpected_exception_raises_api_connection_error() -> None:
    respx.get("http://localhost:8080/v1/sandboxes").mock(return_value=httpx.Response(200, json={"sandboxes": []}))

    with (
        Isola(base_url="http://localhost:8080") as client,
        patch.object(client._api._client, "request", side_effect=RuntimeError("unexpected")),
        pytest.raises(APIConnectionError) as exc_info,
    ):
        client.sandboxes.list()

    assert "unexpected" in exc_info.value.message


# --- Client retry tests ---


@respx.mock
def test_retries_transient_502_then_succeeds(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/v1/sandboxes").mock(
        side_effect=[
            httpx.Response(502, json={"detail": "bad gateway"}),
            httpx.Response(200, json={"sandboxes": []}),
        ]
    )
    with Isola(base_url="http://localhost:8080") as client:
        assert client.sandboxes.list() == []


@respx.mock
def test_retries_transient_504_then_succeeds(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/v1/sandboxes").mock(
        side_effect=[
            httpx.Response(504, json={"detail": "timeout"}),
            httpx.Response(200, json={"sandboxes": []}),
        ]
    )
    with Isola(base_url="http://localhost:8080") as client:
        assert client.sandboxes.list() == []


@respx.mock
def test_retries_connection_error_then_succeeds(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/v1/sandboxes").mock(
        side_effect=[
            httpx.ConnectError("connect failed"),
            httpx.Response(200, json={"sandboxes": []}),
        ]
    )
    with Isola(base_url="http://localhost:8080") as client:
        assert client.sandboxes.list() == []


@respx.mock
def test_no_retry_on_404(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    route = respx.get("http://localhost:8080/v1/sandboxes/bad").mock(
        return_value=httpx.Response(404, json={"detail": "not found"})
    )
    with Isola(base_url="http://localhost:8080") as client, pytest.raises(NotFoundError):
        client.sandboxes.get("bad")
    assert route.call_count == 1


@respx.mock
def test_no_retry_on_400(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    route = respx.get("http://localhost:8080/v1/sandboxes/bad").mock(
        return_value=httpx.Response(400, json={"detail": "bad request"})
    )
    with Isola(base_url="http://localhost:8080") as client, pytest.raises(BadRequestError):
        client.sandboxes.get("bad")
    assert route.call_count == 1


@respx.mock
def test_exhausts_retries_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(502, json={"detail": "bad gateway"})
    )
    with Isola(base_url="http://localhost:8080") as client, pytest.raises(BadGatewayError):
        client.sandboxes.list()
    assert route.call_count == 1 + MAX_RETRIES


@respx.mock
def test_exhausts_retries_on_connection_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    route = respx.get("http://localhost:8080/v1/sandboxes")
    route.mock(side_effect=httpx.ConnectError("connect failed"))
    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIConnectionError):
        client.sandboxes.list()
    assert route.call_count == 1 + MAX_RETRIES


@pytest.mark.asyncio
@respx.mock
async def test_async_retries_transient_502_then_succeeds(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_: float) -> None:
        return None

    monkeypatch.setattr("isola._client.asyncio.sleep", _no_sleep)
    respx.get("http://localhost:8080/v1/sandboxes").mock(
        side_effect=[
            httpx.Response(502, json={"detail": "bad gateway"}),
            httpx.Response(200, json={"sandboxes": []}),
        ]
    )
    async with AsyncIsola(base_url="http://localhost:8080") as client:
        assert await client.sandboxes.list() == []


@pytest.mark.asyncio
@respx.mock
async def test_async_retries_connection_error_then_succeeds(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_: float) -> None:
        return None

    monkeypatch.setattr("isola._client.asyncio.sleep", _no_sleep)
    respx.get("http://localhost:8080/v1/sandboxes").mock(
        side_effect=[
            httpx.ConnectError("connect failed"),
            httpx.Response(200, json={"sandboxes": []}),
        ]
    )
    async with AsyncIsola(base_url="http://localhost:8080") as client:
        assert await client.sandboxes.list() == []


@pytest.mark.asyncio
@respx.mock
async def test_async_no_retry_on_404(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_: float) -> None:
        return None

    monkeypatch.setattr("isola._client.asyncio.sleep", _no_sleep)
    route = respx.get("http://localhost:8080/v1/sandboxes/bad").mock(
        return_value=httpx.Response(404, json={"detail": "not found"})
    )
    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(NotFoundError):
            await client.sandboxes.get("bad")
    assert route.call_count == 1


@pytest.mark.asyncio
@respx.mock
async def test_async_exhausts_retries_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    async def _no_sleep(_: float) -> None:
        return None

    monkeypatch.setattr("isola._client.asyncio.sleep", _no_sleep)
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(502, json={"detail": "bad gateway"})
    )
    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(BadGatewayError):
            await client.sandboxes.list()
    assert route.call_count == 1 + MAX_RETRIES


# --- Context manager and close() tests ---


@respx.mock
def test_sync_context_manager_closes_client() -> None:
    respx.get("http://localhost:8080/v1/sandboxes").mock(return_value=httpx.Response(200, json={"sandboxes": []}))

    with Isola(base_url="http://localhost:8080") as client:
        client.sandboxes.list()

    assert client._api._client.is_closed


@pytest.mark.asyncio
@respx.mock
async def test_async_context_manager_closes_client() -> None:
    respx.get("http://localhost:8080/v1/sandboxes").mock(return_value=httpx.Response(200, json={"sandboxes": []}))

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        await client.sandboxes.list()

    assert client._api._client.is_closed


@respx.mock
def test_sync_context_manager_closes_on_exception() -> None:
    respx.get("http://localhost:8080/v1/sandboxes/missing").mock(
        return_value=httpx.Response(404, json={"detail": "not found"})
    )

    with pytest.raises(NotFoundError), Isola(base_url="http://localhost:8080") as client:
        client.sandboxes.get("missing")

    assert client._api._client.is_closed


@pytest.mark.asyncio
@respx.mock
async def test_async_context_manager_closes_on_exception() -> None:
    respx.get("http://localhost:8080/v1/sandboxes/missing").mock(
        return_value=httpx.Response(404, json={"detail": "not found"})
    )

    with pytest.raises(NotFoundError):
        async with AsyncIsola(base_url="http://localhost:8080") as client:
            await client.sandboxes.get("missing")

    assert client._api._client.is_closed


def test_sync_close_is_idempotent() -> None:
    client = Isola(base_url="http://localhost:8080")
    client.close()
    client.close()
    assert client._api._client.is_closed


@pytest.mark.asyncio
async def test_async_close_is_idempotent() -> None:
    client = AsyncIsola(base_url="http://localhost:8080")
    await client.close()
    await client.close()
    assert client._api._client.is_closed


# --- Stream rewind helpers ---


def test_stream_initial_position_returns_none_for_bytes() -> None:
    assert _stream_initial_position(b"hello") is None
    assert _stream_initial_position(None) is None


def test_stream_initial_position_returns_tell_for_seekable() -> None:
    stream = io.BytesIO(b"hello")
    stream.read(2)
    assert _stream_initial_position(stream) == 2


def test_try_rewind_returns_true_for_bytes() -> None:
    assert _try_rewind(b"hello", None) is True
    assert _try_rewind(None, None) is True


def test_try_rewind_seeks_stream_to_initial_position() -> None:
    stream = io.BytesIO(b"hello world")
    stream.read(5)
    initial = stream.tell()
    stream.read()  # consume the rest
    assert _try_rewind(stream, initial) is True
    assert stream.tell() == initial


def test_try_rewind_returns_false_for_non_seekable() -> None:
    class NonSeekable(io.RawIOBase):
        def read(self, _n: int = -1) -> bytes:
            return b""

        def seek(self, _offset: int, _whence: int = 0) -> int:
            raise OSError("not seekable")

    stream = NonSeekable()
    assert _try_rewind(stream, 0) is False


def test_try_rewind_returns_false_for_not_seekable_sentinel() -> None:
    assert _try_rewind(io.BytesIO(b"x"), _NOT_SEEKABLE) is False


def test_stream_initial_position_returns_not_seekable_when_tell_raises() -> None:
    class TellRaises(io.RawIOBase):
        def tell(self) -> int:
            raise io.UnsupportedOperation("tell")

    assert _stream_initial_position(TellRaises()) == _NOT_SEEKABLE


def test_stream_initial_position_returns_not_seekable_for_stream_without_tell() -> None:
    class NoTell:
        def read(self, _n: int = -1) -> bytes:
            return b""

    assert _stream_initial_position(NoTell()) == _NOT_SEEKABLE  # type: ignore[arg-type]


# --- Stream rewind retry integration tests ---


@respx.mock
def test_retries_rewind_stream_on_transient_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    payload = b"file content here"
    bodies_received: list[bytes] = []

    def capture_body(request: httpx.Request) -> httpx.Response:
        bodies_received.append(request.content)
        if len(bodies_received) == 1:
            return httpx.Response(502, json={"detail": "bad gateway"})
        return httpx.Response(200, json={"sandboxes": []})

    respx.get("http://localhost:8080/v1/sandboxes").mock(side_effect=capture_body)

    stream = io.BytesIO(payload)
    with Isola(base_url="http://localhost:8080") as client:
        client._api.request("GET", "/v1/sandboxes", content=stream)

    assert len(bodies_received) == 2
    assert bodies_received[0] == payload
    assert bodies_received[1] == payload


@respx.mock
def test_no_retry_for_non_seekable_stream_on_transient_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)

    class NonSeekableStream(io.RawIOBase):
        def __init__(self, data: bytes) -> None:
            self._data = data
            self._pos = 0

        def read(self, n: int = -1) -> bytes:
            if n == -1:
                result = self._data[self._pos :]
                self._pos = len(self._data)
            else:
                result = self._data[self._pos : self._pos + n]
                self._pos += len(result)
            return result

        def readinto(self, _b: bytearray | memoryview) -> int:
            data = self.read(len(_b))
            n = len(data)
            _b[:n] = data
            return n

        def readable(self) -> bool:
            return True

        def seekable(self) -> bool:
            return False

        def seek(self, _offset: int, _whence: int = 0) -> int:
            raise OSError("not seekable")

        def tell(self) -> int:
            return self._pos

    respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(502, json={"detail": "bad gateway"})
    )

    stream = NonSeekableStream(b"data")
    with Isola(base_url="http://localhost:8080") as client, pytest.raises(BadGatewayError):
        client._api.request("GET", "/v1/sandboxes", content=stream)


@respx.mock
def test_no_retry_for_non_seekable_stream_on_connection_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)

    class NonSeekableStream(io.RawIOBase):
        def read(self, _n: int = -1) -> bytes:
            return b""

        def readinto(self, _b: bytearray | memoryview) -> int:
            return 0

        def readable(self) -> bool:
            return True

        def seekable(self) -> bool:
            return False

        def seek(self, _offset: int, _whence: int = 0) -> int:
            raise OSError("not seekable")

        def tell(self) -> int:
            return 0

    route = respx.get("http://localhost:8080/v1/sandboxes")
    route.mock(side_effect=httpx.ConnectError("connect failed"))

    stream = NonSeekableStream()
    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIConnectionError):
        client._api.request("GET", "/v1/sandboxes", content=stream)

    assert route.call_count == 1
