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

"""Tests for streaming behavior during output idle gaps.

Verifies that stdout/stderr streams survive idle periods longer than
the per-write deadline (10s) and server WriteTimeout (45s gateway, 75s sidecar).
"""

from __future__ import annotations

import time

import pytest

from isola import Sandbox


@pytest.mark.timeout(45)
def test_stream_survives_15s_idle_gap(session_sandbox: Sandbox) -> None:
    """Stream survives a 15s output gap (> 10s per-write deadline).

    If the DeadlineWriter's 10s timeout killed idle streams, the SDK would
    see a connection error after 10s of silence and need to reconnect.
    This test verifies that doesn't happen — all output arrives in a single
    clean stream.
    """
    cmd = session_sandbox.commands.spawn(
        "sh", "-c", "echo before; sleep 15; echo after",
    )

    chunks: list[str] = []
    for chunk in cmd.stdout:
        chunks.append(chunk)

    output = "".join(chunks)
    assert "before\n" in output
    assert "after\n" in output
    assert cmd.wait() == 0


@pytest.mark.timeout(45)
def test_stream_survives_20s_idle_gap(session_sandbox: Sandbox) -> None:
    """Stream survives a 20s output gap — well above the 10s per-write deadline."""
    cmd = session_sandbox.commands.spawn(
        "sh", "-c", "echo first; sleep 20; echo second",
    )

    output = cmd.stdout.read()
    assert "first\n" in output
    assert "second\n" in output
    assert cmd.wait() == 0


@pytest.mark.timeout(120)
def test_stream_survives_50s_idle_gap(session_sandbox: Sandbox) -> None:
    """Stream survives a 50s output gap (> 45s gateway WriteTimeout).

    The gateway's WriteTimeout is 45s. If it killed streaming responses,
    a command producing no output for 50s would lose the connection.
    """
    cmd = session_sandbox.commands.spawn(
        "sh", "-c", "echo start; sleep 50; echo end",
    )

    output = cmd.stdout.read()
    assert "start\n" in output
    assert "end\n" in output
    assert cmd.wait() == 0


@pytest.mark.timeout(180)
def test_stream_survives_80s_idle_gap(session_sandbox: Sandbox) -> None:
    """Stream survives an 80s output gap (> 75s sidecar WriteTimeout).

    The sidecar's WriteTimeout is 75s. This test verifies whether an idle
    stream can survive past that threshold.
    """
    cmd = session_sandbox.commands.spawn(
        "sh", "-c", "echo start; sleep 80; echo end",
    )

    output = cmd.stdout.read()
    assert "start\n" in output
    assert "end\n" in output
    assert cmd.wait() == 0


@pytest.mark.timeout(60)
def test_no_output_then_burst(session_sandbox: Sandbox) -> None:
    """Command that produces zero output for 30s then a burst.

    Tests the case where DeadlineWriter.Write is never called (no initial
    output), so no per-write deadline is ever set. Only the server-level
    WriteTimeout applies.
    """
    cmd = session_sandbox.commands.spawn(
        "sh", "-c", "sleep 30; echo burst",
    )

    output = cmd.stdout.read()
    assert "burst\n" in output
    assert cmd.wait() == 0


@pytest.mark.timeout(45)
def test_multiple_idle_gaps(session_sandbox: Sandbox) -> None:
    """Multiple 12s idle gaps in sequence — each exceeds the 10s per-write deadline.

    If each gap burned a reconnect attempt, multiple gaps could exhaust
    the SDK's reconnect budget (5 reconnects).
    """
    cmd = session_sandbox.commands.spawn(
        "sh",
        "-c",
        "echo a; sleep 12; echo b; sleep 12; echo c; sleep 12; echo d",
    )

    output = cmd.stdout.read()
    assert "a\n" in output
    assert "b\n" in output
    assert "c\n" in output
    assert "d\n" in output
    assert cmd.wait() == 0


@pytest.mark.timeout(10)
def test_clean_eof_no_reconnect(session_sandbox: Sandbox) -> None:
    """A fast command completes without triggering any reconnect logic.

    Baseline: verifies that a clean stream close works as expected.
    """
    cmd = session_sandbox.commands.spawn("echo", "quick")

    output = cmd.stdout.read()
    assert output == "quick\n"
    assert cmd.wait() == 0
