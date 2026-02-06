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
	Path      string `query:"path" required:"true" doc:"Destination path (absolute or relative to container cwd)"`
	Container string `query:"container" doc:"Container name (defaults to main container)"`
	BodyStream
}

type FilesystemWriteResponse struct {
	AbsolutePath string `json:"absolutePath" example:"/workspace/file.txt" doc:"Absolute path where file was written"`
	BytesWritten int64  `json:"bytesWritten" example:"1024" doc:"Number of bytes written"`
	Container    string `json:"container,omitempty" example:"main" doc:"Container name"`
}

type FilesystemWriteOutput struct {
	Body FilesystemWriteResponse
}
