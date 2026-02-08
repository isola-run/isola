package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
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

			clientConn, err := websocket.Accept(w, r, nil)
			if err != nil {
				h.logger.Error("websocket accept failed", "error", err)
				return
			}
			defer clientConn.CloseNow() //nolint:errcheck // best-effort cleanup

			sidecarURL := fmt.Sprintf("ws://%s:%d/ws/exec", podIP, sidecarPort)
			if r.URL.RawQuery != "" {
				sidecarURL += "?" + r.URL.RawQuery
			}

			// Timeout covers only the TCP+HTTP handshake; once Dial returns, the
			// context is no longer tied to the connection's lifetime.
			dialCtx, dialCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer dialCancel()
			sidecarConn, resp, err := websocket.Dial(dialCtx, sidecarURL, nil)
			if resp != nil && resp.Body != nil {
				_ = resp.Body.Close()
			}
			if err != nil {
				h.logger.Error("sidecar dial failed", "error", err, "url", sidecarURL)
				_ = clientConn.Close(websocket.StatusInternalError, "failed to connect to sandbox")
				return
			}
			defer sidecarConn.CloseNow() //nolint:errcheck // best-effort cleanup

			h.logger.Info("exec proxy established", "sandbox", input.SandboxID, "sidecar", sidecarURL)

			errc := make(chan error, 2)
			go func() { errc <- proxyMessages(sidecarConn, clientConn) }()
			go func() { errc <- proxyMessages(clientConn, sidecarConn) }()

			// When either direction breaks, tear down both sides.
			err = <-errc
			h.logger.Info("exec proxy closed", "sandbox", input.SandboxID, "cause", err)
			_ = clientConn.Close(websocket.StatusNormalClosure, "")
			_ = sidecarConn.Close(websocket.StatusNormalClosure, "")
		},
	}, nil
}

// proxyMessages reads messages from src and writes them to dst until an error occurs.
func proxyMessages(dst, src *websocket.Conn) error {
	for {
		typ, data, err := src.Read(context.Background())
		if err != nil {
			return err
		}
		if err := dst.Write(context.Background(), typ, data); err != nil {
			return err
		}
	}
}

func isSandboxReady(sandbox *sandboxv1alpha1.Sandbox) bool {
	cond := meta.FindStatusCondition(sandbox.Status.Conditions, string(sandboxv1alpha1.SandboxReady))
	return cond != nil && cond.Status == metav1.ConditionTrue
}
