from __future__ import annotations

import httpx
import pytest
import respx

from isola import AsyncIsola, Isola, NotFoundError
from isola import ConnectionError as IsolaConnectionError


@respx.mock
def test_sync_client_uses_base_url_with_trailing_slash() -> None:
    route = respx.get("http://localhost:8080/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

    with Isola(base_url="http://localhost:8080/") as client:
        assert client.sandboxes.list() == []

    assert route.called


@pytest.mark.asyncio
@respx.mock
async def test_async_client_list() -> None:
    route = respx.get("http://localhost:8080/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

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
    assert exc_info.value.detail == "sandbox not found"


@respx.mock
def test_transport_error_mapping() -> None:
    route = respx.get("http://localhost:8080/sandboxes")
    route.mock(side_effect=httpx.ConnectError("connect failed"))

    with Isola(base_url="http://localhost:8080") as client, pytest.raises(IsolaConnectionError) as exc_info:
        client.sandboxes.list()

    assert exc_info.value.status == 0
    assert "connect failed" in exc_info.value.detail
