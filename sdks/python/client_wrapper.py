"""Custom wrapper layer extending the generated Isola client with convenience methods."""

from pathlib import Path
from typing import Optional, Union

from .client import AsyncIsola as _AsyncIsola
from .types.file_upload_response import FileUploadResponse


class AsyncIsola(_AsyncIsola):
    """Extended Isola client with convenience methods."""

    async def upload_file_from_path(
        self,
        sandbox_id: str,
        local_path: Union[str, Path],
        remote_path: Optional[str] = None,
    ) -> FileUploadResponse:
        """Upload a file from the local filesystem to a sandbox.

        Args:
            sandbox_id: Target sandbox ID
            local_path: Path to file on local filesystem
            remote_path: Destination path in sandbox (defaults to filename in /home/user/)

        Returns:
            FileUploadResponse: Response from the upload operation

        Raises:
            FileNotFoundError: If the local file doesn't exist
        """
        local_path = Path(local_path)
        if not local_path.exists():
            raise FileNotFoundError(f"File not found: {local_path}")

        if remote_path is None:
            remote_path = f"/home/user/{local_path.name}"

        content = local_path.read_bytes()
        return await self.files.upload_file(
            id=sandbox_id,
            file=content,
            path=remote_path,
        )
