package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"github.com/isola-ai/isola-sb/internal/api-gateway/sidecar"
)

// SandboxHandlers handles sandbox-related API requests.
type SandboxHandlers struct {
	logger          *slog.Logger
	k8sClient       client.Client
	sidecarClient   *sidecar.Client
	sandboxNamespace string
}

// NewSandboxHandlers creates a new SandboxHandlers instance.
func NewSandboxHandlers(logger *slog.Logger, k8sClient client.Client, sandboxNamespace string) *SandboxHandlers {
	return &SandboxHandlers{
		logger:          logger,
		k8sClient:       k8sClient,
		sidecarClient:   sidecar.NewClient(),
		sandboxNamespace: sandboxNamespace,
	}
}

// getSandboxPodIP retrieves the pod IP for a sandbox.
func (h *SandboxHandlers) getSandboxPodIP(ctx context.Context, sandboxName string) (string, error) {
	sandbox := &sandboxv1alpha1.Sandbox{}
	if err := h.k8sClient.Get(ctx, client.ObjectKey{
		Namespace: h.sandboxNamespace,
		Name:      sandboxName,
	}, sandbox); err != nil {
		return "", fmt.Errorf("get sandbox: %w", err)
	}

	if sandbox.Status.PodIP == "" {
		return "", fmt.Errorf("sandbox pod IP not available (sandbox may not be ready)")
	}

	return sandbox.Status.PodIP, nil
}

// PostExec executes a command in the sandbox.
func (h *SandboxHandlers) PostExec(ctx context.Context, input *SandboxExecInput) (*SandboxExecOutput, error) {
	podIP, err := h.getSandboxPodIP(ctx, input.SandboxName)
	if err != nil {
		h.logger.Error("failed to get sandbox pod IP", "error", err, "sandbox", input.SandboxName)
		return nil, huma.Error404NotFound(fmt.Sprintf("sandbox not found or not ready: %v", err))
	}

	execReq := &sidecar.ExecRequest{
		Cmd:       input.Cmd,
		Args:      input.Args,
		Cwd:       input.Cwd,
		Env:       input.Env,
		Timeout:   input.Timeout,
		Container: input.Container,
	}

	resp, err := h.sidecarClient.Exec(ctx, podIP, execReq)
	if err != nil {
		h.logger.Error("exec failed", "error", err, "sandbox", input.SandboxName, "cmd", input.Cmd)
		return nil, huma.Error502BadGateway(fmt.Sprintf("failed to execute command: %v", err))
	}

	return &SandboxExecOutput{
		Body: SandboxExecResponse{
			ExitCode: resp.ExitCode,
			Stdout:   resp.Stdout,
			Stderr:   resp.Stderr,
		},
	}, nil
}
