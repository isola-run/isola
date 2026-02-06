package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

const sidecarPort = 10032

type ExecHandlers struct {
	logger           *slog.Logger
	k8sClient        client.Client
	sandboxNamespace string
}

func NewExecHandlers(logger *slog.Logger, k8sClient client.Client, sandboxNamespace string) *ExecHandlers {
	return &ExecHandlers{
		logger:           logger,
		k8sClient:        k8sClient,
		sandboxNamespace: sandboxNamespace,
	}
}

type ExecProxyInput struct {
	SandboxID string `path:"sandboxID" doc:"Sandbox ID"`
}

func RegisterExecRoutes(api huma.API, h *ExecHandlers) {
	huma.Register(api, huma.Operation{
		OperationID: "ws-sandbox-exec",
		Method:      http.MethodGet,
		Path:        "/v1/sandboxes/{sandboxID}/ws/exec",
		Summary:     "Interactive shell via WebSocket",
		Tags:        []string{"exec"},
		Responses: map[string]*huma.Response{
			"101": {Description: "Switching Protocols — WebSocket connection established"},
		},
	}, h.ExecProxy)
}

func (h *ExecHandlers) ExecProxy(ctx context.Context, input *ExecProxyInput) (*huma.StreamResponse, error) {
	var sandbox sandboxv1alpha1.Sandbox
	if err := h.k8sClient.Get(ctx, client.ObjectKey{
		Namespace: h.sandboxNamespace,
		Name:      input.SandboxID,
	}, &sandbox); err != nil {
		return nil, huma.Error404NotFound("sandbox not found")
	}

	if !isSandboxReady(&sandbox) {
		return nil, huma.Error503ServiceUnavailable("sandbox not ready")
	}

	if sandbox.Status.PodIP == "" {
		return nil, huma.Error503ServiceUnavailable("sandbox pod IP not assigned")
	}

	podIP := sandbox.Status.PodIP

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			r, w := humachi.Unwrap(humaCtx)

			target := &url.URL{
				Scheme: "http",
				Host:   fmt.Sprintf("%s:%d", podIP, sidecarPort),
			}
			proxy := &httputil.ReverseProxy{
				Rewrite: func(pr *httputil.ProxyRequest) {
					pr.SetURL(target)
					pr.Out.URL.Path = "/ws/exec"
					pr.Out.URL.RawQuery = r.URL.RawQuery
				},
			}
			proxy.ServeHTTP(w, r)
		},
	}, nil
}

func isSandboxReady(sandbox *sandboxv1alpha1.Sandbox) bool {
	cond := meta.FindStatusCondition(sandbox.Status.Conditions, string(sandboxv1alpha1.SandboxReady))
	return cond != nil && cond.Status == metav1.ConditionTrue
}
