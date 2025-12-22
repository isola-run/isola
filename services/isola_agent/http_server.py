"""HTTP server for isola_agent providing API endpoints."""

import logging
import os
from pathlib import Path

from fastapi import FastAPI, File, Form, HTTPException, UploadFile
from pydantic import BaseModel

logger = logging.getLogger(__name__)

# Shared volume path where files are written (accessible by sandbox container)
SANDBOX_DATA_PATH = os.getenv("SANDBOX_DATA_PATH", "/sandbox-data")

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

