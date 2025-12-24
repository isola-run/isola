from __future__ import annotations

import logging
from typing import Optional

import aioboto3
import boto3
from botocore.exceptions import ClientError

from common.storage.base import ObjectStorage

logger = logging.getLogger(__name__)


class S3ObjectStorage(ObjectStorage):
    """
    S3/LocalStack implementation of ObjectStorage using aioboto3.
    
    Supports both AWS S3 and LocalStack (for local development).
    LocalStack uses the same boto3 API as S3, just with a different endpoint URL.
    """
    
    def __init__(
        self,
        bucket: str,
        endpoint_url: Optional[str] = None,
        region: str = "us-east-1",
        access_key_id: Optional[str] = None,
        secret_access_key: Optional[str] = None,
    ):
        """
        Initialize S3ObjectStorage.
        
        Args:
            bucket: The S3 bucket name
            endpoint_url: Optional endpoint URL (for LocalStack, e.g., "http://localhost:4566")
            region: AWS region (default: "us-east-1")
            access_key_id: Optional AWS access key ID (uses default credentials if not provided)
            secret_access_key: Optional AWS secret access key (uses default credentials if not provided)
        """
        self.bucket = bucket
        self.endpoint_url = endpoint_url
        self.region = region
        self.access_key_id = access_key_id
        self.secret_access_key = secret_access_key
        
        # Build session kwargs
        self.session_kwargs = {}
        if access_key_id and secret_access_key:
            self.session_kwargs["aws_access_key_id"] = access_key_id
            self.session_kwargs["aws_secret_access_key"] = secret_access_key
        if endpoint_url:
            self.session_kwargs["endpoint_url"] = endpoint_url
    
    async def generate_presigned_upload_url(
        self,
        key: str,
        expires_in: int = 900,
        content_type: str | None = None,
    ) -> str:
        """
        Generate a presigned URL for uploading an object to S3.
        
        Args:
            key: The object key (path) in the S3 bucket
            expires_in: URL expiration time in seconds (default: 900 = 15 minutes)
            content_type: Optional content type for the upload
            
        Returns:
            A presigned URL that can be used to upload the object via PUT request
        """
        try:
            # Use synchronous boto3 client for presigned URL generation
            # (no network call needed, purely computational)
            s3_client = boto3.client(
                "s3",
                region_name=self.region,
                **self.session_kwargs,
            )
            
            params = {
                "Bucket": self.bucket,
                "Key": key,
            }
            
            if content_type:
                params["ContentType"] = content_type
            
            # generate_presigned_url is synchronous (no network call)
            # ExpiresIn is a separate parameter, not part of Params
            url = s3_client.generate_presigned_url(
                "put_object",
                Params=params,
                ExpiresIn=expires_in,
            )
            
            logger.debug(
                "Generated presigned upload URL for key=%s, expires_in=%d",
                key,
                expires_in,
            )
            
            return url
        except ClientError as e:
            logger.error(
                "Failed to generate presigned upload URL for key=%s: %s",
                key,
                e,
            )
            raise
    
    async def generate_presigned_download_url(
        self,
        key: str,
        expires_in: int = 900,
    ) -> str:
        """
        Generate a presigned URL for downloading an object from S3.
        
        Args:
            key: The object key (path) in the S3 bucket
            expires_in: URL expiration time in seconds (default: 900 = 15 minutes)
            
        Returns:
            A presigned URL that can be used to download the object via GET request
        """
        try:
            # Use synchronous boto3 client for presigned URL generation
            # (no network call needed, purely computational)
            s3_client = boto3.client(
                "s3",
                region_name=self.region,
                **self.session_kwargs,
            )
            
            # generate_presigned_url is synchronous (no network call)
            url = s3_client.generate_presigned_url(
                "get_object",
                Params={
                    "Bucket": self.bucket,
                    "Key": key,
                },
                ExpiresIn=expires_in,
            )
            
            logger.debug(
                "Generated presigned download URL for key=%s, expires_in=%d",
                key,
                expires_in,
            )
            
            return url
        except ClientError as e:
            logger.error(
                "Failed to generate presigned download URL for key=%s: %s",
                key,
                e,
            )
            raise
    
    async def delete_object(self, key: str) -> bool:
        """
        Delete an object from S3.
        
        Args:
            key: The object key (path) in the S3 bucket
            
        Returns:
            True if the object was deleted successfully, False otherwise
        """
        session = aioboto3.Session()
        
        async with session.client(
            "s3",
            region_name=self.region,
            **self.session_kwargs,
        ) as s3_client:
            try:
                await s3_client.delete_object(
                    Bucket=self.bucket,
                    Key=key,
                )
                
                logger.debug("Deleted object with key=%s", key)
                return True
            except ClientError as e:
                logger.error(
                    "Failed to delete object with key=%s: %s",
                    key,
                    e,
                )
                return False

