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

// isola-checkpoint-uploader uploads gvisor checkpoint files to cloud object storage.
// Used as a sidecar container in GvisorCheckpoint jobs to upload checkpoint directories to S3/GCS/Azure.
//
// The uploader determines the revision number by listing existing checkpoints in the bucket.
//
// # Required Bucket Permissions
//
// The uploader requires the following permissions on the target bucket:
//
// AWS S3:
//   - s3:ListBucket (to list existing checkpoints and determine revision)
//   - s3:PutObject (to upload checkpoint files)
//
// Google Cloud Storage:
//   - storage.objects.list (to list existing checkpoints)
//   - storage.objects.create (to upload checkpoint files)
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
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
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
	EnvBucketURL               = "ISOLA_BUCKET_URL"
	EnvCheckpointNamespace     = "CHECKPOINT_NAMESPACE"
	EnvCheckpointSandboxName   = "CHECKPOINT_SANDBOX_NAME"
	EnvCheckpointContainerName = "CHECKPOINT_CONTAINER_NAME"
	EnvLogLevel                = "ISOLA_LOG_LEVEL"

	// EnvCheckpointDir is the path to the local checkpoint directory to upload
	EnvCheckpointDir = "CHECKPOINT_DIR"

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
	checkpointDir := os.Getenv(EnvCheckpointDir)
	namespace := os.Getenv(EnvCheckpointNamespace)
	sandboxName := os.Getenv(EnvCheckpointSandboxName)
	containerName := os.Getenv(EnvCheckpointContainerName)

	if bucketURL == "" {
		logger.Error("missing required environment variable", "var", EnvBucketURL)
		return errMissingEnv(EnvBucketURL)
	}
	if checkpointDir == "" {
		logger.Error("missing required environment variable", "var", EnvCheckpointDir)
		return errMissingEnv(EnvCheckpointDir)
	}
	if namespace == "" {
		logger.Error("missing required environment variable", "var", EnvCheckpointNamespace)
		return errMissingEnv(EnvCheckpointNamespace)
	}
	if sandboxName == "" {
		logger.Error("missing required environment variable", "var", EnvCheckpointSandboxName)
		return errMissingEnv(EnvCheckpointSandboxName)
	}
	if containerName == "" {
		logger.Error("missing required environment variable", "var", EnvCheckpointContainerName)
		return errMissingEnv(EnvCheckpointContainerName)
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

	checkpointKeyPrefix := checkpointKeyPath(namespace, sandboxName, revision, containerName)

	logger.Info("uploading checkpoint",
		"dir", checkpointDir,
		"bucket", bucketURL,
		"keyPrefix", checkpointKeyPrefix,
		"revision", revision,
	)

	var totalBytesWritten int64
	var filesUploaded int

	// Walk the checkpoint directory and upload all files
	err = filepath.WalkDir(checkpointDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Get relative path from checkpoint directory
		relPath, err := filepath.Rel(checkpointDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		objectKey := checkpointKeyPrefix + relPath

		logger.Debug("uploading file", "file", path, "key", objectKey)

		f, err := os.Open(path) //nolint:gosec // path comes from trusted checkpoint dir
		if err != nil {
			logger.Error("failed to open file", "file", path, "error", err)
			return err
		}
		defer func() { _ = f.Close() }()

		w, err := bucket.NewWriter(ctx, objectKey, nil)
		if err != nil {
			logger.Error("failed to create bucket writer", "key", objectKey, "error", err)
			return err
		}

		written, err := io.Copy(w, f)
		if err != nil {
			_ = w.Close()
			logger.Error("failed to upload", "file", path, "error", err)
			return err
		}

		// Close writer to finalize upload
		if err := w.Close(); err != nil {
			logger.Error("failed to finalize upload", "file", path, "error", err)
			return err
		}

		totalBytesWritten += written
		filesUploaded++

		logger.Debug("uploaded file", "file", path, "key", objectKey, "bytes", written)

		return nil
	})

	if err != nil {
		logger.Error("failed to upload checkpoint files", "error", err)
		return err
	}

	logger.Info("upload complete",
		"total_bytes_written", totalBytesWritten,
		"files_uploaded", filesUploaded,
		"key_prefix", checkpointKeyPrefix,
	)

	// Write result to termination log for controller to read
	result := snapshot.UploadResult{
		SnapshotKey:  checkpointKeyPrefix,
		Revision:     revision,
		BytesWritten: totalBytesWritten,
	}
	if err := writeTerminationLog(result); err != nil {
		// Log but don't fail - the upload succeeded
		logger.Warn("failed to write termination log", "error", err)
	}

	return nil
}

func checkpointKeyPath(namespace, sandboxName string, revision int32, containerName string) string {
	return "checkpoints/" + namespace + "/" + sandboxName + "/rev-" + padRevision(revision) + "/" + containerName + "/"
}

func padRevision(rev int32) string {
	return strconv.FormatInt(int64(rev), 10)
}

// getNextRevision lists existing checkpoints in the bucket and returns the next revision number.
// This ensures revision numbers are always increasing even if GvisorCheckpoint resources
// have been deleted from etcd.
func getNextRevision(ctx context.Context, logger *slog.Logger, bucket *blob.Bucket, namespace, sandboxName string) (int32, error) {
	prefix := "checkpoints/" + namespace + "/" + sandboxName + "/rev-"

	// Pattern to extract revision number from keys like "checkpoints/ns/sandbox/rev-00001/container/file"
	revPattern := regexp.MustCompile(`rev-(\d+)/`)

	var maxRevision int32 = 0

	logger.Debug("listing existing checkpoints", "prefix", prefix)

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
