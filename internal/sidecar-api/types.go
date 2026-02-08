// Package sidecarapi defines shared contract types between the api-gateway (client) and
// sandbox-sidecar (server). Only types that are identical across both services belong here.
package sidecarapi

type FilesystemWriteResponse struct {
	AbsolutePath string `json:"absolutePath" example:"/workspace/file.txt" doc:"Absolute path where file was written"`
	BytesWritten int64  `json:"bytesWritten" example:"1024" doc:"Number of bytes written"`
}
