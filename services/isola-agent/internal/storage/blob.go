// Package storage provides blob storage operations using go-cloud.
package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"gocloud.dev/blob"
	_ "gocloud.dev/blob/s3blob" // S3 driver
	"gocloud.dev/gcerrors"
)

var (
	// ErrBucketNotConfigured is returned when BUCKET_NAME env var is not set.
	ErrBucketNotConfigured = errors.New("BUCKET_NAME environment variable is required")
)

// BlobStorage wraps go-cloud blob operations for S3/LocalStack.
type BlobStorage struct {
	bucket *blob.Bucket
	mu     sync.RWMutex
}

// blobStorageInstance is the singleton instance.
var (
	blobStorageInstance *BlobStorage
	blobStorageOnce     sync.Once
	blobStorageErr      error
)

// GetStorage returns the singleton BlobStorage instance.
// It initializes the storage on first call using environment variables:
//   - BUCKET_NAME: The S3 bucket name (required)
//   - ENDPOINT_URL: Optional endpoint URL for LocalStack
//   - REGION: AWS region (default: "us-east-1")
//   - AWS_ACCESS_KEY_ID: AWS access key (optional)
//   - AWS_SECRET_ACCESS_KEY: AWS secret key (optional)
func GetStorage() (*BlobStorage, error) {
	blobStorageOnce.Do(func() {
		blobStorageInstance, blobStorageErr = newBlobStorage()
	})
	return blobStorageInstance, blobStorageErr
}

// newBlobStorage creates a new BlobStorage instance from environment variables.
func newBlobStorage() (*BlobStorage, error) {
	bucketName := os.Getenv("BUCKET_NAME")
	if bucketName == "" {
		return nil, ErrBucketNotConfigured
	}

	endpointURL := os.Getenv("ENDPOINT_URL")
	region := os.Getenv("REGION")
	if region == "" {
		region = "us-east-1"
	}

	// Build the S3 URL for go-cloud
	// Format: s3://bucket-name?region=us-east-1[&endpoint=http://localhost:4566]
	s3URL := fmt.Sprintf("s3://%s?region=%s", bucketName, region)

	// Add endpoint for LocalStack if configured
	if endpointURL != "" {
		s3URL = fmt.Sprintf("%s&endpoint=%s", s3URL, endpointURL)
		log.Printf("Using S3 endpoint: %s", endpointURL)
	}

	// Note: AWS credentials are picked up automatically from:
	// - Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
	// - AWS credentials file
	// - IAM role (when running on AWS)

	ctx := context.Background()
	bucket, err := blob.OpenBucket(ctx, s3URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open bucket %s: %w", bucketName, err)
	}

	log.Printf("Initialized blob storage for bucket: %s", bucketName)

	return &BlobStorage{
		bucket: bucket,
	}, nil
}

// Delete removes an object from the S3 bucket.
// Returns true if the object was deleted successfully, false otherwise.
// If the object doesn't exist, it returns true (idempotent delete).
func (s *BlobStorage) Delete(ctx context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.bucket == nil {
		return false, errors.New("bucket not initialized")
	}

	err := s.bucket.Delete(ctx, key)
	if err != nil {
		// Check if the error is because the object doesn't exist
		// go-cloud uses gcerrors.NotFound for not-found errors
		if gcerrors.Code(err) == gcerrors.NotFound {
			log.Printf("Object %s does not exist, treating as deleted", key)
			return true, nil
		}
		log.Printf("Failed to delete object %s: %v", key, err)
		return false, err
	}

	log.Printf("Successfully deleted object: %s", key)
	return true, nil
}

// Close releases the bucket resources.
func (s *BlobStorage) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bucket != nil {
		err := s.bucket.Close()
		s.bucket = nil
		return err
	}
	return nil
}

// Exists checks if an object exists in the bucket.
func (s *BlobStorage) Exists(ctx context.Context, key string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.bucket == nil {
		return false, errors.New("bucket not initialized")
	}

	exists, err := s.bucket.Exists(ctx, key)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of %s: %w", key, err)
	}

	return exists, nil
}
