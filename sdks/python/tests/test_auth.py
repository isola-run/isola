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

import httpx
import pytest
import respx

from isola import AsyncIsola, Isola

API_KEY = "sk-test-123"
EXPECTED_HEADER = "Bearer sk-test-123"


@pytest.fixture(autouse=True)
def _no_api_key_env(monkeypatch: pytest.MonkeyPatch) -> None:
    """Keep the developer's real ISOLA_API_KEY out of these assertions."""
    monkeypatch.delenv("ISOLA_API_KEY", raising=False)


# --- Authorization header present on every request shape ---


@respx.mock
def test_auth_header_on_json_request() -> None:
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

    with Isola(url="http://localhost:8080", api_key=API_KEY) as client:
        client.sandboxes.list()

    assert route.calls[0].request.headers["Authorization"] == EXPECTED_HEADER


@respx.mock
def test_auth_header_on_octet_stream_upload(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    write_route = respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/filesystem").mock(
        return_value=httpx.Response(204)
    )

    with Isola(url="http://localhost:8080", api_key=API_KEY) as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.filesystem.write("/workspace/file.txt", b"content", container="worker")

    assert write_route.calls[0].request.content == b"content"
    assert write_route.calls[0].request.headers["Authorization"] == EXPECTED_HEADER


@respx.mock
def test_auth_header_on_sse_stream(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/v1/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    respx.post("http://localhost:8080/v1/sandboxes/sandbox-123/commands").mock(
        return_value=httpx.Response(202, json={"id": "cmd-1"})
    )
    stdout_route = respx.get("http://localhost:8080/v1/sandboxes/sandbox-123/commands/cmd-1/stdout").mock(
        return_value=httpx.Response(
            200, content=b"data: hello\ndata: \nid: 1\n\n", headers={"content-type": "text/event-stream"}
        )
    )

    with Isola(url="http://localhost:8080", api_key=API_KEY) as client:
        sandbox = client.sandboxes.get("sandbox-123")
        cmd = sandbox.commands.spawn("echo", "hello")
        assert "".join(cmd.stdout) == "hello\n"

    assert stdout_route.calls[0].request.headers["Authorization"] == EXPECTED_HEADER


@pytest.mark.asyncio
@respx.mock
async def test_async_auth_header_on_json_request() -> None:
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

    async with AsyncIsola(url="http://localhost:8080", api_key=API_KEY) as client:
        await client.sandboxes.list()

    assert route.calls[0].request.headers["Authorization"] == EXPECTED_HEADER


# --- No Authorization header when no key is configured ---


@respx.mock
def test_no_auth_header_when_key_absent() -> None:
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

    with Isola(url="http://localhost:8080") as client:
        client.sandboxes.list()

    assert "Authorization" not in route.calls[0].request.headers


@pytest.mark.asyncio
@respx.mock
async def test_async_no_auth_header_when_key_absent() -> None:
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

    async with AsyncIsola(url="http://localhost:8080") as client:
        await client.sandboxes.list()

    assert "Authorization" not in route.calls[0].request.headers


# --- Env-var fallback (ISOLA_API_KEY), mirroring ISOLA_URL ---


@respx.mock
def test_api_key_from_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("ISOLA_API_KEY", API_KEY)
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

    with Isola(url="http://localhost:8080") as client:
        client.sandboxes.list()

    assert route.calls[0].request.headers["Authorization"] == EXPECTED_HEADER


@respx.mock
def test_explicit_api_key_overrides_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("ISOLA_API_KEY", "sk-from-env")
    route = respx.get("http://localhost:8080/v1/sandboxes").mock(
        return_value=httpx.Response(200, json={"sandboxes": []})
    )

    with Isola(url="http://localhost:8080", api_key=API_KEY) as client:
        client.sandboxes.list()

    assert route.calls[0].request.headers["Authorization"] == EXPECTED_HEADER
