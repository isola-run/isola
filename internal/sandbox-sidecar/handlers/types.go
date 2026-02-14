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
	Path      string `query:"path" required:"true" minLength:"1" doc:"Destination path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	BodyStream
}

type FilesystemWriteOutput struct {
	Body sidecarapi.FilesystemWriteResponse
}

type FilesystemReadInput struct {
	Path      string `query:"path" required:"true" minLength:"1" doc:"Source path (absolute or relative to container cwd)"`
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
}

// --- Command types ---

type CreateCommandInput struct {
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	Body      sidecarapi.CreateCommandRequest
}

type CreateCommandOutput struct {
	Body sidecarapi.CreateCommandResponse
}

type GetCommandStatusInput struct {
	CmdID string `path:"cmdId" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
}

type GetCommandStatusOutput struct {
	Body sidecarapi.CommandStatusResponse
}

type GetCommandStreamInput struct {
	CmdID  string `path:"cmdId" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	Offset int64  `query:"offset,omitempty" minimum:"0" doc:"Byte offset to resume from (default 0)"`
}

type PostCommandStdinInput struct {
	CmdID string `path:"cmdId" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	BodyStream
}

type DeleteCommandInput struct {
	CmdID string `path:"cmdId" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
}
