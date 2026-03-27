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

package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	gonanoid "github.com/matoous/go-nanoid/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	apigateway "github.com/isola-ai/isola/internal/api-gateway"
)

// --- Request types ---

type CreateSandboxInput struct {
	Body CreateSandboxRequest
}

type ContainerSpec struct {
	Image   string            `json:"image" required:"true" minLength:"1" doc:"Container image"`
	Command []string          `json:"command,omitempty" doc:"Override the container entrypoint. Defaults to sleep infinity if omitted."`
	Env     map[string]string `json:"env,omitempty" doc:"Environment variables"`
	// todo benl: those are enforced in gvisor only on the sandbox container, consider moving this to podtemplate and setting pod limits (though they don't support ephemeral storage limits)
	Resources *ResourcesSpec `json:"resources,omitempty" doc:"Resource requests and limits"`
}

type PodTemplate struct {
	// todo benl: list of containers?
	Container ContainerSpec `json:"container" required:"true" doc:"Primary sandbox container"`
}

type CreateSandboxRequest struct {
	PodTemplate           PodTemplate            `json:"podTemplate" required:"true" doc:"Pod template"`
	TimeoutSeconds        *int64                 `json:"timeoutSeconds,omitempty" minimum:"1" doc:"Max lifetime in seconds. Omit for no timeout"`
	StartupTimeoutSeconds *int64                 `json:"startupTimeoutSeconds,omitempty" minimum:"1" doc:"Max seconds for the sandbox to become Ready. Defaults to 60 if omitted."`
	Network               *NetworkSpec           `json:"network,omitempty" doc:"Network isolation config"`
	RootfsSnapshotSources []RootfsSnapshotSource `json:"rootfsSnapshotSources,omitempty" maxItems:"16" doc:"Rootfs snapshots to restore into containers at creation time. Files on separately-mounted filesystems (e.g. /tmp, which gVisor mounts as a separate tmpfs) are not included."`
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
	AllowInternetEgress *bool    `json:"allowInternetEgress,omitempty" doc:"Allow public internet egress"`
	AllowClusterDNS     *bool    `json:"allowClusterDNS,omitempty" doc:"Allow cluster DNS queries"`
	AllowedEgressCIDRs  []string `json:"allowedEgressCIDRs,omitempty" doc:"Allowed egress CIDRs"`
	Nameservers         []string `json:"nameservers,omitempty" maxItems:"3" doc:"Custom DNS servers (max 3)"`
}

type RootfsSnapshotSource struct {
	SnapshotName  string `json:"snapshotName" required:"true" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Name of the rootfs snapshot to restore from."`
	ContainerName string `json:"containerName,omitempty" minLength:"1" maxLength:"63" pattern:"^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" doc:"Container to restore. If empty and there is only one container, that container will be used. Required if there are multiple containers."`
}

type GetSandboxInput struct {
	ID string `path:"id" doc:"Sandbox identifier"`
}

type DeleteSandboxInput struct {
	ID string `path:"id" doc:"Sandbox identifier"`
}

// --- Response types ---
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
	ID                    string                 `json:"id" doc:"Sandbox identifier"`
	PodTemplate           PodTemplateInfo        `json:"podTemplate" doc:"Pod template"`
	TimeoutSeconds        *int64                 `json:"timeoutSeconds,omitempty" doc:"Max lifetime in seconds"`
	StartupTimeoutSeconds *int64                 `json:"startupTimeoutSeconds,omitempty" doc:"Max seconds for the sandbox to become Ready"`
	Network               *NetworkSpec           `json:"network,omitempty" doc:"Network isolation config"`
	RootfsSnapshotSources []RootfsSnapshotSource `json:"rootfsSnapshotSources,omitempty" doc:"Rootfs snapshot restore configuration."`
	Status                string                 `json:"status" doc:"Sandbox status" enum:"creating,running,shuttingDown,failed,stopped,unknown"`
	CreationTimestamp     string                 `json:"creationTimestamp" doc:"Creation UTC timestamp in RFC3339 format"`
}

type SandboxSummary struct {
	ID                string `json:"id" doc:"Sandbox identifier"`
	Status            string `json:"status" doc:"Sandbox status" enum:"creating,running,shuttingDown,failed,stopped,unknown"`
	CreationTimestamp string `json:"creationTimestamp" doc:"Creation timestamp"`
}

type ListSandboxesResponse struct {
	Sandboxes []SandboxSummary `json:"sandboxes" doc:"List of sandboxes"`
}

// --- Handlers ---

const (
	sandboxNameLength = 22
	letterAlphabet    = "abcdefghijklmnopqrstuvwxyz"
	fullAlphabet      = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// generateSandboxName creates a unique sandbox name suitable for Kubernetes DNS-1123 labels.
func generateSandboxName() (string, error) {
	first, err := gonanoid.Generate(letterAlphabet, 1)
	if err != nil {
		return "", fmt.Errorf("generate first char: %w", err)
	}

	rest, err := gonanoid.Generate(fullAlphabet, sandboxNameLength-1)
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

func (h *Handlers) PostSandbox(ctx context.Context, input *CreateSandboxInput) (*CreateSandboxOutput, error) {
	req := input.Body

	name, err := generateSandboxName()
	if err != nil {
		h.logger.Error("failed to generate sandbox name", "error", err)
		return nil, huma.Error500InternalServerError("failed to generate sandbox name")
	}

	sb, err := requestToSandboxCR(req, name, h.sandboxNamespace)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}

	if err := h.k8sClient.Create(ctx, sb); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, huma.Error409Conflict("sandbox already exists")
		}
		h.logger.Error("failed to create sandbox", "error", err)
		return nil, apigateway.K8sErrorToHuma(err, "failed to create sandbox")
	}

	resp := sandboxToResponse(sb)
	return &CreateSandboxOutput{Body: resp}, nil
}

func (h *Handlers) GetSandbox(ctx context.Context, input *GetSandboxInput) (*GetSandboxOutput, error) {
	sb := &sandboxv1alpha1.Sandbox{}
	key := client.ObjectKey{Name: input.ID, Namespace: h.sandboxNamespace}

	if err := h.k8sClient.Get(ctx, key, sb); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("sandbox %q not found", input.ID))
		}
		h.logger.Error("failed to get sandbox", "error", err, "id", input.ID)
		return nil, apigateway.K8sErrorToHuma(err, "failed to get sandbox")
	}

	resp := sandboxToResponse(sb)
	return &GetSandboxOutput{Body: resp}, nil
}

func (h *Handlers) ListSandboxes(ctx context.Context, _ *struct{}) (*ListSandboxesOutput, error) {
	list := &sandboxv1alpha1.SandboxList{}
	// NOT PAGINATED! Be careful if the number of sandboxes gets large.
	// controller-runtime's cached client supports Limit (stops reading early) but rejects
	// Continue ("continue list option is not supported by the cache").
	if err := h.k8sClient.List(ctx, list, client.InNamespace(h.sandboxNamespace)); err != nil {
		h.logger.Error("failed to list sandboxes", "error", err)
		return nil, apigateway.K8sErrorToHuma(err, "failed to list sandboxes")
	}

	// make (not var) ensures non-nil slice so JSON serializes as [] not null
	summaries := make([]SandboxSummary, len(list.Items))
	for i := range list.Items {
		summaries[i] = sandboxToSummary(&list.Items[i])
	}

	return &ListSandboxesOutput{Body: ListSandboxesResponse{Sandboxes: summaries}}, nil
}

func (h *Handlers) DeleteSandbox(ctx context.Context, input *DeleteSandboxInput) (*struct{}, error) {
	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      input.ID,
			Namespace: h.sandboxNamespace,
		},
	}

	if err := client.IgnoreNotFound(h.k8sClient.Delete(ctx, sb)); err != nil {
		h.logger.Error("failed to delete sandbox", "error", err, "id", input.ID)
		return nil, apigateway.K8sErrorToHuma(err, "failed to delete sandbox")
	}

	return nil, nil
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "createSandbox",
		Method:        http.MethodPost,
		Path:          "/sandboxes",
		Summary:       "Create a sandbox",
		Tags:          []string{"sandboxes"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, h.PostSandbox)

	huma.Register(api, huma.Operation{
		OperationID: "listSandboxes",
		Method:      http.MethodGet,
		Path:        "/sandboxes",
		Summary:     "List sandboxes",
		Description: "Eventually consistent: a sandbox returned by POST may not appear immediately in the list. Clients should retry or poll if a recently-created sandbox is missing.",
		Tags:        []string{"sandboxes"},
	}, h.ListSandboxes)

	huma.Register(api, huma.Operation{
		OperationID: "getSandbox",
		Method:      http.MethodGet,
		Path:        "/sandboxes/{id}",
		Summary:     "Get sandbox details",
		Description: "Eventually consistent: a sandbox returned by POST may briefly return 404 on GET. Clients should retry if polling a recently-created sandbox.",
		Tags:        []string{"sandboxes"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetSandbox)

	huma.Register(api, huma.Operation{
		OperationID: "deleteSandbox",
		Method:      http.MethodDelete,
		Path:        "/sandboxes/{id}",
		Summary:     "Delete a sandbox",
		Tags:        []string{"sandboxes"},
	}, h.DeleteSandbox)
}
