// Package storage provides object storage operations using gocloud.dev/blob.
// This file contains the factory function for creating blob.Bucket instances.
// Supports multiple cloud providers via StorageConfig.
package storage

import (
	"context"
	"fmt"

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

// StorageConfig holds all configuration needed for object storage.
type StorageConfig struct {
	Backend string // Backend specifies the storage backend type (s3, localstack, gcs, azure)
	BucketName string
	Region string
	EndpointURL string
	AccessKeyID string
	SecretAccessKey string
}

// CreateObjectStorage creates a blob.Bucket based on the provided configuration.
// It returns the bucket and the bucket name for use in creating a BucketWrapper.
func CreateObjectStorage(ctx context.Context, cfg StorageConfig) (*blob.Bucket, string, error) {
	if cfg.BucketName == "" {
		return nil, "", fmt.Errorf("bucket name is required")
	}

	backend := cfg.Backend
	if backend == "" {
		backend = StorageBackendS3
	}

	switch backend {
	case StorageBackendS3, StorageBackendLocalStack:
		bucket, err := openS3Bucket(ctx, cfg)
		return bucket, cfg.BucketName, err

	case StorageBackendGCS:
		bucket, err := blob.OpenBucket(ctx, fmt.Sprintf("gs://%s", cfg.BucketName))
		return bucket, cfg.BucketName, err

	case StorageBackendAzure:
		bucket, err := blob.OpenBucket(ctx, fmt.Sprintf("azblob://%s", cfg.BucketName))
		return bucket, cfg.BucketName, err

	default:
		return nil, "", fmt.Errorf(
			"unsupported storage backend: %s. Supported values: %s, %s, %s, %s",
			backend,
			StorageBackendS3,
			StorageBackendLocalStack,
			StorageBackendGCS,
			StorageBackendAzure,
		)
	}
}

func openS3Bucket(ctx context.Context, cfg StorageConfig) (*blob.Bucket, error) {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	// Load AWS config
	var awsCfg aws.Config
	var err error

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		awsCfg = aws.Config{
			Region: region,
			Credentials: credentials.NewStaticCredentialsProvider(
				cfg.AccessKeyID,
				cfg.SecretAccessKey,
				"",
			),
		}
	} else {
		// Use default credential chain (IAM roles, etc.)
		awsCfg, err = config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("failed to load AWS config: %w", err)
		}
	}

	var s3Options []func(*s3.Options)

	// Custom endpoint for LocalStack or S3-compatible services
	if cfg.EndpointURL != "" {
		s3Options = append(s3Options, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
			o.UsePathStyle = true // Required for LocalStack, MinIO, etc.
		})
	}

	s3Client := s3.NewFromConfig(awsCfg, s3Options...)

	return s3blob.OpenBucketV2(ctx, s3Client, cfg.BucketName, nil)
}
