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

import copy

import pytest


@pytest.fixture
def sandbox_response() -> dict[str, object]:
    return {
        "id": "sandbox-123",
        "status": "running",
        "creationTimestamp": "2026-02-18T00:00:00Z",
        "podTemplate": {
            "containers": [{
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
            }]
        },
        "network": {
            "allowInternetEgress": True,
        },
        "timeoutSeconds": 3600,
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


@pytest.fixture
def rootfs_snapshot_response() -> dict[str, object]:
    return {
        "id": "snapshot-123",
        "sandboxId": "sandbox-123",
        "snapshotName": "my-snapshot",
        "containerName": "worker",
        "timeoutSeconds": 300,
        "ttlSecondsAfterFinished": 300,
        "status": "complete",
        "creationTimestamp": "2026-02-18T00:00:00Z",
    }


@pytest.fixture
def rootfs_snapshot_response_copy(rootfs_snapshot_response: dict[str, object]) -> dict[str, object]:
    return copy.deepcopy(rootfs_snapshot_response)
