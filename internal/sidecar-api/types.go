// Package sidecarapi defines shared contract types between the api-gateway (client) and
// sandbox-sidecar (server). Only types that are identical across both services belong here.
package sidecarapi

type FilesystemWriteResponse struct {
	AbsolutePath string `json:"absolutePath" example:"/workspace/file.txt" doc:"Absolute path where file was written"`
	BytesWritten int64  `json:"bytesWritten" example:"1024" doc:"Number of bytes written"`
}

type CreateCommandRequest struct {
	Cmd     string            `json:"cmd" required:"true" minLength:"1" doc:"Executable path"`
	Args    []string          `json:"args,omitempty" doc:"Arguments to the executable"`
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
