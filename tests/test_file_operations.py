"""
Tests for file upload and download operations.
"""
from __future__ import annotations

import pytest

from client.isola_client import IsolaClient, IsolaError


class TestFileUpload:
    """Test file upload operations."""

    @pytest.mark.smoke
    def test_generate_upload_url(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Generate a presigned upload URL."""
        result = isola_client.generate_upload_url(
            sandbox_id=sandbox["id"],
            path="/tmp",
            filename="test.txt",
        )

        # Should return an upload URL and upload ID
        assert "uploadUrl" in result or "upload_url" in result
        assert "uploadId" in result or "upload_id" in result

    def test_generate_upload_url_with_content_type(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Generate upload URL with specific content type."""
        result = isola_client.generate_upload_url(
            sandbox_id=sandbox["id"],
            path="/tmp",
            filename="data.json",
            content_type="application/json",
        )

        assert "uploadUrl" in result or "upload_url" in result

    def test_generate_upload_url_nonexistent_sandbox(
        self,
        isola_client: IsolaClient,
    ) -> None:
        """Generate upload URL for non-existent sandbox should fail."""
        with pytest.raises(IsolaError) as exc_info:
            isola_client.generate_upload_url(
                sandbox_id="nonexistent-id",
                path="/tmp",
                filename="test.txt",
            )

        assert exc_info.value.status_code == 404


class TestMultipartFileUpload:
    """Test multipart file upload (streaming upload to agent)."""

    @pytest.mark.smoke
    def test_upload_small_file(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Upload a small file via multipart form."""
        content = b"Hello from multipart upload test!"
        path = "/tmp/multipart-test.txt"

        result = isola_client.upload_file_multipart(
            sandbox_id=sandbox["id"],
            path=path,
            content=content,
            filename="test.txt",
        )

        assert result.get("success") is True
        assert result.get("path") == path

        # Verify file exists and has correct content
        verify_result = isola_client.execute_command(
            sandbox["id"],
            f"cat {path}",
        )
        assert verify_result["exitCode"] == 0
        assert "Hello from multipart upload test!" in verify_result["stdout"]

    def test_upload_binary_file(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Upload a binary file via multipart form."""
        # Create 1KB of pseudo-random binary data
        content = bytes(range(256)) * 4  # 1024 bytes
        path = "/tmp/binary-multipart.bin"

        result = isola_client.upload_file_multipart(
            sandbox_id=sandbox["id"],
            path=path,
            content=content,
            filename="data.bin",
        )

        assert result.get("success") is True

        # Verify file size
        verify_result = isola_client.execute_command(
            sandbox["id"],
            f"wc -c < {path}",
        )
        assert verify_result["exitCode"] == 0
        assert "1024" in verify_result["stdout"]

    def test_upload_to_nonexistent_sandbox(
        self,
        isola_client: IsolaClient,
    ) -> None:
        """Upload to non-existent sandbox should fail."""
        with pytest.raises(IsolaError) as exc_info:
            isola_client.upload_file_multipart(
                sandbox_id="nonexistent-id",
                path="/tmp/test.txt",
                content=b"test",
            )

        assert exc_info.value.status_code == 404


class TestFileOperationsViaCommands:
    """Test file operations using command execution."""

    @pytest.mark.smoke
    def test_create_and_read_file(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Create a file via command and read it back."""
        content = "Hello from e2e test!"

        # Create file
        create_result = isola_client.execute_command(
            sandbox["id"],
            f"echo '{content}' > /tmp/e2e-test.txt",
        )
        assert create_result["exitCode"] == 0

        # Read file
        read_result = isola_client.execute_command(
            sandbox["id"],
            "cat /tmp/e2e-test.txt",
        )
        assert read_result["exitCode"] == 0
        assert content in read_result["stdout"]

    def test_create_directory_and_file(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Create a directory structure and files."""
        # Create directory
        mkdir_result = isola_client.execute_command(
            sandbox["id"],
            "mkdir -p /tmp/e2e-test-dir/subdir",
        )
        assert mkdir_result["exitCode"] == 0

        # Create file in directory
        create_result = isola_client.execute_command(
            sandbox["id"],
            "echo 'nested content' > /tmp/e2e-test-dir/subdir/file.txt",
        )
        assert create_result["exitCode"] == 0

        # Verify structure
        ls_result = isola_client.execute_command(
            sandbox["id"],
            "ls -la /tmp/e2e-test-dir/subdir/",
        )
        assert ls_result["exitCode"] == 0
        assert "file.txt" in ls_result["stdout"]

    def test_file_permissions(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Test file permission operations."""
        # Create file
        isola_client.execute_command(
            sandbox["id"],
            "echo 'test' > /tmp/perm-test.txt",
        )

        # Change permissions
        chmod_result = isola_client.execute_command(
            sandbox["id"],
            "chmod 755 /tmp/perm-test.txt",
        )
        assert chmod_result["exitCode"] == 0

        # Verify permissions
        ls_result = isola_client.execute_command(
            sandbox["id"],
            "ls -la /tmp/perm-test.txt",
        )
        assert ls_result["exitCode"] == 0
        assert "rwx" in ls_result["stdout"]

    def test_large_file_content(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Test handling of larger file content."""
        # Create a file with multiple lines
        lines = 100
        create_result = isola_client.execute_command(
            sandbox["id"],
            f"seq 1 {lines} > /tmp/large-test.txt",
        )
        assert create_result["exitCode"] == 0

        # Count lines
        wc_result = isola_client.execute_command(
            sandbox["id"],
            "wc -l /tmp/large-test.txt",
        )
        assert wc_result["exitCode"] == 0
        assert str(lines) in wc_result["stdout"]

    def test_binary_file_handling(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Test handling of binary files."""
        # Create a small binary file using dd
        create_result = isola_client.execute_command(
            sandbox["id"],
            "dd if=/dev/urandom of=/tmp/binary-test.bin bs=1024 count=1 2>/dev/null",
        )
        assert create_result["exitCode"] == 0

        # Verify it exists and has content
        ls_result = isola_client.execute_command(
            sandbox["id"],
            "ls -la /tmp/binary-test.bin",
        )
        assert ls_result["exitCode"] == 0
        assert "binary-test.bin" in ls_result["stdout"]
