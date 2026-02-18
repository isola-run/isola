from __future__ import annotations

import copy

import pytest


@pytest.fixture
def sandbox_response() -> dict[str, object]:
    return {
        "id": "sandbox-123",
        "status": "running",
        "creationTimestamp": "2026-02-18T00:00:00Z",
        "podTemplate": {
            "container": {
                "image": "python:3.12",
                "command": ["sleep", "infinity"],
                "resources": {
                    "limits": {
                        "cpu": "500m",
                        "memory": "1Gi",
                        "ephemeralStorage": "2Gi",
                    },
                    "requests": {
                        "cpu": "500m",
                        "memory": "1Gi",
                        "ephemeralStorage": "2Gi",
                    },
                },
            }
        },
        "network": {
            "allowInternetEgress": True,
        },
        "activeDeadlineSeconds": 3600,
    }


@pytest.fixture
def sandbox_summary_response() -> dict[str, object]:
    return {
        "sandboxes": [
            {
                "id": "sandbox-123",
                "status": "running",
                "creationTimestamp": "2026-02-18T00:00:00Z",
            },
            {
                "id": "sandbox-456",
                "status": "creating",
                "creationTimestamp": "2026-02-18T00:01:00Z",
            },
        ]
    }


@pytest.fixture
def sandbox_response_copy(sandbox_response: dict[str, object]) -> dict[str, object]:
    return copy.deepcopy(sandbox_response)
