package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for internal sidecar API
	},
}

// StreamMessage represents a message sent over the WebSocket.
type StreamMessage struct {
	Type     string `json:"type"`               // "stdout", "stderr", "exit", "error", "stdin"
	Data     string `json:"data,omitempty"`     // Output data or error message
	ExitCode int    `json:"exit_code,omitempty"` // Exit code (for "exit" type)
}

// ExecStreamHandler handles WebSocket connections for streaming command execution.
type ExecStreamHandler struct {
	logger *slog.Logger
	procFS proc.ProcFS
	fsh    *FilesystemHandlers
}

// NewExecStreamHandler creates a new ExecStreamHandler.
func NewExecStreamHandler(logger *slog.Logger, procFS proc.ProcFS, fsh *FilesystemHandlers) *ExecStreamHandler {
	return &ExecStreamHandler{
		logger: logger,
		procFS: procFS,
		fsh:    fsh,
	}
}

// ServeHTTP handles WebSocket upgrade and command streaming.
func (h *ExecStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	container := r.URL.Query().Get("container")
	cmd := r.URL.Query().Get("cmd")
	argsStr := r.URL.Query().Get("args")
	cwd := r.URL.Query().Get("cwd")

	if cmd == "" {
		http.Error(w, "cmd query parameter is required", http.StatusBadRequest)
		return
	}

	// Parse args (comma-separated)
	var args []string
	if argsStr != "" {
		args = strings.Split(argsStr, ",")
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", "error", err)
		return
	}
	defer func() { _ = conn.Close() }()

	// Find container PID
	pid, err := h.fsh.findCachedContainerPID(container)
	if err != nil {
		h.sendError(conn, "container not found")
		return
	}

	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		h.sendError(conn, "failed to get container uid/gid")
		return
	}

	// Resolve working directory
	if cwd == "" {
		cwd, err = h.procFS.GetCwd(pid)
		if err != nil {
			h.logger.Warn("failed to get container cwd, using /", "error", err)
			cwd = "/"
		}
	}

	// Get container's environment
	containerEnv, err := proc.GetEnviron(pid)
	if err != nil {
		h.logger.Warn("failed to read container environment, using minimal env", "error", err)
		containerEnv = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	}

	// Create context with timeout (10 minutes for streaming)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	// Build command using nsenter
	nsenterArgs := []string{
		"-t", fmt.Sprintf("%d", pid),
		"-m", "-u", "-i", "-n", "-p",
		"--",
		"sh", "-c",
		fmt.Sprintf("cd %s && exec %s", shellQuote(cwd), buildCommand(cmd, args)),
	}

	execCmd := exec.CommandContext(ctx, "nsenter", nsenterArgs...)
	execCmd.Env = containerEnv
	execCmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}

	// Get pipes for stdout/stderr
	stdout, err := execCmd.StdoutPipe()
	if err != nil {
		h.sendError(conn, fmt.Sprintf("failed to create stdout pipe: %v", err))
		return
	}

	stderr, err := execCmd.StderrPipe()
	if err != nil {
		h.sendError(conn, fmt.Sprintf("failed to create stderr pipe: %v", err))
		return
	}

	stdin, err := execCmd.StdinPipe()
	if err != nil {
		h.sendError(conn, fmt.Sprintf("failed to create stdin pipe: %v", err))
		return
	}

	// Start the command
	if err := execCmd.Start(); err != nil {
		h.sendError(conn, fmt.Sprintf("failed to start command: %v", err))
		return
	}

	// Handle incoming WebSocket messages (for stdin)
	go func() {
		defer func() { _ = stdin.Close() }()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg StreamMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			if msg.Type == "stdin" {
				_, _ = stdin.Write([]byte(msg.Data))
			}
		}
	}()

	// Stream stdout
	go h.streamOutput(conn, stdout, "stdout")

	// Stream stderr
	go h.streamOutput(conn, stderr, "stderr")

	// Wait for command to complete
	err = execCmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// Send exit message
	h.sendMessage(conn, StreamMessage{
		Type:     "exit",
		ExitCode: exitCode,
	})
}

func (h *ExecStreamHandler) streamOutput(conn *websocket.Conn, reader io.Reader, outputType string) {
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			h.sendMessage(conn, StreamMessage{
				Type: outputType,
				Data: string(buf[:n]),
			})
		}
		if err != nil {
			return
		}
	}
}

func (h *ExecStreamHandler) sendMessage(conn *websocket.Conn, msg StreamMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("failed to marshal message", "error", err)
		return
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		h.logger.Error("failed to write websocket message", "error", err)
	}
}

func (h *ExecStreamHandler) sendError(conn *websocket.Conn, message string) {
	h.sendMessage(conn, StreamMessage{
		Type: "error",
		Data: message,
	})
}
