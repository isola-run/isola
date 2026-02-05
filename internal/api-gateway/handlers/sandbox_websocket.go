package handlers

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	"github.com/isola-ai/isola-sb/internal/api-gateway/sidecar"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins; production should restrict this
	},
}

// WebSocketProxyHandler handles WebSocket proxy requests to sandbox sidecars.
type WebSocketProxyHandler struct {
	logger           *slog.Logger
	k8sClient        client.Client
	sandboxNamespace string
}

// NewWebSocketProxyHandler creates a new WebSocketProxyHandler.
func NewWebSocketProxyHandler(logger *slog.Logger, k8sClient client.Client, sandboxNamespace string) *WebSocketProxyHandler {
	return &WebSocketProxyHandler{
		logger:           logger,
		k8sClient:        k8sClient,
		sandboxNamespace: sandboxNamespace,
	}
}

// getSandboxPodIP retrieves the pod IP for a sandbox.
func (h *WebSocketProxyHandler) getSandboxPodIP(ctx context.Context, sandboxName string) (string, error) {
	sandbox := &sandboxv1alpha1.Sandbox{}
	if err := h.k8sClient.Get(ctx, client.ObjectKey{
		Namespace: h.sandboxNamespace,
		Name:      sandboxName,
	}, sandbox); err != nil {
		return "", err
	}

	if sandbox.Status.PodIP == "" {
		return "", nil
	}

	return sandbox.Status.PodIP, nil
}

// ExecStreamHandler handles WebSocket connections for streaming command execution.
func (h *WebSocketProxyHandler) ExecStreamHandler(w http.ResponseWriter, r *http.Request) {
	sandboxName := chi.URLParam(r, "sandbox_name")
	if sandboxName == "" {
		http.Error(w, "sandbox_name is required", http.StatusBadRequest)
		return
	}

	podIP, err := h.getSandboxPodIP(r.Context(), sandboxName)
	if err != nil {
		h.logger.Error("failed to get sandbox pod IP", "error", err, "sandbox", sandboxName)
		http.Error(w, "sandbox not found", http.StatusNotFound)
		return
	}
	if podIP == "" {
		http.Error(w, "sandbox not ready", http.StatusServiceUnavailable)
		return
	}

	// Build sidecar WebSocket URL with query params
	query := url.Values{}
	if cmd := r.URL.Query().Get("cmd"); cmd != "" {
		query.Set("cmd", cmd)
	}
	if args := r.URL.Query().Get("args"); args != "" {
		query.Set("args", args)
	}
	if cwd := r.URL.Query().Get("cwd"); cwd != "" {
		query.Set("cwd", cwd)
	}
	if container := r.URL.Query().Get("container"); container != "" {
		query.Set("container", container)
	}
	if timeout := r.URL.Query().Get("timeout"); timeout != "" {
		query.Set("timeout", timeout)
	}

	sidecarURL := sidecar.GetWebSocketURL(podIP, "/exec/stream", query)

	h.proxyWebSocket(w, r, sidecarURL)
}

// PTYHandler handles WebSocket connections for interactive PTY sessions.
func (h *WebSocketProxyHandler) PTYHandler(w http.ResponseWriter, r *http.Request) {
	sandboxName := chi.URLParam(r, "sandbox_name")
	if sandboxName == "" {
		http.Error(w, "sandbox_name is required", http.StatusBadRequest)
		return
	}

	podIP, err := h.getSandboxPodIP(r.Context(), sandboxName)
	if err != nil {
		h.logger.Error("failed to get sandbox pod IP", "error", err, "sandbox", sandboxName)
		http.Error(w, "sandbox not found", http.StatusNotFound)
		return
	}
	if podIP == "" {
		http.Error(w, "sandbox not ready", http.StatusServiceUnavailable)
		return
	}

	// Build sidecar WebSocket URL with query params
	query := url.Values{}
	if cmd := r.URL.Query().Get("cmd"); cmd != "" {
		query.Set("cmd", cmd)
	}
	if cols := r.URL.Query().Get("cols"); cols != "" {
		query.Set("cols", cols)
	}
	if rows := r.URL.Query().Get("rows"); rows != "" {
		query.Set("rows", rows)
	}
	if container := r.URL.Query().Get("container"); container != "" {
		query.Set("container", container)
	}

	sidecarURL := sidecar.GetWebSocketURL(podIP, "/pty", query)

	h.proxyWebSocket(w, r, sidecarURL)
}

// proxyWebSocket establishes a bidirectional WebSocket proxy.
func (h *WebSocketProxyHandler) proxyWebSocket(w http.ResponseWriter, r *http.Request, targetURL string) {
	// Upgrade client connection
	clientConn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("failed to upgrade client websocket", "error", err)
		return
	}
	defer func() { _ = clientConn.Close() }()

	// Connect to sidecar
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	sidecarConn, _, err := dialer.Dial(targetURL, nil)
	if err != nil {
		h.logger.Error("failed to connect to sidecar websocket", "error", err, "url", targetURL)
		_ = clientConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"error","data":"failed to connect to sandbox"}`))
		return
	}
	defer func() { _ = sidecarConn.Close() }()

	// Bidirectional proxy
	errChan := make(chan error, 2)

	// Client -> Sidecar
	go func() {
		errChan <- h.copyWebSocket(sidecarConn, clientConn)
	}()

	// Sidecar -> Client
	go func() {
		errChan <- h.copyWebSocket(clientConn, sidecarConn)
	}()

	// Wait for either direction to close
	<-errChan

	// Close both connections to stop the other goroutine
	_ = clientConn.Close()
	_ = sidecarConn.Close()
}

// copyWebSocket copies messages from src to dst.
func (h *WebSocketProxyHandler) copyWebSocket(dst, src *websocket.Conn) error {
	for {
		msgType, data, err := src.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				return nil
			}
			if err == io.EOF {
				return nil
			}
			return err
		}

		if err := dst.WriteMessage(msgType, data); err != nil {
			return err
		}
	}
}
