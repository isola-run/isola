from __future__ import annotations

from abc import ABC, abstractmethod
from enum import Enum


class StorageType(str, Enum):
    """
    Enumeration of available storage types.
    
    Values:
        S3: AWS S3 storage
        LOCALSTACK: LocalStack (for local development)
    """
    S3 = "s3"
    LOCALSTACK = "localstack"


class ObjectStorage(ABC):
    """
    Abstract interface for object storage operations.
    
    This interface supports operations for generating presigned URLs
    for uploads/downloads and deleting objects from storage.
    """
    
    @abstractmethod
    async def generate_presigned_upload_url(
        self,
        key: str,
        expires_in: int = 900,
        content_type: str | None = None,
    ) -> str:
        """
        Generate a presigned URL for uploading an object.
        
        Args:
            key: The object key (path) in the storage bucket
            expires_in: URL expiration time in seconds (default: 900 = 15 minutes)
            content_type: Optional content type for the upload
            
        Returns:
            A presigned URL that can be used to upload the object via PUT request
        """
        pass
    
    @abstractmethod
    async def generate_presigned_download_url(
        self,
        key: str,
        expires_in: int = 900,
    ) -> str:
        """
        Generate a presigned URL for downloading an object.
        
        Args:
            key: The object key (path) in the storage bucket
            expires_in: URL expiration time in seconds (default: 900 = 15 minutes)
            
        Returns:
            A presigned URL that can be used to download the object via GET request
        """
        pass
    
    @abstractmethod
    async def delete_object(self, key: str) -> bool:
        """
        Delete an object from storage.
        
        Args:
            key: The object key (path) in the storage bucket
            
        Returns:
            True if the object was deleted successfully, False otherwise
        """
        pass

