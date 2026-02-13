package handlers

import (
	"io"

	"github.com/danielgtaylor/huma/v2"
)

type HealthResponse struct {
	Status string `json:"status" example:"ok" doc:"Health status"`
}

type HealthOutput struct {
	Body HealthResponse
}

// --- Sandbox request types ---

type CreateSandboxInput struct {
	Body CreateSandboxRequest
}

type ContainerSpec struct {
	Image     string            `json:"image" required:"true" doc:"Container image"`
	Command   []string          `json:"command,omitempty" doc:"Override the container entrypoint. Defaults to sleep infinity if omitted."`
	Env       map[string]string `json:"env,omitempty" doc:"Environment variables"`
	Resources *ResourcesSpec    `json:"resources,omitempty" doc:"Resource requests and limits"`
}

type PodTemplate struct {
	Container ContainerSpec `json:"container" required:"true" doc:"Primary sandbox container"`
}

type CreateSandboxRequest struct {
	PodTemplate           PodTemplate  `json:"podTemplate" required:"true" doc:"Pod template"`
	ActiveDeadlineSeconds *int64       `json:"activeDeadlineSeconds,omitempty" minimum:"1" doc:"Max lifetime in seconds. Omit for no timeout"`
	Network               *NetworkSpec `json:"network,omitempty" doc:"Network isolation config"`
}

type ResourcesSpec struct {
	Limits   *ResourceList `json:"limits,omitempty" doc:"Resource limits"`
	Requests *ResourceList `json:"requests,omitempty" doc:"Resource requests"`
}

type ResourceList struct {
	CPU              string `json:"cpu,omitempty" example:"125m" doc:"CPU (K8s quantity)"`
	Memory           string `json:"memory,omitempty" example:"512Mi" doc:"Memory (K8s quantity)"`
	EphemeralStorage string `json:"ephemeralStorage,omitempty" example:"1Gi" doc:"Ephemeral storage (K8s quantity)"`
}

type NetworkSpec struct {
	AllowAllInternet   *bool    `json:"allowAllInternet,omitempty" doc:"Allow public internet egress"`
	AllowClusterDNS    *bool    `json:"allowClusterDNS,omitempty" doc:"Allow cluster DNS queries"`
	AllowedEgressCIDRs []string `json:"allowedEgressCIDRs,omitempty" doc:"Allowed egress CIDRs"`
	Nameservers        []string `json:"nameservers,omitempty" doc:"Custom DNS servers (max 3)"`
}

type GetSandboxInput struct {
	ID string `path:"id" doc:"Sandbox identifier"`
}

type DeleteSandboxInput struct {
	ID string `path:"id" doc:"Sandbox identifier"`
}

// --- Sandbox response types ---
// Response types omit env vars (write-only) to avoid leaking secrets.

type ContainerInfo struct {
	Image     string         `json:"image" doc:"Container image"`
	Command   []string       `json:"command,omitempty" doc:"Container entrypoint override"`
	Resources *ResourcesSpec `json:"resources,omitempty" doc:"Resource requests and limits"`
}

type PodTemplateInfo struct {
	Container ContainerInfo `json:"container" doc:"Primary sandbox container"`
}

type CreateSandboxOutput struct {
	Body SandboxResponse
}

type GetSandboxOutput struct {
	Body SandboxResponse
}

type ListSandboxesOutput struct {
	Body ListSandboxesResponse
}

type SandboxResponse struct {
	ID                    string          `json:"id" doc:"Sandbox identifier"`
	PodTemplate           PodTemplateInfo `json:"podTemplate" doc:"Pod template"`
	ActiveDeadlineSeconds *int64          `json:"activeDeadlineSeconds,omitempty" doc:"Max lifetime in seconds"`
	Network               *NetworkSpec    `json:"network,omitempty" doc:"Network isolation config"`
	Status                string          `json:"status" doc:"Sandbox status" enum:"creating,running,shuttingDown,failed,stopped,unknown"`
	CreationTimestamp     string          `json:"creationTimestamp" doc:"Creation UTC timestamp in RFC3339 format"`
}

type SandboxSummary struct {
	ID                string `json:"id" doc:"Sandbox identifier"`
	Status            string `json:"status" doc:"Sandbox status"`
	CreationTimestamp string `json:"creationTimestamp" doc:"Creation timestamp"`
}

type ListSandboxesResponse struct {
	Sandboxes []SandboxSummary `json:"sandboxes" doc:"List of sandboxes"`
}

// --- Filesystem types ---

// BodyStream provides streaming access to request body via Huma's Resolver pattern.
// See https://github.com/danielgtaylor/huma/issues/749
type BodyStream struct {
	Stream io.Reader
}

func (b *BodyStream) Resolve(ctx huma.Context) []error {
	b.Stream = ctx.BodyReader()
	return nil
}

type FilesystemWriteInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" doc:"Destination path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	BodyStream
}

type FilesystemWriteResponse struct {
	AbsolutePath string `json:"absolutePath" example:"/workspace/file.txt" doc:"Absolute path where file was written"`
	BytesWritten int64  `json:"bytesWritten" example:"1024" doc:"Number of bytes written"`
}

type FilesystemWriteOutput struct {
	Body FilesystemWriteResponse
}

type FilesystemReadInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Path      string `query:"path" required:"true" doc:"Source path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

// --- Command types ---

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

type CreateSandboxCommandInput struct {
	ID        string `path:"id" doc:"Sandbox identifier"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	Body      CreateCommandRequest
}

type CreateSandboxCommandOutput struct {
	Body CreateCommandResponse
}

type GetSandboxCommandStatusInput struct {
	ID    string `path:"id" doc:"Sandbox identifier"`
	CmdID string `path:"cmdId" doc:"Command identifier"`
}

type GetSandboxCommandStatusOutput struct {
	Body CommandStatusResponse
}

type GetSandboxCommandStreamInput struct {
	ID     string `path:"id" doc:"Sandbox identifier"`
	CmdID  string `path:"cmdId" doc:"Command identifier"`
	Offset int64  `query:"offset,omitempty" doc:"Byte offset to resume from (default 0)"`
}

type PostSandboxCommandStdinInput struct {
	ID    string `path:"id" doc:"Sandbox identifier"`
	CmdID string `path:"cmdId" doc:"Command identifier"`
	BodyStream
}

type DeleteSandboxCommandInput struct {
	ID    string `path:"id" doc:"Sandbox identifier"`
	CmdID string `path:"cmdId" doc:"Command identifier"`
}
