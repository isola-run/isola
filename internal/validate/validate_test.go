// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package validate

import (
	"strings"
	"testing"
)

func TestLogLevel(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "warning", "error", "DEBUG", "Info", "WARN"} {
		t.Run("valid_"+level, func(t *testing.T) {
			if err := LogLevel(level); err != nil {
				t.Errorf("LogLevel(%q) = %v, want nil", level, err)
			}
		})
	}

	for _, level := range []string{"", "trace", "fatal", "verbose", "none"} {
		t.Run("invalid_"+level, func(t *testing.T) {
			if err := LogLevel(level); err == nil {
				t.Errorf("LogLevel(%q) = nil, want error", level)
			}
		})
	}
}

func TestPort(t *testing.T) {
	for _, port := range []int{1, 80, 443, 8080, 10032, 65535} {
		if err := Port(port); err != nil {
			t.Errorf("Port(%d) = %v, want nil", port, err)
		}
	}

	for _, port := range []int{0, -1, -100, 65536, 100000} {
		if err := Port(port); err == nil {
			t.Errorf("Port(%d) = nil, want error", port)
		}
	}
}

func TestContainerImage(t *testing.T) {
	valid := []string{
		"python:3.12",
		"ubuntu:latest",
		"ghcr.io/isola-ai/sandbox-sidecar:v0.1.0",
		"registry.example.com/org/image:tag",
		"nginx",
	}
	for _, image := range valid {
		t.Run("valid_"+image, func(t *testing.T) {
			if err := ContainerImage(image); err != nil {
				t.Errorf("ContainerImage(%q) = %v, want nil", image, err)
			}
		})
	}

	invalid := []string{
		"",
		" leading-space:latest",
		"trailing-space:latest ",
		"has space:latest",
		"has\ttab:latest",
		"has\nnewline:latest",
	}
	for _, image := range invalid {
		t.Run("invalid", func(t *testing.T) {
			if err := ContainerImage(image); err == nil {
				t.Errorf("ContainerImage(%q) = nil, want error", image)
			}
		})
	}
}

func TestBucketURL(t *testing.T) {
	valid := []string{
		"s3://my-bucket?region=us-east-1",
		"s3://my-bucket",
		"gs://my-bucket",
		"azblob://my-container",
		"mem://test-bucket",
		"file:///tmp/snapshots",
	}
	for _, u := range valid {
		t.Run("valid_"+u, func(t *testing.T) {
			if err := BucketURL(u); err != nil {
				t.Errorf("BucketURL(%q) = %v, want nil", u, err)
			}
		})
	}

	invalid := []struct {
		url     string
		wantMsg string
	}{
		{"my-bucket", "must include a scheme"},
		{"ftp://my-bucket", "unsupported scheme"},
		{"http://my-bucket", "unsupported scheme"},
		{"s3://", "must include a bucket"},
	}
	for _, tc := range invalid {
		t.Run("invalid_"+tc.url, func(t *testing.T) {
			err := BucketURL(tc.url)
			if err == nil {
				t.Errorf("BucketURL(%q) = nil, want error containing %q", tc.url, tc.wantMsg)
				return
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("BucketURL(%q) = %v, want error containing %q", tc.url, err, tc.wantMsg)
			}
		})
	}
}
