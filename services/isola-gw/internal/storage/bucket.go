// Package storage provides object storage operations using gocloud.dev/blob.
// This file provides a wrapper for generating presigned URLs.
package storage

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"gocloud.dev/blob"
)

// BucketWrapper wraps a blob.Bucket and provides presigned URL functionality
type BucketWrapper struct {
	bucket     *blob.Bucket
	bucketName string
	s3Client   *s3.Client
}

// NewBucketWrapper creates a new BucketWrapper
func NewBucketWrapper(bucket *blob.Bucket, bucketName string) (*BucketWrapper, error) {
	wrapper := &BucketWrapper{
		bucket:     bucket,
		bucketName: bucketName,
	}

	// Initialize S3 client for presigned URLs
	ctx := context.Background()
	
	// Get configuration from environment
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	region := os.Getenv("REGION")
	if region == "" {
		region = "us-east-1"
	}
	endpointURL := os.Getenv("ENDPOINT_URL")

	var cfg aws.Config
	var err error

	// If credentials are provided, use them explicitly
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
		// Try to load default config
		cfg, err = config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("failed to load AWS config: %w", err)
		}
	}

	// Set custom endpoint (LocalStack) if provided
	if endpointURL != "" {
		cfg.BaseEndpoint = aws.String(endpointURL)
	}

	// Create S3 client with path-style addressing for LocalStack
	var s3Options []func(*s3.Options)
	if endpointURL != "" {
		// Force path-style addressing for LocalStack (required for custom endpoints)
		s3Options = append(s3Options, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	wrapper.s3Client = s3.NewFromConfig(cfg, s3Options...)

	return wrapper, nil
}

// GeneratePresignedUploadURL generates a presigned URL for uploading
func (b *BucketWrapper) GeneratePresignedUploadURL(ctx context.Context, key string, expiresIn int, contentType string) (string, error) {
	presignClient := s3.NewPresignClient(b.s3Client)

	putObjectInput := &s3.PutObjectInput{
		Bucket: aws.String(b.bucketName),
		Key:    aws.String(key),
	}

	if contentType != "" {
		putObjectInput.ContentType = aws.String(contentType)
	}

	presignRequest, err := presignClient.PresignPutObject(ctx, putObjectInput, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(expiresIn) * time.Second
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned upload URL: %w", err)
	}

	return presignRequest.URL, nil
}

// GeneratePresignedDownloadURL generates a presigned URL for downloading
func (b *BucketWrapper) GeneratePresignedDownloadURL(ctx context.Context, key string, expiresIn int) (string, error) {
	presignClient := s3.NewPresignClient(b.s3Client)

	getObjectInput := &s3.GetObjectInput{
		Bucket: aws.String(b.bucketName),
		Key:    aws.String(key),
	}

	presignRequest, err := presignClient.PresignGetObject(ctx, getObjectInput, func(opts *s3.PresignOptions) {
		opts.Expires = time.Duration(expiresIn) * time.Second
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned download URL: %w", err)
	}

	return presignRequest.URL, nil
}

// DeleteObject deletes an object from the bucket
func (b *BucketWrapper) DeleteObject(ctx context.Context, key string) error {
	return b.bucket.Delete(ctx, key)
}

