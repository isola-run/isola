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

// Package sidecarapi defines shared contract types between the api-gateway (client) and
// sandbox-sidecar (server). Only types that are identical across both services belong here.
package sidecarapi

import "time"

type CreateCommandRequest struct {
	Args           []string          `json:"args" required:"true" minItems:"1" doc:"Argument vector: Args[0] is the executable path, Args[1:] are its arguments"`
	Env            map[string]string `json:"env,omitempty" doc:"Environment variable overrides"`
	Cwd            string            `json:"cwd,omitempty" doc:"Working directory inside the sandbox. Defaults to the target container process's working directory if omitted."`
	TimeoutSeconds *int              `json:"timeoutSeconds,omitempty" minimum:"1" doc:"Max execution time in seconds"`
}

type CreateCommandResponse struct {
	ID string `json:"id" doc:"Unique command identifier"`
}

type CommandStatusResponse struct {
	ExitCode *int `json:"exitCode" doc:"Process exit code, null if still running"`
}

type FilesystemEntry struct {
	Name          string    `json:"name" doc:"Entry name (final path component)"`
	Path          string    `json:"path" doc:"Absolute path inside the container"`
	Type          string    `json:"type" enum:"file,directory,symlink,other" doc:"Entry type. Symlinks are reported, not followed."`
	Size          int64     `json:"size" doc:"Size in bytes"`
	Permissions   string    `json:"permissions" doc:"Octal permission bits, e.g. 0644"`
	UID           int       `json:"uid" doc:"Owner user ID"`
	GID           int       `json:"gid" doc:"Owner group ID"`
	ModifiedTime  time.Time `json:"modifiedTime" doc:"Last modification time"`
	SymlinkTarget string    `json:"symlinkTarget,omitempty" doc:"Symlink target path. Only set for symlinks."`
}

type ListFilesystemEntriesResponse struct {
	Entries []FilesystemEntry `json:"entries" doc:"Directory entries sorted by name"`
}

type MoveFilesystemEntryRequest struct {
	SourcePath      string `json:"sourcePath" required:"true" minLength:"1" doc:"Path to move (absolute or relative to container cwd)"`
	DestinationPath string `json:"destinationPath" required:"true" minLength:"1" doc:"Destination path (absolute or relative to container cwd). Parent directories are created automatically."`
}
