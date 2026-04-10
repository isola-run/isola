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

import time

import pytest

from isola import Container, Isola, Sandbox

from utils import wait_for_running


@pytest.fixture()
def multi_container_sandbox(isola_client: Isola, sandbox_factory) -> Sandbox:
    """Create a two-container sandbox with a Python HTTP server and an Alpine sidecar."""
    sb = sandbox_factory(
        containers=[
            Container(
                name="server",
                image="python:3.12-slim",
                command=["python3", "-m", "http.server", "8080"],
            ),
            Container(name="client", image="alpine:3.21"),
        ],
    )
    return wait_for_running(isola_client, sb.id)


@pytest.mark.timeout(90)
def test_cross_container_http(multi_container_sandbox: Sandbox) -> None:
    """Containers in the same sandbox share a network namespace; HTTP over 127.0.0.1 works."""
    sb = multi_container_sandbox
    # Give the HTTP server a moment to bind
    time.sleep(2)

    result = sb.commands.run(
        "wget", "-qO-", "http://127.0.0.1:8080",
        container="client",
        timeout_seconds=10,
    )

    assert result.exit_code == 0, f"wget failed: {result.stderr}"
    assert "Directory listing" in result.stdout


@pytest.mark.timeout(90)
def test_command_targets_correct_container(multi_container_sandbox: Sandbox) -> None:
    """Commands run in the targeted container, not any other."""
    sb = multi_container_sandbox

    # Python is available in server (python:3.12-slim) but not in client (alpine)
    server_result = sb.commands.run("python3", "--version", container="server")
    assert server_result.exit_code == 0

    client_result = sb.commands.run("python3", "--version", container="client")
    assert client_result.exit_code != 0
