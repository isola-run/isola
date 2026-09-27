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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isola-run/isola/internal/snapshot"
	"gocloud.dev/blob/memblob"
)

// fakeUploader records calls and can return a configurable error.
type fakeUploader struct {
	key     string
	data    []byte
	err     error
	called  bool
	written int64

	overrideWritten *int64
}

func (f *fakeUploader) upload(_ context.Context, key string, r io.Reader) (int64, error) {
	f.called = true
	f.key = key
	if f.err != nil {
		return 0, f.err
	}
	var buf bytes.Buffer
	n, err := io.Copy(&buf, r)
	if err != nil {
		return 0, err
	}
	f.data = buf.Bytes()
	f.written = n
	if f.overrideWritten != nil {
		return *f.overrideWritten, nil
	}
	return n, nil
}

type errReader struct {
	data []byte
	pos  int
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWriteTerminationLogTo(t *testing.T) {
	t.Run("writes valid JSON", func(t *testing.T) {
		dir := t.TempDir()
		logPath := filepath.Join(dir, "termination-log")

		result := snapshot.UploadResult{
			SnapshotKey:  "rootfssnapshots/ns/snap.tar",
			BytesWritten: 42,
		}
		if err := writeTerminationLogTo(logPath, result); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(logPath) //nolint:gosec // test reads from t.TempDir()
		if err != nil {
			t.Fatalf("failed to read log: %v", err)
		}

		var got snapshot.UploadResult
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("invalid JSON: %v", err)
		}
		if got.SnapshotKey != result.SnapshotKey {
			t.Errorf("snapshotKey = %q, want %q", got.SnapshotKey, result.SnapshotKey)
		}
		if got.BytesWritten != result.BytesWritten {
			t.Errorf("bytesWritten = %d, want %d", got.BytesWritten, result.BytesWritten)
		}
	})

	t.Run("returns error for invalid path", func(t *testing.T) {
		err := writeTerminationLogTo("/nonexistent/dir/file", snapshot.UploadResult{})
		if err == nil {
			t.Fatal("expected error for invalid path")
		}
	})
}

func TestRunMissingEnvVars(t *testing.T) {
	// Clear all relevant env vars to ensure clean state
	envVars := []string{EnvBucketURL, EnvSnapshotFile, EnvSnapshotName, EnvSnapshotNamespace}

	tests := []struct {
		name       string
		setEnvs    map[string]string
		wantSubstr string
	}{
		{
			name:       "missing bucket URL",
			setEnvs:    map[string]string{},
			wantSubstr: EnvBucketURL,
		},
		{
			name: "missing snapshot file",
			setEnvs: map[string]string{
				EnvBucketURL: "s3://bucket",
			},
			wantSubstr: EnvSnapshotFile,
		},
		{
			name: "missing snapshot name",
			setEnvs: map[string]string{
				EnvBucketURL:    "s3://bucket",
				EnvSnapshotFile: "/tmp/file.tar",
			},
			wantSubstr: EnvSnapshotName,
		},
		{
			name: "missing snapshot namespace",
			setEnvs: map[string]string{
				EnvBucketURL:    "s3://bucket",
				EnvSnapshotFile: "/tmp/file.tar",
				EnvSnapshotName: "snap1",
			},
			wantSubstr: EnvSnapshotNamespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range envVars {
				t.Setenv(k, "")
			}
			for k, v := range tt.setEnvs {
				t.Setenv(k, v)
			}

			err := run(discardLogger())
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantSubstr)
			}
		})
	}
}

func TestUploadSnapshotSuccess(t *testing.T) {
	dir := t.TempDir()
	content := []byte("fake-tarball-content-1234567890")

	srcPath := filepath.Join(dir, "snapshot.tar")
	if err := os.WriteFile(srcPath, content, 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	termLogPath := filepath.Join(dir, "termination-log")
	uploader := &fakeUploader{}

	err := uploadSnapshot(context.Background(), discardLogger(), uploader, uploadConfig{
		snapshotFile:      srcPath,
		snapshotName:      "my-snap",
		snapshotNamespace: "test-ns",
		terminationLog:    termLogPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !uploader.called {
		t.Fatal("uploader was not called")
	}

	// Verify the key format
	wantKey := "rootfssnapshots/test-ns/my-snap.tar"
	if uploader.key != wantKey {
		t.Errorf("upload key = %q, want %q", uploader.key, wantKey)
	}

	// Verify the uploaded data matches
	if !bytes.Equal(uploader.data, content) {
		t.Errorf("uploaded data = %q, want %q", uploader.data, content)
	}

	// Verify bytes written
	if uploader.written != int64(len(content)) {
		t.Errorf("bytes written = %d, want %d", uploader.written, len(content))
	}

	// Verify termination log
	logData, err := os.ReadFile(termLogPath) //nolint:gosec // test reads from t.TempDir()
	if err != nil {
		t.Fatalf("failed to read termination log: %v", err)
	}

	var result snapshot.UploadResult
	if err := json.Unmarshal(logData, &result); err != nil {
		t.Fatalf("invalid termination log JSON: %v", err)
	}
	if result.SnapshotKey != wantKey {
		t.Errorf("termination log snapshotKey = %q, want %q", result.SnapshotKey, wantKey)
	}
	if result.BytesWritten != int64(len(content)) {
		t.Errorf("termination log bytesWritten = %d, want %d", result.BytesWritten, len(content))
	}
}

func TestUploadSnapshotFileOpenFailure(t *testing.T) {
	uploader := &fakeUploader{}

	err := uploadSnapshot(context.Background(), discardLogger(), uploader, uploadConfig{
		snapshotFile:      "/nonexistent/path/to/file.tar",
		snapshotName:      "snap1",
		snapshotNamespace: "ns",
		terminationLog:    filepath.Join(t.TempDir(), "term-log"),
	})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected not-exist error, got: %v", err)
	}
	if uploader.called {
		t.Error("uploader should not be called when file open fails")
	}
}

func TestUploadSnapshotUploadFailure(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "snapshot.tar")
	if err := os.WriteFile(srcPath, []byte("data"), 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	uploadErr := errors.New("simulated cloud storage failure")
	uploader := &fakeUploader{err: uploadErr}

	err := uploadSnapshot(context.Background(), discardLogger(), uploader, uploadConfig{
		snapshotFile:      srcPath,
		snapshotName:      "snap1",
		snapshotNamespace: "ns",
		terminationLog:    filepath.Join(dir, "term-log"),
	})
	if err == nil {
		t.Fatal("expected error from upload failure")
	}
	if !errors.Is(err, uploadErr) {
		t.Errorf("error = %v, want %v", err, uploadErr)
	}

	// Termination log should not be written on upload failure
	if _, statErr := os.Stat(filepath.Join(dir, "term-log")); !os.IsNotExist(statErr) {
		t.Error("termination log should not exist after upload failure")
	}
}

func TestUploadSnapshotTerminationLogFailureDoesNotFail(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "snapshot.tar")
	if err := os.WriteFile(srcPath, []byte("data"), 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	uploader := &fakeUploader{}

	// Use a path that doesn't exist (no parent directory) for the termination log
	err := uploadSnapshot(context.Background(), discardLogger(), uploader, uploadConfig{
		snapshotFile:      srcPath,
		snapshotName:      "snap1",
		snapshotNamespace: "ns",
		terminationLog:    "/nonexistent/dir/term-log",
	})
	// Upload succeeded even though termination log write failed
	if err != nil {
		t.Fatalf("upload should succeed even if termination log write fails: %v", err)
	}
	if !uploader.called {
		t.Error("uploader should have been called")
	}
}

func TestBlobUploaderAbortsOnCopyError(t *testing.T) {
	ctx := context.Background()
	bucket := memblob.OpenBucket(nil)
	defer func() { _ = bucket.Close() }()

	const key = "rootfssnapshots/ns/snap.tar"

	good := []byte("good-complete-tarball-contents")
	if err := bucket.WriteAll(ctx, key, good, nil); err != nil {
		t.Fatalf("seed write failed: %v", err)
	}

	uploader := &blobUploader{bucket: bucket}
	copyErr := errors.New("disk read failure mid-copy")
	r := &errReader{data: []byte("partial-truncated-data"), err: copyErr}

	if _, err := uploader.upload(ctx, key, r); err == nil {
		t.Fatal("expected upload error from mid-copy read failure")
	}

	got, err := bucket.ReadAll(ctx, key)
	if err != nil {
		t.Fatalf("reading key after failed upload: %v", err)
	}
	if !bytes.Equal(got, good) {
		t.Errorf("canonical key clobbered with partial data %q; want unchanged %q", got, good)
	}
}

func TestUploadSnapshotSizeMismatch(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "snapshot.tar")
	content := []byte("full-tarball-contents-here")
	if err := os.WriteFile(srcPath, content, 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	short := int64(len(content) - 5)
	uploader := &fakeUploader{overrideWritten: &short}

	termLogPath := filepath.Join(dir, "term-log")
	err := uploadSnapshot(context.Background(), discardLogger(), uploader, uploadConfig{
		snapshotFile:      srcPath,
		snapshotName:      "snap1",
		snapshotNamespace: "ns",
		terminationLog:    termLogPath,
	})
	if err == nil {
		t.Fatal("expected error from truncated upload")
	}

	if _, statErr := os.Stat(termLogPath); !os.IsNotExist(statErr) {
		t.Error("termination log should not exist after truncated upload")
	}
}

func TestUploadSnapshotEmptyFile(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "empty.tar")
	if err := os.WriteFile(srcPath, []byte{}, 0600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	termLogPath := filepath.Join(dir, "termination-log")
	uploader := &fakeUploader{}

	err := uploadSnapshot(context.Background(), discardLogger(), uploader, uploadConfig{
		snapshotFile:      srcPath,
		snapshotName:      "snap1",
		snapshotNamespace: "ns",
		terminationLog:    termLogPath,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if uploader.written != 0 {
		t.Errorf("bytes written = %d, want 0", uploader.written)
	}

	logData, err := os.ReadFile(termLogPath) //nolint:gosec // test reads from t.TempDir()
	if err != nil {
		t.Fatalf("failed to read termination log: %v", err)
	}
	var result snapshot.UploadResult
	if err := json.Unmarshal(logData, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.BytesWritten != 0 {
		t.Errorf("termination log bytesWritten = %d, want 0", result.BytesWritten)
	}
}
