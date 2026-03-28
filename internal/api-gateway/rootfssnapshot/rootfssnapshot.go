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

package rootfssnapshot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	gonanoid "github.com/matoous/go-nanoid/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	apigateway "github.com/isola-ai/isola/internal/api-gateway"
)

type CreateRootfsSnapshotInput struct {
	Body CreateRootfsSnapshotRequest
}

type CreateRootfsSnapshotRequest struct {
	SandboxID               string `json:"sandboxId" required:"true" minLength:"1" doc:"ID of the sandbox to snapshot (as returned by POST /v1/sandboxes)"`
	SnapshotName            string `json:"snapshotName" required:"true" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Identifier for this snapshot, used both as the storage key and as the restore reference. To restore from this snapshot, pass this value as rootfsSnapshotSources[].snapshotName when creating a sandbox."`
	ContainerName           string `json:"containerName,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container to snapshot. Defaults to the first container if omitted."`
	TimeoutSeconds          *int64 `json:"timeoutSeconds,omitempty" minimum:"1" doc:"Max duration in seconds for the snapshot job. Defaults to 300 if omitted."`
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty" minimum:"0" doc:"Seconds to retain the resource after completion. 0 means immediate deletion. Defaults to 300 if omitted."`
}

type GetRootfsSnapshotInput struct {
	ID string `path:"id" doc:"RootfsSnapshot identifier"`
}

type CreateRootfsSnapshotOutput struct {
	Body RootfsSnapshotResponse
}

type GetRootfsSnapshotOutput struct {
	Body RootfsSnapshotResponse
}

type RootfsSnapshotResponse struct {
	ID                      string `json:"id" doc:"RootfsSnapshot identifier"`
	SandboxID               string `json:"sandboxId" doc:"ID of the sandbox being snapshotted"`
	SnapshotName            string `json:"snapshotName" doc:"Snapshot storage key and restore reference"`
	ContainerName           string `json:"containerName,omitempty" doc:"Container being snapshotted"`
	TimeoutSeconds          *int64 `json:"timeoutSeconds,omitempty" doc:"Max duration in seconds for the snapshot job"`
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty" doc:"Seconds to retain the resource after completion"`
	Status                  string `json:"status" doc:"Snapshot status" enum:"pending,in_progress,complete,failed"`
	CreationTimestamp       string `json:"creationTimestamp" doc:"Creation UTC timestamp in RFC3339 format"`
}

const (
	snapshotIDLength = 22
	letterAlphabet   = "abcdefghijklmnopqrstuvwxyz"
	fullAlphabet     = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// generateSnapshotID creates a unique snapshot ID suitable for Kubernetes DNS-1123 labels.
func generateSnapshotID() (string, error) {
	first, err := gonanoid.Generate(letterAlphabet, 1)
	if err != nil {
		return "", fmt.Errorf("generate first char: %w", err)
	}

	rest, err := gonanoid.Generate(fullAlphabet, snapshotIDLength-1)
	if err != nil {
		return "", fmt.Errorf("generate remaining chars: %w", err)
	}

	return first + rest, nil
}

type Handlers struct {
	logger           *slog.Logger
	k8sClient        client.Client
	sandboxNamespace string
}

func New(logger *slog.Logger, sandboxNamespace string, k8sClient client.Client) *Handlers {
	return &Handlers{
		logger:           logger,
		k8sClient:        k8sClient,
		sandboxNamespace: sandboxNamespace,
	}
}

func (h *Handlers) PostRootfsSnapshot(ctx context.Context, input *CreateRootfsSnapshotInput) (*CreateRootfsSnapshotOutput, error) {
	// by design, we currently DO NOT verify the target sandbox existence before creating the rootfs snapshot CR. This is because:
	// 1) the controller will handle the missing sandbox case anyway.
	// 2) if we handle it here, we hit eventual consistency issues where the informer cache may not have the sandbox immediately after it's created.
	// we might revisit this decision in the future if and when it poses actual problems for users.
	req := input.Body

	name, err := generateSnapshotID()
	if err != nil {
		h.logger.Error("failed to generate snapshot id", "error", err)
		return nil, huma.Error500InternalServerError("failed to generate snapshot id")
	}

	cr := requestToRootfsSnapshotCR(req, name, h.sandboxNamespace)

	if err := h.k8sClient.Create(ctx, cr); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, huma.Error409Conflict("rootfs snapshot already exists")
		}
		h.logger.Error("failed to create rootfs snapshot", "error", err)
		return nil, apigateway.K8sErrorToHuma(err, "failed to create rootfs snapshot")
	}

	resp := rootfsSnapshotToResponse(cr)
	return &CreateRootfsSnapshotOutput{Body: resp}, nil
}

func (h *Handlers) GetRootfsSnapshot(ctx context.Context, input *GetRootfsSnapshotInput) (*GetRootfsSnapshotOutput, error) {
	rs := &sandboxv1alpha1.RootfsSnapshot{}
	key := client.ObjectKey{Name: input.ID, Namespace: h.sandboxNamespace}

	if err := h.k8sClient.Get(ctx, key, rs); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("rootfs snapshot %q not found", input.ID))
		}
		h.logger.Error("failed to get rootfs snapshot", "error", err, "id", input.ID)
		return nil, apigateway.K8sErrorToHuma(err, "failed to get rootfs snapshot")
	}

	resp := rootfsSnapshotToResponse(rs)
	return &GetRootfsSnapshotOutput{Body: resp}, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "createRootfsSnapshot",
		Method:        http.MethodPost,
		Path:          "/rootfs-snapshots",
		Summary:       "Create a rootfs snapshot",
		Description:   "Creates a snapshot of a sandbox's root filesystem overlay upper layer.",
		Tags:          []string{"rootfs-snapshots"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, h.PostRootfsSnapshot)

	huma.Register(api, huma.Operation{
		OperationID: "getRootfsSnapshot",
		Method:      http.MethodGet,
		Path:        "/rootfs-snapshots/{id}",
		Summary:     "Get rootfs snapshot details",
		Description: "Eventually consistent: a snapshot returned by POST may briefly return 404 on GET. Clients should retry if polling a recently-created snapshot.",
		Tags:        []string{"rootfs-snapshots"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetRootfsSnapshot)
}
