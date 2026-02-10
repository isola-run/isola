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

// isola-uploader uploads a local file to cloud object storage.
// Used as a sidecar container in RootfsSnapshot jobs to upload tarballs to S3/GCS/Azure.
//
// The uploader determines the revision number by listing existing rootfs snapshots in the bucket.
//
// # Required Bucket Permissions
//
// The uploader requires the following permissions on the target bucket:
//
// AWS S3:
//   - s3:ListBucket (to list existing rootfs snapshots and determine revision)
//   - s3:PutObject (to upload the rootfs snapshot)
//
// Google Cloud Storage:
//   - storage.objects.list (to list existing rootfs snapshots)
//   - storage.objects.create (to upload the rootfs snapshot)
//
// Azure Blob Storage:
//   - Microsoft.Storage/storageAccounts/blobServices/containers/blobs/read (list)
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
	"regexp"
	"strconv"
	"syscall"

	"github.com/isola-ai/isola-sb/internal/logging"
	"github.com/isola-ai/isola-sb/internal/snapshot"
	"gocloud.dev/blob"

	// Import blob drivers - they register themselves via init()
	_ "gocloud.dev/blob/azureblob" // Azure Blob Storage (azblob://)
	_ "gocloud.dev/blob/gcsblob"   // Google Cloud Storage (gs://)
	_ "gocloud.dev/blob/s3blob"    // S3 and S3-compatible (s3://)
)

const (
	EnvBucketURL             = "ISOLA_BUCKET_URL"
	EnvSnapshotNamespace     = "SNAPSHOT_NAMESPACE"
	EnvSnapshotSandboxName   = "SNAPSHOT_SANDBOX_NAME"
	EnvSnapshotContainerName = "SNAPSHOT_CONTAINER_NAME"
	EnvLogLevel              = "ISOLA_LOG_LEVEL"

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
	namespace := os.Getenv(EnvSnapshotNamespace)
	sandboxName := os.Getenv(EnvSnapshotSandboxName)
	containerName := os.Getenv(EnvSnapshotContainerName)

	if bucketURL == "" {
		logger.Error("missing required environment variable", "var", EnvBucketURL)
		return errMissingEnv(EnvBucketURL)
	}
	if snapshotFile == "" {
		logger.Error("missing required environment variable", "var", EnvSnapshotFile)
		return errMissingEnv(EnvSnapshotFile)
	}
	if namespace == "" {
		logger.Error("missing required environment variable", "var", EnvSnapshotNamespace)
		return errMissingEnv(EnvSnapshotNamespace)
	}
	if sandboxName == "" {
		logger.Error("missing required environment variable", "var", EnvSnapshotSandboxName)
		return errMissingEnv(EnvSnapshotSandboxName)
	}
	if containerName == "" {
		logger.Error("missing required environment variable", "var", EnvSnapshotContainerName)
		return errMissingEnv(EnvSnapshotContainerName)
	}

	logger.Info("opening bucket", "url", bucketURL)
	bucket, err := blob.OpenBucket(ctx, bucketURL)
	if err != nil {
		logger.Error("failed to open bucket", "error", err)
		return err
	}
	defer func() { _ = bucket.Close() }()

	revision, err := getNextRevision(ctx, logger, bucket, namespace, sandboxName)
	if err != nil {
		logger.Error("failed to determine revision", "error", err)
		return err
	}

	snapshotKey := snapshotKeyPath(namespace, sandboxName, revision, containerName)

	logger.Info("uploading rootfs snapshot",
		"file", snapshotFile,
		"bucket", bucketURL,
		"key", snapshotKey,
		"revision", revision,
	)

	f, err := os.Open(snapshotFile) //nolint:gosec // snapshotFile comes from trusted env var
	if err != nil {
		logger.Error("failed to open snapshot file", "file", snapshotFile, "error", err)
		return err
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		logger.Error("failed to stat snapshot file", "file", snapshotFile, "error", err)
		return err
	}

	w, err := bucket.NewWriter(ctx, snapshotKey, nil)
	if err != nil {
		logger.Error("failed to create bucket writer", "key", snapshotKey, "error", err)
		return err
	}

	written, err := io.Copy(w, f)
	if err != nil {
		_ = w.Close()
		logger.Error("failed to upload", "error", err)
		return err
	}

	// Close writer to finalize upload
	if err := w.Close(); err != nil {
		logger.Error("failed to finalize upload", "error", err)
		return err
	}

	logger.Info("upload complete",
		"bytes_written", written,
		"file_size", stat.Size(),
		"key", snapshotKey,
	)

	// Write result to termination log for controller to read
	result := snapshot.UploadResult{
		SnapshotKey:  snapshotKey,
		Revision:     revision,
		BytesWritten: written,
	}
	if err := writeTerminationLog(result); err != nil {
		// Log but don't fail - the upload succeeded
		logger.Warn("failed to write termination log", "error", err)
	}

	return nil
}

func snapshotKeyPath(namespace, sandboxName string, revision int32, containerName string) string {
	return "snapshots/" + namespace + "/" + sandboxName + "/rev-" + padRevision(revision) + "/" + containerName + ".tar"
}

func padRevision(rev int32) string {
	return strconv.FormatInt(int64(rev), 10)
}

// getNextRevision lists existing rootfs snapshots in the bucket and returns the next revision number.
// This ensures revision numbers are always increasing even if RootfsSnapshot resources
// have been deleted from etcd.
func getNextRevision(ctx context.Context, logger *slog.Logger, bucket *blob.Bucket, namespace, sandboxName string) (int32, error) {
	prefix := "snapshots/" + namespace + "/" + sandboxName + "/rev-"

	// Pattern to extract revision number from keys like "snapshots/ns/sandbox/rev-00001/container.tar"
	revPattern := regexp.MustCompile(`rev-(\d+)/`)

	var maxRevision int32 = 0

	logger.Debug("listing existing rootfs snapshots", "prefix", prefix)

	iter := bucket.List(&blob.ListOptions{Prefix: prefix})
	for {
		obj, err := iter.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}

		matches := revPattern.FindStringSubmatch(obj.Key)
		if len(matches) >= 2 {
			rev, err := strconv.ParseInt(matches[1], 10, 32)
			if err == nil && int32(rev) > maxRevision {
				maxRevision = int32(rev)
			}
		}
	}

	logger.Debug("determined next revision", "max_existing", maxRevision, "next", maxRevision+1)
	return maxRevision + 1, nil
}

func writeTerminationLog(result snapshot.UploadResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(terminationLogPath, data, 0600)
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
