from __future__ import annotations

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
from isola._client import MAX_RETRIES


@respx.mock
def test_sync_client_uses_base_url_with_trailing_slash() -> None:
    route = respx.get("http://localhost:8080/sandboxes").mock(return_value=httpx.Response(200, json={"sandboxes": []}))

    with Isola(base_url="http://localhost:8080/") as client:
        assert client.sandboxes.list() == []

    assert route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_client_list() -> None:
    route = respx.get("http://localhost:8080/sandboxes").mock(return_value=httpx.Response(200, json={"sandboxes": []}))

    async with AsyncIsola(base_url="http://localhost:8080") as client:
        sandboxes = await client.sandboxes.list()

    assert sandboxes == []
    assert route.called


@respx.mock
def test_error_mapping_for_problem_details() -> None:
    respx.get("http://localhost:8080/sandboxes/missing").mock(
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
    route = respx.get("http://localhost:8080/sandboxes")
    route.mock(side_effect=httpx.ConnectError("connect failed"))

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIConnectionError) as exc_info:
        client.sandboxes.list()

    assert "connect failed" in exc_info.value.message
    assert "GET /sandboxes" in exc_info.value.message


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
def test_error_status_code_mapping(
    status_code: int, exc_type: type[APIError], monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    respx.get("http://localhost:8080/sandboxes/bad").mock(
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
    respx.get("http://localhost:8080/sandboxes/bad").mock(
        return_value=httpx.Response(503, json={"detail": "service unavailable"})
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIError) as exc_info:
        client.sandboxes.get("bad")

    assert type(exc_info.value) is APIError
    assert exc_info.value.status_code == 503


@respx.mock
def test_invalid_json_response_raises_api_error() -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, content=b"<html>not json</html>")
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIError) as exc_info:
        client.sandboxes.get("sandbox-123")

    assert exc_info.value.message == "200: invalid response payload"


@respx.mock
def test_json_schema_mismatch_raises_api_error() -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
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
    respx.get("http://localhost:8080/sandboxes").mock(return_value=httpx.Response(200, json={"sandboxes": []}))

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
    respx.get("http://localhost:8080/sandboxes").mock(
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
    respx.get("http://localhost:8080/sandboxes").mock(
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
    respx.get("http://localhost:8080/sandboxes").mock(
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
    route = respx.get("http://localhost:8080/sandboxes/bad").mock(
        return_value=httpx.Response(404, json={"detail": "not found"})
    )
    with Isola(base_url="http://localhost:8080") as client, pytest.raises(NotFoundError):
        client.sandboxes.get("bad")
    assert route.call_count == 1


@respx.mock
def test_no_retry_on_400(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    route = respx.get("http://localhost:8080/sandboxes/bad").mock(
        return_value=httpx.Response(400, json={"detail": "bad request"})
    )
    with Isola(base_url="http://localhost:8080") as client, pytest.raises(BadRequestError):
        client.sandboxes.get("bad")
    assert route.call_count == 1


@respx.mock
def test_exhausts_retries_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    route = respx.get("http://localhost:8080/sandboxes").mock(
        return_value=httpx.Response(502, json={"detail": "bad gateway"})
    )
    with Isola(base_url="http://localhost:8080") as client, pytest.raises(BadGatewayError):
        client.sandboxes.list()
    assert route.call_count == 1 + MAX_RETRIES


@respx.mock
def test_exhausts_retries_on_connection_error(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("isola._client.time.sleep", lambda _: None)
    route = respx.get("http://localhost:8080/sandboxes")
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
    respx.get("http://localhost:8080/sandboxes").mock(
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
    respx.get("http://localhost:8080/sandboxes").mock(
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
    route = respx.get("http://localhost:8080/sandboxes/bad").mock(
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
    route = respx.get("http://localhost:8080/sandboxes").mock(
        return_value=httpx.Response(502, json={"detail": "bad gateway"})
    )
    async with AsyncIsola(base_url="http://localhost:8080") as client:
        with pytest.raises(BadGatewayError):
            await client.sandboxes.list()
    assert route.call_count == 1 + MAX_RETRIES
