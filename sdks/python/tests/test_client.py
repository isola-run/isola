from __future__ import annotations

import httpx
import pytest
import respx

from isola import (
    APIConnectionError,
    AsyncIsola,
    BadGatewayError,
    BadRequestError,
    ConflictError,
    InternalError,
    Isola,
    IsolaError,
    NotFoundError,
    ValidationError,
)


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

    assert exc_info.value.status == 404
    assert "sandbox not found" in exc_info.value.detail


@respx.mock
def test_transport_error_mapping() -> None:
    route = respx.get("http://localhost:8080/sandboxes")
    route.mock(side_effect=httpx.ConnectError("connect failed"))

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(APIConnectionError) as exc_info:
        client.sandboxes.list()

    assert exc_info.value.status == 0
    assert "connect failed" in exc_info.value.detail
    assert "GET /sandboxes" in exc_info.value.detail


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
def test_error_status_code_mapping(status_code: int, exc_type: type[IsolaError]) -> None:
    respx.get("http://localhost:8080/sandboxes/bad").mock(
        return_value=httpx.Response(
            status_code,
            json={"status": status_code, "detail": "test error"},
        )
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(exc_type) as exc_info:
        client.sandboxes.get("bad")

    assert exc_info.value.status == status_code
    assert "test error" in exc_info.value.detail


@respx.mock
def test_unknown_status_code_raises_base_isola_error() -> None:
    respx.get("http://localhost:8080/sandboxes/bad").mock(
        return_value=httpx.Response(503, json={"detail": "service unavailable"})
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaError) as exc_info:
        client.sandboxes.get("bad")

    assert type(exc_info.value) is IsolaError
    assert exc_info.value.status == 503


@respx.mock
def test_invalid_json_response_raises_isola_error() -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, content=b"<html>not json</html>")
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaError) as exc_info:
        client.sandboxes.get("sandbox-123")

    assert exc_info.value.detail == "invalid response payload"


@respx.mock
def test_json_schema_mismatch_raises_isola_error() -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json={"unexpected": "schema"})
    )

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaError) as exc_info:
        client.sandboxes.get("sandbox-123")

    assert exc_info.value.detail == "invalid response payload"


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
