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

package snapshot

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	apigateway "github.com/isola-ai/isola/internal/api-gateway"
)

// --- Request types ---

type CreateRootfsSnapshotInput struct {
	ID   string `path:"id" doc:"Sandbox identifier"`
	Body CreateRootfsSnapshotRequest
}

type CreateRootfsSnapshotRequest struct {
	// SnapshotName is the name used for the snapshot storage key.
	// Two snapshots with the same name produce the same storage key — the newer one overwrites the older.
	SnapshotName            string `json:"snapshotName" required:"true" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Snapshot name used for the storage key. Must be a valid DNS label."`
	Container               string `json:"container,omitempty" doc:"Container to snapshot (defaults to the first container)"`
	ActiveDeadlineSeconds   *int64 `json:"activeDeadlineSeconds,omitempty" minimum:"1" doc:"Max duration in seconds for the snapshot job"`
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty" minimum:"0" doc:"Seconds after completion before auto-deletion"`
}

type GetRootfsSnapshotInput struct {
	ID     string `path:"id" doc:"Sandbox identifier"`
	SnapID string `path:"snapId" doc:"Rootfs snapshot identifier"`
}

// --- Response types ---

type CreateRootfsSnapshotOutput struct {
	Body RootfsSnapshotResponse
}

type GetRootfsSnapshotOutput struct {
	Body RootfsSnapshotResponse
}

type RootfsSnapshotResponse struct {
	ID                      string  `json:"id" doc:"Rootfs snapshot identifier"`
	SandboxID               string  `json:"sandboxId" doc:"Sandbox identifier"`
	SnapshotName            string  `json:"snapshotName" doc:"Snapshot name used for the storage key"`
	Container               string  `json:"container,omitempty" doc:"Container that was snapshotted"`
	Status                  string  `json:"status" doc:"Snapshot status" enum:"pending,inProgress,complete,failed"`
	FailureMessage          *string `json:"failureMessage,omitempty" doc:"Failure details when status is failed"`
	SnapshotKey             *string `json:"snapshotKey,omitempty" doc:"Object storage key for the snapshot tarball"`
	CreationTimestamp       string  `json:"creationTimestamp" doc:"Creation UTC timestamp in RFC3339 format"`
	StartTime               *string `json:"startTime,omitempty" doc:"When the snapshot job started (RFC3339)"`
	CompletionTime          *string `json:"completionTime,omitempty" doc:"When the snapshot finished (RFC3339)"`
	ActiveDeadlineSeconds   *int64  `json:"activeDeadlineSeconds,omitempty" doc:"Max duration in seconds for the snapshot job"`
	TTLSecondsAfterFinished *int32  `json:"ttlSecondsAfterFinished,omitempty" doc:"Seconds after completion before auto-deletion"`
}

// --- Handlers ---

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
	_, err := apigateway.GetReadySandbox(ctx, h.k8sClient, h.sandboxNamespace, input.ID, h.logger)
	if err != nil {
		return nil, err
	}

	cr := requestToRootfsSnapshotCR(input.Body, input.ID, h.sandboxNamespace)

	if err := h.k8sClient.Create(ctx, cr); err != nil {
		h.logger.Error("failed to create rootfs snapshot", "error", err, "sandbox", input.ID)
		return nil, apigateway.K8sErrorToHuma(err, "failed to create rootfs snapshot")
	}

	resp := rootfsSnapshotToResponse(cr)
	return &CreateRootfsSnapshotOutput{Body: resp}, nil
}

func (h *Handlers) GetRootfsSnapshot(ctx context.Context, input *GetRootfsSnapshotInput) (*GetRootfsSnapshotOutput, error) {
	snap := &sandboxv1alpha1.RootfsSnapshot{}
	key := client.ObjectKey{Name: input.SnapID, Namespace: h.sandboxNamespace}

	if err := h.k8sClient.Get(ctx, key, snap); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil, huma.Error404NotFound("rootfs snapshot not found")
		}
		h.logger.Error("failed to get rootfs snapshot", "error", err, "id", input.SnapID)
		return nil, apigateway.K8sErrorToHuma(err, "failed to get rootfs snapshot")
	}

	// Verify the snapshot belongs to the requested sandbox (avoid leaking cross-sandbox info)
	if snap.Spec.SandboxName != input.ID {
		return nil, huma.Error404NotFound("rootfs snapshot not found")
	}

	resp := rootfsSnapshotToResponse(snap)
	return &GetRootfsSnapshotOutput{Body: resp}, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "createRootfsSnapshot",
		Method:        http.MethodPost,
		Path:          "/sandboxes/{id}/rootfssnapshots",
		Summary:       "Create a rootfs snapshot",
		Tags:          []string{"rootfssnapshots"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, h.PostRootfsSnapshot)

	huma.Register(api, huma.Operation{
		OperationID: "getRootfsSnapshot",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}/rootfssnapshots/{snapId}",
		Summary:     "Get rootfs snapshot details",
		Tags:        []string{"rootfssnapshots"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetRootfsSnapshot)
}
