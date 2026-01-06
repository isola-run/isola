// Package storage provides object storage operations using gocloud.dev/blob.
// Uses URL-based bucket opening for cloud-agnostic storage access.
// Supports S3, GCS, Azure, and S3-compatible services via standard URL schemes.
package storage

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"gocloud.dev/blob"
	// Import blob drivers - they register themselves via init()
	_ "gocloud.dev/blob/azureblob" // Azure Blob Storage (azblob://)
	_ "gocloud.dev/blob/gcsblob"   // Google Cloud Storage (gs://)
	_ "gocloud.dev/blob/s3blob"    // S3 and S3-compatible (s3://)
)

// Environment variable for bucket URL
const EnvBucketURL = "ISOLA_BUCKET_URL"

// OpenBucket opens a storage bucket using the ISOLA_BUCKET_URL environment variable.
// Returns the bucket and bucket name, or an error.
//
// Supported URL schemes:
//   - s3://bucket-name?region=us-east-1 (AWS S3)
//   - s3://bucket-name?endpoint=http://localhost:4566&use_path_style=true (LocalStack/MinIO)
//   - gs://bucket-name (Google Cloud Storage)
//   - azblob://container-name (Azure Blob Storage)
//
// Credentials are loaded automatically from standard environment variables:
//   - AWS: AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION
//   - GCS: GOOGLE_APPLICATION_CREDENTIALS
//   - Azure: AZURE_STORAGE_ACCOUNT, AZURE_STORAGE_KEY
func OpenBucket(ctx context.Context) (*blob.Bucket, string, error) {
	bucketURL := os.Getenv(EnvBucketURL)
	if bucketURL == "" {
		return nil, "", fmt.Errorf("%s environment variable is required", EnvBucketURL)
	}

	// Parse URL to extract bucket name
	u, err := url.Parse(bucketURL)
	if err != nil {
		return nil, "", fmt.Errorf("invalid bucket URL %q: %w", bucketURL, err)
	}

	bucketName := u.Host
	if bucketName == "" {
		return nil, "", fmt.Errorf("bucket URL %q must include bucket name as host", bucketURL)
	}

	bucket, err := blob.OpenBucket(ctx, bucketURL)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open bucket %q: %w", bucketURL, err)
	}

	return bucket, bucketName, nil
}


type BucketWrapper struct {
	bucket     *blob.Bucket
	bucketName string
}

func NewBucketWrapper(bucket *blob.Bucket, bucketName string) (*BucketWrapper, error) {
	return &BucketWrapper{
		bucket:     bucket,
		bucketName: bucketName,
	}, nil
}

// GeneratePresignedUploadURL generates a presigned URL for uploading a file.
// TODO: limit the size of the file that can be uploaded
func (b *BucketWrapper) GeneratePresignedUploadURL(ctx context.Context, key string, expiresIn int, contentType string) (string, error) {
	opts := &blob.SignedURLOptions{
		Expiry: time.Duration(expiresIn) * time.Second,
		Method: "PUT",
	}

	url, err := b.bucket.SignedURL(ctx, key, opts)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned upload URL: %w", err)
	}

	return url, nil
}

// GeneratePresignedDownloadURL generates a presigned URL for downloading a file.
func (b *BucketWrapper) GeneratePresignedDownloadURL(ctx context.Context, key string, expiresIn int) (string, error) {
	opts := &blob.SignedURLOptions{
		Expiry: time.Duration(expiresIn) * time.Second,
		Method: "GET",
	}

	url, err := b.bucket.SignedURL(ctx, key, opts)
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned download URL: %w", err)
	}

	return url, nil
}

// DeleteObject deletes an object from the bucket.
func (b *BucketWrapper) DeleteObject(ctx context.Context, key string) error {
	return b.bucket.Delete(ctx, key)
}

// Close closes the underlying bucket.
func (b *BucketWrapper) Close() error {
	return b.bucket.Close()
}
