/*
Copyright 2025 isola.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// isola-restorer downloads a rootfs snapshot from cloud object storage.
// Used as an init container in Sandbox pods to restore filesystem state before container start.
//
// The restorer downloads a snapshot tarball and saves it to a local path,
// which will then be extracted by the main container on startup.
//
// # Required Bucket Permissions
//
// The restorer requires the following permissions on the target bucket:
//
// AWS S3:
//   - s3:GetObject (to download the snapshot)
//
// Google Cloud Storage:
//   - storage.objects.get (to download the snapshot)
//
// Azure Blob Storage:
//   - Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read
//   - Or use the "Storage Blob Data Reader" built-in role
//
// These permissions can be provided via:
//   - A Kubernetes Secret with credentials (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, etc.)
//   - Pod identity (IRSA for EKS, Workload Identity for GKE, Managed Identity for AKS)
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/isola-ai/isola-sb/internal/logging"
	"gocloud.dev/blob"

	// Import blob drivers - they register themselves via init()
	_ "gocloud.dev/blob/azureblob" // Azure Blob Storage (azblob://)
	_ "gocloud.dev/blob/gcsblob"   // Google Cloud Storage (gs://)
	_ "gocloud.dev/blob/s3blob"    // S3 and S3-compatible (s3://)
)

const (
	EnvBucketURL    = "ISOLA_BUCKET_URL"
	EnvSnapshotKey  = "RESTORE_SNAPSHOT_KEY"
	EnvRestoreFile  = "RESTORE_FILE"
	EnvLogLevel     = "ISOLA_LOG_LEVEL"
)

func main() {
	logger := logging.New(logging.Config{
		Level:   getEnv(EnvLogLevel, "info"),
		DevMode: false, // Always JSON for pod logs
	})

	if err := run(logger); err != nil {
		logger.Error("restore failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	bucketURL := os.Getenv(EnvBucketURL)
	snapshotKey := os.Getenv(EnvSnapshotKey)
	restoreFile := os.Getenv(EnvRestoreFile)

	if bucketURL == "" {
		logger.Error("missing required environment variable", "var", EnvBucketURL)
		return errMissingEnv(EnvBucketURL)
	}
	if snapshotKey == "" {
		logger.Error("missing required environment variable", "var", EnvSnapshotKey)
		return errMissingEnv(EnvSnapshotKey)
	}
	if restoreFile == "" {
		logger.Error("missing required environment variable", "var", EnvRestoreFile)
		return errMissingEnv(EnvRestoreFile)
	}

	logger.Info("opening bucket", "url", bucketURL)
	bucket, err := blob.OpenBucket(ctx, bucketURL)
	if err != nil {
		logger.Error("failed to open bucket", "error", err)
		return err
	}
	defer func() { _ = bucket.Close() }()

	logger.Info("downloading snapshot",
		"key", snapshotKey,
		"destination", restoreFile,
	)

	// Check if the snapshot exists
	exists, err := bucket.Exists(ctx, snapshotKey)
	if err != nil {
		logger.Error("failed to check snapshot existence", "key", snapshotKey, "error", err)
		return fmt.Errorf("failed to check snapshot existence: %w", err)
	}
	if !exists {
		logger.Error("snapshot not found", "key", snapshotKey)
		return fmt.Errorf("snapshot not found: %s", snapshotKey)
	}

	// Open the snapshot from the bucket
	reader, err := bucket.NewReader(ctx, snapshotKey, nil)
	if err != nil {
		logger.Error("failed to open snapshot", "key", snapshotKey, "error", err)
		return fmt.Errorf("failed to open snapshot: %w", err)
	}
	defer func() { _ = reader.Close() }()

	// Create the destination file
	f, err := os.Create(restoreFile) //nolint:gosec // restoreFile comes from trusted env var
	if err != nil {
		logger.Error("failed to create restore file", "file", restoreFile, "error", err)
		return fmt.Errorf("failed to create restore file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Download the snapshot
	written, err := io.Copy(f, reader)
	if err != nil {
		logger.Error("failed to download snapshot", "error", err)
		return fmt.Errorf("failed to download snapshot: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := f.Sync(); err != nil {
		logger.Error("failed to sync file", "error", err)
		return fmt.Errorf("failed to sync file: %w", err)
	}

	logger.Info("download complete",
		"bytes_downloaded", written,
		"key", snapshotKey,
		"destination", restoreFile,
	)

	return nil
}

func errMissingEnv(name string) error {
	return fmt.Errorf("%s environment variable is required", name)
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
