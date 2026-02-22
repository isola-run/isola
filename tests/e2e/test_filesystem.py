from __future__ import annotations

import uuid

import pytest

from isola import BadRequestError, FileWriteResult, IsolaError, NotFoundError, Sandbox



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


def test_write_returns_metadata(session_sandbox: Sandbox) -> None:
    path = _unique_path("metadata")
    content = b"metadata test content"

    result = session_sandbox.filesystem.write(path, content)

    assert isinstance(result, FileWriteResult)
    assert result.absolute_path == path
    assert result.bytes_written == len(content)


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
    """A file uploaded via filesystem.write() is readable and executable by commands.

    Tests cross-subsystem consistency: filesystem handler writes via /proc/<pid>/root,
    command handler runs via nsenter --root. Both must see the same filesystem.
    """
    unique = uuid.uuid4().hex
    path = f"/tmp/test_{unique}.sh"
    session_sandbox.filesystem.write(path, b"#!/bin/sh\necho cross_subsystem_works\n")

    result = session_sandbox.commands.run("sh", path)
    assert result.exit_code == 0
    assert "cross_subsystem_works" in result.stdout
