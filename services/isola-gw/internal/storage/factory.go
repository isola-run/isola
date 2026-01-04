// Package storage provides object storage operations using gocloud.dev/blob.
// This file contains the factory function for creating blob.Bucket instances.
package storage

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"gocloud.dev/blob"
	"gocloud.dev/blob/s3blob"
)

func CreateObjectStorage(ctx context.Context) (*blob.Bucket, error) {
	storageTypeStr := strings.ToLower(os.Getenv("STORAGE_BACKEND"))
	if storageTypeStr == "" {
		storageTypeStr = "s3"
	}

	bucketName := os.Getenv("BUCKET_NAME")
	if bucketName == "" {
		return nil, fmt.Errorf("BUCKET_NAME environment variable is required")
	}

	var scheme string
	switch storageTypeStr {
	case "s3", "localstack":
		// Both S3 and LocalStack use the same blob storage implementation
		scheme = s3blob.Scheme // Use the constant instead of hardcoding "s3"
	default:
		return nil, fmt.Errorf(
			"unsupported STORAGE_BACKEND: %s. Supported values: s3, localstack",
			storageTypeStr,
		)
	}

	region := os.Getenv("REGION")
	if region == "" {
		region = "us-east-1"
	}

	bucketURL := fmt.Sprintf("%s://%s?region=%s", scheme, bucketName, region)

	// TODO:__OMER__ change this
	// Add endpoint URL for LocalStack if provided
	endpointURL := os.Getenv("ENDPOINT_URL")
	if endpointURL != "" {
		bucketURL += fmt.Sprintf("&endpoint=%s", url.QueryEscape(endpointURL))
		// LocalStack requires path-style addressing
		bucketURL += "&s3ForcePathStyle=true"
	}

	bucket, err := blob.OpenBucket(ctx, bucketURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open bucket: %w", err)
	}

	return bucket, nil
}

