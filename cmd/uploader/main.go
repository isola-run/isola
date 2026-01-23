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
// Used as a sidecar container in snapshot jobs to upload tarballs to S3/GCS/Azure.
//
// The uploader determines the revision number by listing existing snapshots in the bucket.
//
// # Required Bucket Permissions
//
// The uploader requires the following permissions on the target bucket:
//
// AWS S3:
//   - s3:ListBucket (to list existing snapshots and determine revision)
//   - s3:PutObject (to upload the snapshot)
//
// Google Cloud Storage:
//   - storage.objects.list (to list existing snapshots)
//   - storage.objects.create (to upload the snapshot)
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
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"syscall"

	"github.com/isola-ai/isola-sb/internal/snapshot"
	"gocloud.dev/blob"

	// Import blob drivers - they register themselves via init()
	_ "gocloud.dev/blob/azureblob" // Azure Blob Storage (azblob://)
	_ "gocloud.dev/blob/gcsblob"   // Google Cloud Storage (gs://)
	_ "gocloud.dev/blob/s3blob"    // S3 and S3-compatible (s3://)
)

const (
	// EnvBucketURL is the bucket URL (e.g., s3://bucket?region=us-east-1)
	EnvBucketURL = "ISOLA_BUCKET_URL"
	// EnvSnapshotFile is the path to the local file to upload
	EnvSnapshotFile = "SNAPSHOT_FILE"
	// EnvSnapshotNamespace is the namespace of the sandbox
	EnvSnapshotNamespace = "SNAPSHOT_NAMESPACE"
	// EnvSnapshotSandboxName is the name of the sandbox
	EnvSnapshotSandboxName = "SNAPSHOT_SANDBOX_NAME"
	// EnvSnapshotContainerName is the name of the container being snapshotted
	EnvSnapshotContainerName = "SNAPSHOT_CONTAINER_NAME"

	// terminationLogPath is where we write the result for the controller to read
	terminationLogPath = "/dev/termination-log"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	bucketURL := os.Getenv(EnvBucketURL)
	snapshotFile := os.Getenv(EnvSnapshotFile)
	namespace := os.Getenv(EnvSnapshotNamespace)
	sandboxName := os.Getenv(EnvSnapshotSandboxName)
	containerName := os.Getenv(EnvSnapshotContainerName)

	if bucketURL == "" {
		return fmt.Errorf("%s environment variable is required", EnvBucketURL)
	}
	if snapshotFile == "" {
		return fmt.Errorf("%s environment variable is required", EnvSnapshotFile)
	}
	if namespace == "" {
		return fmt.Errorf("%s environment variable is required", EnvSnapshotNamespace)
	}
	if sandboxName == "" {
		return fmt.Errorf("%s environment variable is required", EnvSnapshotSandboxName)
	}
	if containerName == "" {
		return fmt.Errorf("%s environment variable is required", EnvSnapshotContainerName)
	}

	bucket, err := blob.OpenBucket(ctx, bucketURL)
	if err != nil {
		return fmt.Errorf("failed to open bucket: %w", err)
	}
	defer func() { _ = bucket.Close() }()

	revision, err := getNextRevision(ctx, bucket, namespace, sandboxName)
	if err != nil {
		return fmt.Errorf("failed to determine revision: %w", err)
	}

	snapshotKey := fmt.Sprintf("snapshots/%s/%s/rev-%05d/%s.tar", namespace, sandboxName, revision, containerName)

	fmt.Printf("Uploading %s to %s (key: %s, revision: %d)\n", snapshotFile, bucketURL, snapshotKey, revision)

	f, err := os.Open(snapshotFile) //nolint:gosec // snapshotFile comes from trusted env var
	if err != nil {
		return fmt.Errorf("failed to open snapshot file: %w", err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat snapshot file: %w", err)
	}

	w, err := bucket.NewWriter(ctx, snapshotKey, nil)
	if err != nil {
		return fmt.Errorf("failed to create bucket writer: %w", err)
	}

	written, err := io.Copy(w, f)
	if err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to upload: %w", err)
	}

	// Close writer to finalize upload
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to finalize upload: %w", err)
	}

	fmt.Printf("Successfully uploaded %d bytes (file size: %d bytes)\n", written, stat.Size())

	// Write result to termination log for controller to read
	result := snapshot.UploadResult{
		SnapshotKey:  snapshotKey,
		Revision:     revision,
		BytesWritten: written,
	}
	if err := writeTerminationLog(result); err != nil {
		// Log but don't fail - the upload succeeded
		fmt.Fprintf(os.Stderr, "warning: failed to write termination log: %v\n", err)
	}

	return nil
}

// getNextRevision lists existing snapshots in the bucket and returns the next revision number.
// This ensures revision numbers are always increasing even if RootfsSnapshot resources
// have been deleted from etcd.
func getNextRevision(ctx context.Context, bucket *blob.Bucket, namespace, sandboxName string) (int32, error) {
	prefix := fmt.Sprintf("snapshots/%s/%s/rev-", namespace, sandboxName)

	// Pattern to extract revision number from keys like "snapshots/ns/sandbox/rev-00001/container.tar"
	revPattern := regexp.MustCompile(`rev-(\d+)/`)

	var maxRevision int32 = 0

	iter := bucket.List(&blob.ListOptions{Prefix: prefix})
	for {
		obj, err := iter.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("failed to list bucket objects: %w", err)
		}

		matches := revPattern.FindStringSubmatch(obj.Key)
		if len(matches) >= 2 {
			rev, err := strconv.ParseInt(matches[1], 10, 32)
			if err == nil && int32(rev) > maxRevision {
				maxRevision = int32(rev)
			}
		}
	}

	return maxRevision + 1, nil
}

func writeTerminationLog(result snapshot.UploadResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(terminationLogPath, data, 0600)
}
