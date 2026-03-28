// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// isola-uploader uploads a local file to cloud object storage.
// Used as a sidecar container in RootfsSnapshot jobs to upload tarballs to S3/GCS/Azure.
//
// # Required Bucket Permissions
//
// The uploader requires the following permissions on the target bucket:
//
// AWS S3:
//   - s3:PutObject (to upload the rootfs snapshot)
//
// Google Cloud Storage:
//   - storage.objects.create (to upload the rootfs snapshot)
//
// Azure Blob Storage:
//   - Microsoft.Storage/storageAccounts/blobServices/containers/blobs/write (modify blobs)
//   - Microsoft.Storage/storageAccounts/blobServices/containers/blobs/add/action (create new blobs)
//   - Or use the "Storage Blob Data Contributor" built-in role
//
// These permissions can be provided via:
//   - A Kubernetes Secret with credentials (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, etc.)
//   - Pod identity (IRSA for EKS, Workload Identity for GKE, Managed Identity for AKS)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/isola-run/isola/internal/logging"
	"github.com/isola-run/isola/internal/snapshot"
	"gocloud.dev/blob"

	// Import blob drivers - they register themselves via init()
	_ "gocloud.dev/blob/azureblob" // Azure Blob Storage (azblob://)
	_ "gocloud.dev/blob/gcsblob"   // Google Cloud Storage (gs://)
	_ "gocloud.dev/blob/s3blob"    // S3 and S3-compatible (s3://)
)

// objectUploader abstracts writing an object to cloud storage, enabling test fakes.
type objectUploader interface {
	upload(ctx context.Context, key string, r io.Reader) (int64, error)
}

// blobUploader implements objectUploader using a gocloud.dev blob.Bucket.
type blobUploader struct {
	bucket *blob.Bucket
}

func (u *blobUploader) upload(ctx context.Context, key string, r io.Reader) (int64, error) {
	w, err := u.bucket.NewWriter(ctx, key, nil)
	if err != nil {
		return 0, err
	}

	written, err := io.Copy(w, r)
	if err != nil {
		_ = w.Close()
		return 0, err
	}

	if err := w.Close(); err != nil {
		return 0, err
	}
	return written, nil
}

const (
	EnvBucketURL         = "ISOLA_BUCKET_URL"
	EnvSnapshotName      = "SNAPSHOT_NAME"
	EnvSnapshotNamespace = "SNAPSHOT_NAMESPACE"
	EnvLogLevel          = "ISOLA_LOG_LEVEL"

	// EnvSnapshotFile is the path to the local file to upload
	EnvSnapshotFile = "SNAPSHOT_FILE"

	// terminationLogPath is where we write the result for the controller to read
	terminationLogPath = "/dev/termination-log"
)

func main() {
	logger := logging.New(logging.Config{
		Level:   getEnv(EnvLogLevel, "info"),
		DevMode: false, // Always JSON for job logs
	})

	if err := run(logger); err != nil {
		logger.Error("upload failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	bucketURL := os.Getenv(EnvBucketURL)
	snapshotFile := os.Getenv(EnvSnapshotFile)
	snapshotName := os.Getenv(EnvSnapshotName)
	snapshotNamespace := os.Getenv(EnvSnapshotNamespace)

	if bucketURL == "" {
		logger.Error("missing required environment variable", "var", EnvBucketURL)
		return errMissingEnv(EnvBucketURL)
	}
	if snapshotFile == "" {
		logger.Error("missing required environment variable", "var", EnvSnapshotFile)
		return errMissingEnv(EnvSnapshotFile)
	}
	if snapshotName == "" {
		logger.Error("missing required environment variable", "var", EnvSnapshotName)
		return errMissingEnv(EnvSnapshotName)
	}
	if snapshotNamespace == "" {
		logger.Error("missing required environment variable", "var", EnvSnapshotNamespace)
		return errMissingEnv(EnvSnapshotNamespace)
	}

	logger.Info("opening bucket", "url", bucketURL)
	bucket, err := blob.OpenBucket(ctx, bucketURL)
	if err != nil {
		logger.Error("failed to open bucket", "error", err)
		return err
	}
	defer func() { _ = bucket.Close() }()

	return uploadSnapshot(ctx, logger, &blobUploader{bucket: bucket}, uploadConfig{
		snapshotFile:      snapshotFile,
		snapshotName:      snapshotName,
		snapshotNamespace: snapshotNamespace,
		terminationLog:    terminationLogPath,
	})
}

type uploadConfig struct {
	snapshotFile      string
	snapshotName      string
	snapshotNamespace string
	terminationLog    string
}

func uploadSnapshot(ctx context.Context, logger *slog.Logger, uploader objectUploader, cfg uploadConfig) error {
	snapshotKey := snapshotKeyPath(cfg.snapshotNamespace, cfg.snapshotName)

	logger.Info("uploading rootfs snapshot",
		"file", cfg.snapshotFile,
		"key", snapshotKey,
	)

	f, err := os.Open(cfg.snapshotFile) //nolint:gosec // snapshotFile comes from trusted env var
	if err != nil {
		logger.Error("failed to open snapshot file", "file", cfg.snapshotFile, "error", err)
		return err
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		logger.Error("failed to stat snapshot file", "file", cfg.snapshotFile, "error", err)
		return err
	}

	written, err := uploader.upload(ctx, snapshotKey, f)
	if err != nil {
		logger.Error("failed to upload", "error", err)
		return err
	}

	logger.Info("upload complete",
		"bytes_written", written,
		"file_size", stat.Size(),
		"key", snapshotKey,
	)

	result := snapshot.UploadResult{
		SnapshotKey:  snapshotKey,
		BytesWritten: written,
	}
	if err := writeTerminationLogTo(cfg.terminationLog, result); err != nil {
		// Log but don't fail - the upload succeeded
		logger.Warn("failed to write termination log", "error", err)
	}

	return nil
}

func snapshotKeyPath(namespace, snapshotName string) string {
	return "rootfssnapshots/" + namespace + "/" + snapshotName + ".tar"
}

func writeTerminationLogTo(path string, result snapshot.UploadResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
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
