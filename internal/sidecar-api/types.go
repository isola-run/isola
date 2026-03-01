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

type FilesystemWriteResponse struct {
	AbsolutePath string `json:"absolutePath" example:"/workspace/file.txt" doc:"Absolute path where file was written"`
	BytesWritten int64  `json:"bytesWritten" example:"1024" doc:"Number of bytes written"`
}

type CreateCommandRequest struct {
	Args    []string          `json:"args" required:"true" minItems:"1" doc:"Argument vector: Args[0] is the executable path, Args[1:] are its arguments"`
	Env     map[string]string `json:"env,omitempty" doc:"Environment variable overrides"`
	Cwd     string            `json:"cwd,omitempty" doc:"Working directory inside the sandbox. Defaults to the target container process's working directory if omitted."`
	Timeout *int              `json:"timeout,omitempty" minimum:"1" doc:"Max execution time in seconds"`
}

type CreateCommandResponse struct {
	CommandID string `json:"commandId" doc:"Unique command identifier"`
}

type CommandStatusResponse struct {
	ExitCode *int `json:"exitCode" doc:"Process exit code, null if still running"`
}
