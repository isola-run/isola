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

import uuid

import pytest

from isola import BadRequestError, IsolaError, NotFoundError, Sandbox



def _unique_path(prefix: str, ext: str = ".txt") -> str:
    return f"/tmp/{prefix}_{uuid.uuid4().hex}{ext}"


def test_write_and_read_text(session_sandbox: Sandbox) -> None:
    path = _unique_path("write_text")
    content = b"hello, isola sandbox!"

    session_sandbox.filesystem.write(path, content)
    result = session_sandbox.filesystem.read(path)

    assert result == content


def test_write_and_read_binary(session_sandbox: Sandbox) -> None:
    path = _unique_path("write_binary", ext=".bin")
    content = bytes(range(256)) + b"\x00\xff\x80\xfe\x01"

    session_sandbox.filesystem.write(path, content)
    result = session_sandbox.filesystem.read(path)

    assert result == content


def test_overwrite_file(session_sandbox: Sandbox) -> None:
    path = _unique_path("overwrite")
    original = b"original content"
    updated = b"updated content"

    session_sandbox.filesystem.write(path, original)
    assert session_sandbox.filesystem.read(path) == original

    session_sandbox.filesystem.write(path, updated)
    assert session_sandbox.filesystem.read(path) == updated


def test_read_nonexistent_file(session_sandbox: Sandbox) -> None:
    path = f"/tmp/nonexistent_{uuid.uuid4().hex}.txt"

    with pytest.raises(NotFoundError):
        session_sandbox.filesystem.read(path)


def test_write_nested_path(session_sandbox: Sandbox) -> None:
    unique = uuid.uuid4().hex
    path = f"/tmp/{unique}/a/b/c/deep.txt"
    content = b"deeply nested file content"

    session_sandbox.filesystem.write(path, content)
    result = session_sandbox.filesystem.read(path)

    assert result == content


def test_large_file(session_sandbox: Sandbox) -> None:
    path = _unique_path("large_file", ext=".bin")
    content = b"x" * (1024 * 1024)  # 1 MB

    session_sandbox.filesystem.write(path, content)
    result = session_sandbox.filesystem.read(path)

    assert len(result) == len(content)
    assert result == content



def test_special_characters_in_filename(session_sandbox: Sandbox) -> None:
    unique = uuid.uuid4().hex
    path = f"/tmp/test file (1) {unique}.txt"
    content = b"special chars in filename"

    session_sandbox.filesystem.write(path, content)
    result = session_sandbox.filesystem.read(path)

    assert result == content


def test_read_directory_returns_error(session_sandbox: Sandbox) -> None:
    """Reading a directory (not a regular file) should return 400.

    The sidecar rejects non-regular files to prevent blocking on FIFOs/devices.
    """
    with pytest.raises((BadRequestError, IsolaError)):
        session_sandbox.filesystem.read("/tmp")


def test_file_written_is_executable_by_command(session_sandbox: Sandbox) -> None:
    """A file uploaded via filesystem.write() is readable and executable by commands."""

    unique = uuid.uuid4().hex
    path = f"/tmp/test_{unique}.sh"
    session_sandbox.filesystem.write(path, b"#!/bin/sh\necho cross_subsystem_works\n")

    result = session_sandbox.commands.run("sh", path)
    assert result.exit_code == 0
    assert "cross_subsystem_works" in result.stdout


def test_relative_path_resolved(session_sandbox: Sandbox) -> None:
    """A relative path is resolved against the container's cwd."""
    unique = uuid.uuid4().hex
    filename = f"relative_{unique}.txt"
    content = b"relative path test"

    session_sandbox.filesystem.write(filename, content)

    read_back = session_sandbox.filesystem.read(filename)
    assert read_back == content


def test_write_parent_is_file_raises_error(session_sandbox: Sandbox) -> None:
    """Writing a file whose parent path component is itself a file should raise an error.

    mkdirAllChown returns an error when it encounters a regular file where it
    expects a directory, causing the sidecar to return a 500.
    """
    unique = uuid.uuid4().hex
    blocker_path = f"/tmp/{unique}/blocker"

    # Create a regular file at the blocker path
    session_sandbox.filesystem.write(blocker_path, b"I am a file, not a directory")

    # Attempt to write nested under the file (treating it as a directory)
    nested_path = f"{blocker_path}/nested.txt"
    with pytest.raises(IsolaError):
        session_sandbox.filesystem.write(nested_path, b"this should fail")


def test_empty_file_write(session_sandbox: Sandbox) -> None:
    """Writing zero bytes creates an empty file."""
    path = _unique_path("empty")

    session_sandbox.filesystem.write(path, b"")

    assert session_sandbox.filesystem.read(path) == b""


def test_file_ownership_matches_container(session_sandbox: Sandbox) -> None:
    """A file written via the API is owned by the container's UID/GID.

    The sidecar resolves the container's uid/gid via /proc/<pid>/status and
    applies os.Chown to the written file.
    """
    path = _unique_path("ownership")
    session_sandbox.filesystem.write(path, b"ownership test")

    uid_result = session_sandbox.commands.run("id", "-u")
    gid_result = session_sandbox.commands.run("id", "-g")
    stat_result = session_sandbox.commands.run("stat", "-c", "%u %g", path)

    uid = uid_result.stdout.strip()
    gid = gid_result.stdout.strip()
    file_uid, file_gid = stat_result.stdout.strip().split()

    assert file_uid == uid
    assert file_gid == gid


def test_command_written_file_readable_via_api(session_sandbox: Sandbox) -> None:
    """A file written by a command inside the sandbox is readable via the filesystem API.

    Verifies nsenter/proc cross-subsystem consistency: the command handler
    (nsenter --root) and the filesystem handler (/proc/<pid>/root) share the same view.
    """
    unique = uuid.uuid4().hex
    path = f"/tmp/cmd_written_{unique}.txt"
    expected = f"written_{unique}"

    result = session_sandbox.commands.run(
        "sh", "-c", f"printf '%s' {expected} > {path}",
    )
    assert result.exit_code == 0

    content = session_sandbox.filesystem.read(path)
    assert content == expected.encode()


def test_container_param_on_filesystem(session_sandbox: Sandbox) -> None:
    """Explicitly targeting the primary container by name should work."""
    path = _unique_path("container_param")
    content = b"container param test"

    session_sandbox.filesystem.write(path, content, container="sandbox0")

    read_back = session_sandbox.filesystem.read(path, container="sandbox0")
    assert read_back == content
