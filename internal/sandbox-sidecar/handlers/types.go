package handlers

import (
	"io"

	"github.com/danielgtaylor/huma/v2"

	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
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
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required." `
	BodyStream
}

type FilesystemWriteOutput struct {
	Body sidecarapi.FilesystemWriteResponse
}
