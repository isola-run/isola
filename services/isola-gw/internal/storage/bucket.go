// Package storage provides object storage operations using gocloud.dev/blob.
// This file provides a wrapper for generating presigned URLs.
// Supports any cloud provider that gocloud.dev/blob supports (S3, GCS, Azure, etc.)
package storage

import (
	"context"
	"fmt"
	"time"

	"gocloud.dev/blob"
)

// BucketWrapper wraps a blob.Bucket and provides presigned URL functionality.
// It uses gocloud.dev/blob's cloud-agnostic SignedURL method.
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

func (b *BucketWrapper) DeleteObject(ctx context.Context, key string) error {
	return b.bucket.Delete(ctx, key)
}

