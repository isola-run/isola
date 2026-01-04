from __future__ import annotations

import os
from typing import Optional

from common.storage.base import ObjectStorage, StorageType
from common.storage.s3 import S3ObjectStorage


def create_storage() -> ObjectStorage:
    """
    Factory function to create an ObjectStorage instance based on configuration.
    
    Environment variables:
        STORAGE_BACKEND: "s3" or "localstack" (default: "s3")
        BUCKET_NAME: Bucket name for file uploads (required)
        ENDPOINT_URL: LocalStack endpoint URL (optional, for dev)
        REGION: region (default: "us-east-1")
        ACCESS_KEY_ID: Access key
        SECRET_ACCESS_KEY: Secret key
    
    Returns:
        ObjectStorage: An instance of the configured storage type
        
    Raises:
        ValueError: If BUCKET_NAME is not set or STORAGE_BACKEND is unsupported
    """
    storage_type_str = os.getenv("STORAGE_BACKEND", StorageType.S3.value).lower()
    bucket = os.getenv("BUCKET_NAME")
    
    if not bucket:
        raise ValueError("BUCKET_NAME environment variable is required")
    
    try:
        storage_type = StorageType(storage_type_str)
    except ValueError:
        supported_values = ", ".join([st.value for st in StorageType])
        raise ValueError(
            f"Unsupported STORAGE_BACKEND: {storage_type_str}. "
            f"Supported values: {supported_values}"
        )
    
    if storage_type in (StorageType.S3, StorageType.LOCALSTACK):
        endpoint_url: Optional[str] = os.getenv("ENDPOINT_URL")
        region = os.getenv("REGION", "us-east-1")
        access_key_id = os.getenv("ACCESS_KEY_ID")
        secret_access_key = os.getenv("SECRET_ACCESS_KEY")
        
        return S3ObjectStorage(
            bucket=bucket,
            endpoint_url=endpoint_url,
            region=region,
            access_key_id=access_key_id,
            secret_access_key=secret_access_key,
        )
    else:
        # This should never happen with current enum values, but kept for future extensibility
        supported_values = ", ".join([st.value for st in StorageType])
        raise ValueError(
            f"Unsupported STORAGE_BACKEND: {storage_type.value}. "
            f"Supported values: {supported_values}"
        )

