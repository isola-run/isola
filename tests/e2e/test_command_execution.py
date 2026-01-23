"""
Tests for command execution in sandboxes.
"""
from __future__ import annotations

import pytest

from client.isola_client import IsolaClient, IsolaError


class TestCommandExecution:
    """Test executing commands in sandboxes."""

    @pytest.mark.smoke
    def test_execute_simple_command(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute a simple echo command."""
        result = isola_client.execute_command(sandbox["id"], "echo hello")

        assert result["exitCode"] == 0
        assert "hello" in result["stdout"]

    def test_execute_command_with_exit_code(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute a command that exits with non-zero code."""
        result = isola_client.execute_command(sandbox["id"], "exit 42")

        assert result["exitCode"] == 42

    def test_execute_command_with_stderr(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute a command that writes to stderr."""
        result = isola_client.execute_command(sandbox["id"], "echo error >&2")

        assert result["exitCode"] == 0
        assert "error" in result["stderr"]

    def test_execute_command_stdout_and_stderr(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute a command that writes to both stdout and stderr."""
        result = isola_client.execute_command(
            sandbox["id"],
            "echo out && echo err >&2",
        )

        assert result["exitCode"] == 0
        assert "out" in result["stdout"]
        assert "err" in result["stderr"]

    def test_execute_multiline_script(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute a multi-line script."""
        script = """
echo "line 1"
echo "line 2"
echo "line 3"
"""
        result = isola_client.execute_command(sandbox["id"], script)

        assert result["exitCode"] == 0
        assert "line 1" in result["stdout"]
        assert "line 2" in result["stdout"]
        assert "line 3" in result["stdout"]

    def test_execute_command_with_env_vars(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute a command that uses environment variables."""
        result = isola_client.execute_command(
            sandbox["id"],
            "export MY_VAR=hello && echo $MY_VAR",
        )

        assert result["exitCode"] == 0
        assert "hello" in result["stdout"]

    def test_execute_command_with_pipes(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute a command with pipes."""
        result = isola_client.execute_command(
            sandbox["id"],
            "echo 'hello world' | grep world",
        )

        assert result["exitCode"] == 0
        assert "world" in result["stdout"]

    def test_execute_on_nonexistent_sandbox(
        self,
        isola_client: IsolaClient,
    ) -> None:
        """Execute on non-existent sandbox should return 404."""
        with pytest.raises(IsolaError) as exc_info:
            isola_client.execute_command("nonexistent-123", "echo test")

        assert exc_info.value.status_code == 404

    @pytest.mark.slow
    def test_execute_long_running_command(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute a command that takes some time."""
        result = isola_client.execute_command(sandbox["id"], "sleep 3 && echo done")

        assert result["exitCode"] == 0
        assert "done" in result["stdout"]

    def test_execute_creates_file(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute commands that create and read files."""
        # Create a file
        create_result = isola_client.execute_command(
            sandbox["id"],
            "echo 'test content' > /tmp/testfile.txt",
        )
        assert create_result["exitCode"] == 0

        # Read it back
        read_result = isola_client.execute_command(
            sandbox["id"],
            "cat /tmp/testfile.txt",
        )
        assert read_result["exitCode"] == 0
        assert "test content" in read_result["stdout"]

    def test_execute_working_directory(
        self,
        sandbox: dict,
        isola_client: IsolaClient,
    ) -> None:
        """Execute command and verify working directory."""
        result = isola_client.execute_command(sandbox["id"], "pwd")

        assert result["exitCode"] == 0
        # Should return some valid path
        assert "/" in result["stdout"]
