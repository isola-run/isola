from __future__ import annotations

import uuid

import pytest

import time

from isola import BadRequestError, FileWriteResult, IsolaError, NotFoundError, Sandbox


def _unique_path(prefix: str, ext: str = ".txt") -> str:
    return f"/tmp/{prefix}_{uuid.uuid4().hex}{ext}"


@pytest.mark.smoke
def test_write_and_read_text(shared_sandbox: Sandbox) -> None:
    path = _unique_path("write_text")
    content = b"hello, isola sandbox!"

    shared_sandbox.filesystem.write(path, content)
    result = shared_sandbox.filesystem.read(path)

    assert result == content


def test_write_and_read_binary(shared_sandbox: Sandbox) -> None:
    path = _unique_path("write_binary", ext=".bin")
    content = bytes(range(256)) + b"\x00\xff\x80\xfe\x01"

    shared_sandbox.filesystem.write(path, content)
    result = shared_sandbox.filesystem.read(path)

    assert result == content


def test_overwrite_file(shared_sandbox: Sandbox) -> None:
    path = _unique_path("overwrite")
    original = b"original content"
    updated = b"updated content"

    shared_sandbox.filesystem.write(path, original)
    assert shared_sandbox.filesystem.read(path) == original

    shared_sandbox.filesystem.write(path, updated)
    assert shared_sandbox.filesystem.read(path) == updated


def test_read_nonexistent_file(shared_sandbox: Sandbox) -> None:
    path = f"/tmp/nonexistent_{uuid.uuid4().hex}.txt"

    with pytest.raises(NotFoundError):
        shared_sandbox.filesystem.read(path)


def test_write_nested_path(shared_sandbox: Sandbox) -> None:
    unique = uuid.uuid4().hex
    path = f"/tmp/{unique}/a/b/c/deep.txt"
    content = b"deeply nested file content"

    shared_sandbox.filesystem.write(path, content)
    result = shared_sandbox.filesystem.read(path)

    assert result == content


def test_large_file(shared_sandbox: Sandbox) -> None:
    path = _unique_path("large_file", ext=".bin")
    content = b"x" * (1024 * 1024)  # 1 MB

    shared_sandbox.filesystem.write(path, content)
    result = shared_sandbox.filesystem.read(path)

    assert len(result) == len(content)
    assert result == content


def test_write_returns_metadata(shared_sandbox: Sandbox) -> None:
    path = _unique_path("metadata")
    content = b"metadata test content"

    result = shared_sandbox.filesystem.write(path, content)

    assert isinstance(result, FileWriteResult)
    assert result.absolute_path == path
    assert result.bytes_written == len(content)


def test_special_characters_in_filename(shared_sandbox: Sandbox) -> None:
    unique = uuid.uuid4().hex
    path = f"/tmp/test file (1) {unique}.txt"
    content = b"special chars in filename"

    shared_sandbox.filesystem.write(path, content)
    result = shared_sandbox.filesystem.read(path)

    assert result == content


def test_read_directory_returns_error(shared_sandbox: Sandbox) -> None:
    """Reading a directory (not a regular file) should return 400.

    The sidecar rejects non-regular files to prevent blocking on FIFOs/devices.
    """
    with pytest.raises((BadRequestError, IsolaError)):
        shared_sandbox.filesystem.read("/tmp")


def test_file_written_is_executable_by_command(shared_sandbox: Sandbox) -> None:
    """A file uploaded via filesystem.write() is readable and executable by commands.

    Tests cross-subsystem consistency: filesystem handler writes via /proc/<pid>/root,
    command handler runs via nsenter --root. Both must see the same filesystem.
    """
    unique = uuid.uuid4().hex
    path = f"/tmp/test_{unique}.sh"
    shared_sandbox.filesystem.write(path, b"#!/bin/sh\necho cross_subsystem_works\n")

    cmd = shared_sandbox.commands.run(cmd="sh", args=[path])

    deadline = time.monotonic() + 15
    while time.monotonic() < deadline:
        if cmd.exit_code() is not None:
            break
        time.sleep(0.3)

    assert cmd.exit_code() == 0
    with cmd.stdout() as stream:
        output = "".join(chunk for chunk in stream)
    assert "cross_subsystem_works" in output
