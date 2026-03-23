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

// Package validate provides startup-time validation for configuration values.
// These checks catch misconfigurations early (at binary startup) rather than
// at first use, which may be minutes or hours later.
package validate

import (
	"fmt"
	"net/url"
	"strings"
)

// LogLevel returns an error if level is not a recognized log level.
func LogLevel(level string) error {
	switch strings.ToLower(level) {
	case "debug", "info", "warn", "warning", "error":
		return nil
	default:
		return fmt.Errorf("invalid log level %q (must be one of: debug, info, warn, warning, error)", level)
	}
}

// Port returns an error if port is outside the valid TCP range.
func Port(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d (must be between 1 and 65535)", port)
	}
	return nil
}

// ContainerImage returns an error if image is clearly malformed.
// This is a lightweight check — it catches empty strings, whitespace, and
// obviously broken values. It does not validate that the image exists in a
// registry.
func ContainerImage(image string) error {
	if image == "" {
		return fmt.Errorf("container image must not be empty")
	}
	if strings.TrimSpace(image) != image {
		return fmt.Errorf("container image %q must not have leading or trailing whitespace", image)
	}
	if strings.ContainsAny(image, " \t\n\r") {
		return fmt.Errorf("container image %q must not contain whitespace", image)
	}
	return nil
}

// allowedBucketSchemes lists the URL schemes supported by gocloud.dev/blob.
var allowedBucketSchemes = map[string]bool{
	"s3":      true,
	"gs":      true,
	"azblob":  true,
	"mem":     true, // in-memory, used in tests
	"file":    true, // local filesystem, used in dev
}

// BucketURL returns an error if bucketURL is not a well-formed URL with a
// recognized cloud storage scheme.
func BucketURL(bucketURL string) error {
	u, err := url.Parse(bucketURL)
	if err != nil {
		return fmt.Errorf("invalid bucket URL %q: %w", bucketURL, err)
	}
	if u.Scheme == "" {
		return fmt.Errorf("bucket URL %q must include a scheme (e.g., s3://bucket, gs://bucket, azblob://container)", bucketURL)
	}
	if !allowedBucketSchemes[u.Scheme] {
		return fmt.Errorf("bucket URL %q has unsupported scheme %q (supported: s3, gs, azblob, file, mem)", bucketURL, u.Scheme)
	}
	if u.Host == "" && u.Opaque == "" {
		return fmt.Errorf("bucket URL %q must include a bucket/container name after the scheme", bucketURL)
	}
	return nil
}
