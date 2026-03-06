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

// Package snapshot provides shared types for snapshot operations between
// the isola-operator controller and isola-uploader.
package snapshot

// UploadResult is the contract between the uploader and the controller.
// The uploader writes this as JSON to its termination log (/dev/termination-log),
// and the controller parses it to update the RootfsSnapshot status.
//
// This struct is the single source of truth - both services import this package
// to ensure type safety and prevent drift.
type UploadResult struct {
	// SnapshotKey is the object key in the bucket (e.g., "rootfssnapshots/<name>.tar")
	SnapshotKey string `json:"snapshotKey"`
	// BytesWritten is the number of bytes uploaded
	BytesWritten int64 `json:"bytesWritten"`
}
