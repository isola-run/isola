// Package storage provides object storage operations using gocloud.dev/blob.
// This file contains the factory function for creating blob.Bucket instances.
// Supports multiple cloud providers via STORAGE_BACKEND env variable.
package storage

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gocloud.dev/blob"
	"gocloud.dev/blob/s3blob"
	// Import blob drivers for GCS and Azure (S3 is handled programmatically)
	_ "gocloud.dev/blob/azureblob" // Azure Blob Storage (azblob://)
	_ "gocloud.dev/blob/gcsblob"   // Google Cloud Storage (gs://)
)

// Supported storage backend types
const (
	StorageBackendS3         = "s3"
	StorageBackendLocalStack = "localstack"
	StorageBackendGCS        = "gcs"
	StorageBackendAzure      = "azure"
)

func CreateObjectStorage(ctx context.Context) (*blob.Bucket, error) {
	storageTypeStr := strings.ToLower(os.Getenv("STORAGE_BACKEND"))
	if storageTypeStr == "" {
		storageTypeStr = StorageBackendS3
	}

	bucketName := os.Getenv("BUCKET_NAME")
	if bucketName == "" {
		return nil, fmt.Errorf("BUCKET_NAME environment variable is required")
	}

	switch storageTypeStr {
	case StorageBackendS3, StorageBackendLocalStack:
		return openS3Bucket(ctx, bucketName)

	case StorageBackendGCS:
		return blob.OpenBucket(ctx, fmt.Sprintf("gs://%s", bucketName))

	case StorageBackendAzure:
		return blob.OpenBucket(ctx, fmt.Sprintf("azblob://%s", bucketName))

	default:
		return nil, fmt.Errorf(
			"unsupported STORAGE_BACKEND: %s. Supported values: %s, %s, %s, %s",
			storageTypeStr,
			StorageBackendS3,
			StorageBackendLocalStack,
			StorageBackendGCS,
			StorageBackendAzure,
		)
	}
}

func openS3Bucket(ctx context.Context, bucketName string) (*blob.Bucket, error) {
	region := os.Getenv("REGION")
	if region == "" {
		region = "us-east-1"
	}
	endpointURL := os.Getenv("ENDPOINT_URL")

	// Load AWS config
	var cfg aws.Config
	var err error

	accessKeyID := os.Getenv("ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("SECRET_ACCESS_KEY")

	if accessKeyID != "" && secretAccessKey != "" {
		cfg = aws.Config{
			Region: region,
			Credentials: credentials.NewStaticCredentialsProvider(
				accessKeyID,
				secretAccessKey,
				"",
			),
		}
	} else {
		// Use default credential chain (IAM roles, etc.)
		cfg, err = config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("failed to load AWS config: %w", err)
		}
	}

	var s3Options []func(*s3.Options)

	// Custom endpoint for LocalStack or S3-compatible services
	if endpointURL != "" {
		s3Options = append(s3Options, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpointURL)
			o.UsePathStyle = true // Required for LocalStack, MinIO, etc.
		})
	}

	s3Client := s3.NewFromConfig(cfg, s3Options...)

	return s3blob.OpenBucketV2(ctx, s3Client, bucketName, nil)
}

