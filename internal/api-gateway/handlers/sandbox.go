package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	gonanoid "github.com/matoous/go-nanoid/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

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

type SandboxHandlers struct {
	logger           *slog.Logger
	k8sClient        client.Client
	sandboxNamespace string
}

func NewSandboxHandlers(logger *slog.Logger, sandboxNamespace string, k8sClient client.Client) *SandboxHandlers {
	return &SandboxHandlers{
		logger:           logger,
		k8sClient:        k8sClient,
		sandboxNamespace: sandboxNamespace,
	}
}

func (h *SandboxHandlers) PostSandbox(ctx context.Context, input *CreateSandboxInput) (*CreateSandboxOutput, error) {
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
		return nil, k8sErrorToHuma(err, "failed to create sandbox")
	}

	resp := sandboxToResponse(sb)
	return &CreateSandboxOutput{Body: resp}, nil
}

func (h *SandboxHandlers) GetSandbox(ctx context.Context, input *GetSandboxInput) (*GetSandboxOutput, error) {
	sb := &sandboxv1alpha1.Sandbox{}
	key := client.ObjectKey{Name: input.ID, Namespace: h.sandboxNamespace}

	if err := h.k8sClient.Get(ctx, key, sb); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, huma.Error404NotFound(fmt.Sprintf("sandbox %q not found", input.ID))
		}
		h.logger.Error("failed to get sandbox", "error", err, "id", input.ID)
		return nil, k8sErrorToHuma(err, "failed to get sandbox")
	}

	resp := sandboxToResponse(sb)
	return &GetSandboxOutput{Body: resp}, nil
}

func (h *SandboxHandlers) ListSandboxes(ctx context.Context, _ *struct{}) (*ListSandboxesOutput, error) {
	list := &sandboxv1alpha1.SandboxList{}
	// NOT PAGINATED! Be careful if the number of sandboxes gets large.
	// controller-runtime's cached client supports Limit (stops reading early) but rejects
	// Continue ("continue list option is not supported by the cache").
	if err := h.k8sClient.List(ctx, list, client.InNamespace(h.sandboxNamespace)); err != nil {
		h.logger.Error("failed to list sandboxes", "error", err)
		return nil, k8sErrorToHuma(err, "failed to list sandboxes")
	}

	// make (not var) ensures non-nil slice so JSON serializes as [] not null
	summaries := make([]SandboxSummary, len(list.Items))
	for i := range list.Items {
		summaries[i] = sandboxToSummary(&list.Items[i])
	}

	return &ListSandboxesOutput{Body: ListSandboxesResponse{Sandboxes: summaries}}, nil
}

func (h *SandboxHandlers) DeleteSandbox(ctx context.Context, input *DeleteSandboxInput) (*struct{}, error) {
	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      input.ID,
			Namespace: h.sandboxNamespace,
		},
	}

	if err := client.IgnoreNotFound(h.k8sClient.Delete(ctx, sb)); err != nil {
		h.logger.Error("failed to delete sandbox", "error", err, "id", input.ID)
		return nil, k8sErrorToHuma(err, "failed to delete sandbox")
	}

	return nil, nil
}
