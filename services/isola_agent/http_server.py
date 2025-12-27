"""HTTP server for isola_agent providing API endpoints."""

import logging
import os
from pathlib import Path
from typing import Optional

import httpx
from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from pydantic import BaseModel

from common.models.sandbox import DownloadRequest, DownloadResponse
from common.storage import create_storage
from common.storage.base import ObjectStorage

logger = logging.getLogger(__name__)

# Lazy-initialized storage instance for S3 operations (used for delete_after)
_storage: Optional[ObjectStorage] = None


def get_storage() -> ObjectStorage:
    """Get or create the storage instance for S3 operations."""
    global _storage
    if _storage is None:
        _storage = create_storage()
    return _storage

# Shared volume path where files are written (accessible by sandbox container)
SANDBOX_DATA_PATH = os.getenv("SANDBOX_DATA_PATH", "/sandbox-data")

# Timeout for downloading files from S3 (in seconds)
DOWNLOAD_TIMEOUT_SECONDS = 300.0

app = FastAPI(
    title="Isola Agent",
    description="Agent sidecar for sandbox file operations",
    version="1.0.0",
)


class UploadResponse(BaseModel):
    """Response model for file upload."""
    success: bool
    path: str
    size: int


@app.get("/health")
async def health_check():
    """Health check endpoint."""
    return {"status": "healthy"}


@app.post("/upload", response_model=UploadResponse)
async def upload_file(
    file: UploadFile = File(..., description="File to upload"),
    path: str = Form(..., description="Target path relative to sandbox data directory"),
):
    """
    Upload a file to the sandbox shared volume.
    
    The file will be written to the shared volume at the specified path,
    making it accessible to the sandbox container.
    
    Args:
        file: The file to upload
        path: Target path relative to the sandbox data directory
        
    Returns:
        UploadResponse with success status, final path, and file size
    """
    try:
        # Sanitize the path to prevent directory traversal attacks
        # Remove leading slashes and normalize
        clean_path = path.lstrip("/")
        if ".." in clean_path:
            raise HTTPException(
                status_code=400,
                detail="Invalid path: directory traversal not allowed"
            )
        
        # Construct the full target path
        target_path = Path(SANDBOX_DATA_PATH) / clean_path
        
        # Ensure parent directories exist
        target_path.parent.mkdir(parents=True, exist_ok=True)
        
        # Read file content and write to target
        content = await file.read()
        file_size = len(content)
        
        target_path.write_bytes(content)
        
        logger.info(
            "Successfully uploaded file to %s (size: %d bytes)",
            target_path,
            file_size
        )
        
        return UploadResponse(
            success=True,
            path=str(target_path),
            size=file_size
        )
        
    except HTTPException:
        raise
    except PermissionError as e:
        logger.error("Permission denied writing to %s: %s", path, e)
        raise HTTPException(
            status_code=403,
            detail=f"Permission denied: cannot write to {path}"
        )
    except OSError as e:
        logger.error("OS error writing file to %s: %s", path, e)
        raise HTTPException(
            status_code=500,
            detail=f"Failed to write file: {str(e)}"
        )
    except Exception as e:
        logger.exception("Unexpected error during file upload to %s", path)
        raise HTTPException(
            status_code=500,
            detail=f"Unexpected error: {str(e)}"
        )


@app.post("/download", response_model=DownloadResponse)
async def download_file(request: DownloadRequest):
    """
    Download a file from S3 (via presigned URL) to the sandbox shared volume.
    
    This endpoint is called by the controller after a client confirms a large file upload.
    The file is downloaded from S3, written to the shared volume, and optionally deleted
    from S3 after successful download.
    
    Args:
        request: DownloadRequest containing download_url, path, and optional delete_after
        
    Returns:
        DownloadResponse with success status, final path, file size, and deletion status
    """
    deleted_from_s3 = False
    
    try:
        # Sanitize the path to prevent directory traversal attacks
        clean_path = request.path.lstrip("/")
        if ".." in clean_path:
            raise HTTPException(
                status_code=400,
                detail="Invalid path: directory traversal not allowed"
            )
        
        # Construct the full target path
        target_path = Path(SANDBOX_DATA_PATH) / clean_path
        
        # Ensure parent directories exist
        target_path.parent.mkdir(parents=True, exist_ok=True)
        
        # Download the file from the presigned URL
        async with httpx.AsyncClient(timeout=DOWNLOAD_TIMEOUT_SECONDS) as client:
            logger.info("Sending GET request to download URL")
            response = await client.get(request.download_url)
            
            if response.status_code != 200:
                logger.error(
                    "Failed to download file from S3: HTTP %d - %s",
                    response.status_code,
                    response.text[:500] if response.text else "No response body"
                )
                raise HTTPException(
                    status_code=502,
                    detail=f"Failed to download file from S3: HTTP {response.status_code}"
                )
            
            content = response.content
            file_size = len(content)
        
        # Write file to the shared volume
        target_path.write_bytes(content)
        
        logger.info(
            "Successfully downloaded file to %s (size: %d bytes)",
            target_path,
            file_size
        )
        
        # Delete from S3 if requested
        if request.delete_after:
            if not request.s3_key:
                logger.warning(
                    "delete_after=True but no s3_key provided, skipping S3 deletion"
                )
            else:
                try:
                    storage = get_storage()
                    deleted_from_s3 = await storage.delete_object(request.s3_key)
                    if deleted_from_s3:
                        logger.info("Deleted S3 object: %s", request.s3_key)
                    else:
                        logger.warning("Failed to delete S3 object: %s", request.s3_key)
                except Exception as e:
                    # Log but don't fail the request - the file was already written
                    logger.error(
                        "Error deleting S3 object %s: %s",
                        request.s3_key,
                        e
                    )
        
        return DownloadResponse(
            success=True,
            path=str(target_path),
            size=file_size,
            deleted_from_s3=deleted_from_s3
        )
        
    except HTTPException:
        raise
    except httpx.TimeoutException as e:
        logger.error("Timeout downloading file from S3: %s", e)
        logger.error("Timeout occurred after %s seconds", DOWNLOAD_TIMEOUT_SECONDS)
        try:
            from urllib.parse import urlparse
            parsed_url = urlparse(request.download_url)
            logger.error("Timeout URL hostname: %s", parsed_url.hostname)
        except Exception as parse_error:
            logger.warning("Could not parse download URL in timeout handler: %s", parse_error)
        raise HTTPException(
            status_code=504,
            detail="Timeout downloading file from S3"
        )
    except httpx.RequestError as e:
        logger.error("Request error downloading file from S3: %s", e)
        raise HTTPException(
            status_code=502,
            detail=f"Failed to download file from S3: {str(e)}"
        )
    except PermissionError as e:
        logger.error("Permission denied writing to %s: %s", request.path, e)
        raise HTTPException(
            status_code=403,
            detail=f"Permission denied: cannot write to {request.path}"
        )
    except OSError as e:
        logger.error("OS error writing file to %s: %s", request.path, e)
        raise HTTPException(
            status_code=500,
            detail=f"Failed to write file: {str(e)}"
        )
    except Exception as e:
        logger.exception("Unexpected error during file download to %s", request.path)
        raise HTTPException(
            status_code=500,
            detail=f"Unexpected error: {str(e)}"
        )

