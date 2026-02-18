from __future__ import annotations

import json

import httpx
import respx

from isola import Isola, SandboxStatus


@respx.mock
def test_create_sandbox_maps_flat_resources(sandbox_response_copy: dict[str, object]) -> None:
    create_route = respx.post("http://localhost:8080/sandboxes").mock(
        return_value=httpx.Response(201, json=sandbox_response_copy)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.create(
            image="python:3.12",
            command=["sleep", "infinity"],
            env={"KEY": "value"},
            cpu="500m",
            memory="1Gi",
            disk="2Gi",
            active_deadline_seconds=3600,
        )

    assert sandbox.id == "sandbox-123"
    assert sandbox.status == SandboxStatus.RUNNING

    payload = json.loads(create_route.calls[0].request.content)
    assert payload["podTemplate"]["container"]["resources"] == {
        "limits": {"cpu": "500m", "memory": "1Gi", "ephemeralStorage": "2Gi"},
        "requests": {"cpu": "500m", "memory": "1Gi", "ephemeralStorage": "2Gi"},
    }


@respx.mock
def test_list_sandboxes(sandbox_summary_response: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes").mock(
        return_value=httpx.Response(200, json=sandbox_summary_response)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandboxes = client.sandboxes.list()

    assert [s.id for s in sandboxes] == ["sandbox-123", "sandbox-456"]


@respx.mock
def test_get_and_delete_sandbox(sandbox_response_copy: dict[str, object]) -> None:
    respx.get("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(200, json=sandbox_response_copy)
    )
    delete_route = respx.delete("http://localhost:8080/sandboxes/sandbox-123").mock(
        return_value=httpx.Response(204)
    )

    with Isola(base_url="http://localhost:8080") as client:
        sandbox = client.sandboxes.get("sandbox-123")
        sandbox.delete()

    assert sandbox.id == "sandbox-123"
    assert delete_route.called
