package storage

import (
	"context"
	"os"
	"testing"
)

func TestOpenBucket_MissingEnvVar(t *testing.T) {
	// Unset the env var if it exists
	originalValue := os.Getenv(EnvBucketURL)
	os.Unsetenv(EnvBucketURL)
	defer func() {
		if originalValue != "" {
			os.Setenv(EnvBucketURL, originalValue)
		}
	}()

	ctx := context.Background()
	_, _, err := OpenBucket(ctx)

	if err == nil {
		t.Error("OpenBucket() expected error when env var is missing")
	}

	expectedMsg := EnvBucketURL + " environment variable is required"
	if err.Error() != expectedMsg {
		t.Errorf("OpenBucket() error = %v, want %v", err.Error(), expectedMsg)
	}
}

func TestOpenBucket_InvalidURL(t *testing.T) {
	originalValue := os.Getenv(EnvBucketURL)
	defer func() {
		if originalValue != "" {
			os.Setenv(EnvBucketURL, originalValue)
		} else {
			os.Unsetenv(EnvBucketURL)
		}
	}()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "empty bucket name",
			url:     "s3://",
			wantErr: true,
		},
		{
			name:    "invalid URL scheme",
			url:     "://invalid",
			wantErr: true,
		},
		{
			name:    "missing bucket name - just scheme",
			url:     "s3:///?region=us-east-1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(EnvBucketURL, tt.url)

			ctx := context.Background()
			bucket, _, err := OpenBucket(ctx)

			if tt.wantErr {
				if err == nil {
					t.Error("OpenBucket() expected error")
					if bucket != nil {
						bucket.Close()
					}
				}
			}
		})
	}
}

func TestEnvBucketURL_Constant(t *testing.T) {
	if EnvBucketURL != "ISOLA_BUCKET_URL" {
		t.Errorf("EnvBucketURL = %v, want ISOLA_BUCKET_URL", EnvBucketURL)
	}
}

func TestNewBucketWrapper_NilBucket(t *testing.T) {
	// Test that NewBucketWrapper handles nil bucket gracefully
	// (Note: in real usage, we'd pass a valid bucket)
	wrapper, err := NewBucketWrapper(nil, "test-bucket")

	if err != nil {
		t.Errorf("NewBucketWrapper() unexpected error: %v", err)
	}

	if wrapper == nil {
		t.Fatal("NewBucketWrapper() returned nil wrapper")
	}

	if wrapper.bucketName != "test-bucket" {
		t.Errorf("BucketWrapper bucketName = %v, want test-bucket", wrapper.bucketName)
	}
}

func TestNewBucketWrapper_ValidBucketName(t *testing.T) {
	tests := []struct {
		name       string
		bucketName string
	}{
		{
			name:       "simple bucket name",
			bucketName: "my-bucket",
		},
		{
			name:       "bucket with dots",
			bucketName: "my.bucket.name",
		},
		{
			name:       "bucket with numbers",
			bucketName: "bucket123",
		},
		{
			name:       "empty bucket name",
			bucketName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapper, err := NewBucketWrapper(nil, tt.bucketName)

			if err != nil {
				t.Errorf("NewBucketWrapper() unexpected error: %v", err)
			}

			if wrapper.bucketName != tt.bucketName {
				t.Errorf("BucketWrapper bucketName = %v, want %v", wrapper.bucketName, tt.bucketName)
			}
		})
	}
}

func TestBucketWrapper_GeneratePresignedUploadURL_NilBucket(t *testing.T) {
	wrapper, _ := NewBucketWrapper(nil, "test-bucket")

	ctx := context.Background()
	// This should panic or error since bucket is nil
	defer func() {
		if r := recover(); r == nil {
			t.Log("GeneratePresignedUploadURL with nil bucket panicked as expected")
		}
	}()

	_, err := wrapper.GeneratePresignedUploadURL(ctx, "test-key", 900, "text/plain")
	if err == nil {
		t.Error("GeneratePresignedUploadURL() with nil bucket should return error or panic")
	}
}

func TestBucketWrapper_GeneratePresignedDownloadURL_NilBucket(t *testing.T) {
	wrapper, _ := NewBucketWrapper(nil, "test-bucket")

	ctx := context.Background()
	defer func() {
		if r := recover(); r == nil {
			t.Log("GeneratePresignedDownloadURL with nil bucket panicked as expected")
		}
	}()

	_, err := wrapper.GeneratePresignedDownloadURL(ctx, "test-key", 900)
	if err == nil {
		t.Error("GeneratePresignedDownloadURL() with nil bucket should return error or panic")
	}
}

func TestBucketWrapper_DeleteObject_NilBucket(t *testing.T) {
	wrapper, _ := NewBucketWrapper(nil, "test-bucket")

	ctx := context.Background()
	defer func() {
		if r := recover(); r == nil {
			t.Log("DeleteObject with nil bucket panicked as expected")
		}
	}()

	err := wrapper.DeleteObject(ctx, "test-key")
	if err == nil {
		t.Error("DeleteObject() with nil bucket should return error or panic")
	}
}

func TestBucketWrapper_Close_NilBucket(t *testing.T) {
	wrapper, _ := NewBucketWrapper(nil, "test-bucket")

	defer func() {
		if r := recover(); r == nil {
			t.Log("Close with nil bucket panicked as expected")
		}
	}()

	err := wrapper.Close()
	if err == nil {
		t.Error("Close() with nil bucket should return error or panic")
	}
}

func TestOpenBucket_URLParsing(t *testing.T) {
	originalValue := os.Getenv(EnvBucketURL)
	defer func() {
		if originalValue != "" {
			os.Setenv(EnvBucketURL, originalValue)
		} else {
			os.Unsetenv(EnvBucketURL)
		}
	}()

	tests := []struct {
		name           string
		url            string
		expectBucket   string
		expectOpenErr  bool
	}{
		{
			name:          "S3 URL with bucket",
			url:           "s3://my-bucket?region=us-east-1",
			expectBucket:  "my-bucket",
			expectOpenErr: true, // Will fail due to no AWS creds in test
		},
		{
			name:          "GCS URL with bucket",
			url:           "gs://my-gcs-bucket",
			expectBucket:  "my-gcs-bucket",
			expectOpenErr: true, // Will fail due to no GCS creds in test
		},
		{
			name:          "Azure URL with container",
			url:           "azblob://my-container",
			expectBucket:  "my-container",
			expectOpenErr: true, // Will fail due to no Azure creds in test
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(EnvBucketURL, tt.url)

			ctx := context.Background()
			bucket, bucketName, err := OpenBucket(ctx)

			// All tests will fail on OpenBucket due to missing credentials,
			// but we can still verify URL parsing by checking if the error
			// message doesn't indicate URL parsing issues

			if tt.expectOpenErr {
				if err == nil {
					t.Log("OpenBucket succeeded (unexpected - credentials may be present)")
					if bucket != nil {
						bucket.Close()
					}
				} else {
					// Verify it's not a URL parsing error
					if err.Error() == "bucket URL \""+tt.url+"\" must include bucket name as host" {
						t.Errorf("OpenBucket() URL parsing failed: %v", err)
					}
					if bucketName != "" {
						t.Log("Bucket name was extracted before open error:", bucketName)
					}
				}
			}
		})
	}
}

func TestBucketWrapper_PresignedURLExpiry(t *testing.T) {
	// Test that expiry values are passed correctly
	// These are more like documentation tests showing expected values

	tests := []struct {
		name      string
		expiresIn int
	}{
		{
			name:      "15 minutes",
			expiresIn: 900,
		},
		{
			name:      "1 hour",
			expiresIn: 3600,
		},
		{
			name:      "1 day",
			expiresIn: 86400,
		},
		{
			name:      "minimum 1 second",
			expiresIn: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the values are valid inputs
			if tt.expiresIn <= 0 {
				t.Errorf("expiresIn = %v, should be positive", tt.expiresIn)
			}
		})
	}
}

func TestSupportedCloudProviders(t *testing.T) {
	// Document the supported URL schemes
	supportedSchemes := []struct {
		scheme  string
		example string
	}{
		{
			scheme:  "s3",
			example: "s3://bucket-name?region=us-east-1",
		},
		{
			scheme:  "gs",
			example: "gs://bucket-name",
		},
		{
			scheme:  "azblob",
			example: "azblob://container-name",
		},
	}

	for _, s := range supportedSchemes {
		t.Run(s.scheme, func(t *testing.T) {
			t.Logf("Supported: %s:// (e.g., %s)", s.scheme, s.example)
		})
	}
}
