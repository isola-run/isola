package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

const (
	defaultPTYCols = 80
	defaultPTYRows = 24
	maxPTYCols     = 500
	maxPTYRows     = 200
	defaultShell   = "/bin/sh"
	ptyTimeout     = 24 * time.Hour // Long timeout for interactive sessions
)

// PTYMessage represents a message sent over the PTY WebSocket.
type PTYMessage struct {
	Type string `json:"type"`           // "input", "output", "resize", "exit", "error"
	Data string `json:"data,omitempty"` // Input/output data or error message
	Cols int    `json:"cols,omitempty"` // For resize
	Rows int    `json:"rows,omitempty"` // For resize
}

// PTYHandler handles PTY WebSocket connections.
type PTYHandler struct {
	logger *slog.Logger
	procFS proc.ProcFS
	fsh    *FilesystemHandlers
}

// NewPTYHandler creates a new PTYHandler.
func NewPTYHandler(logger *slog.Logger, procFS proc.ProcFS, fsh *FilesystemHandlers) *PTYHandler {
	return &PTYHandler{
		logger: logger,
		procFS: procFS,
		fsh:    fsh,
	}
}

// ServeHTTP handles WebSocket upgrade and PTY session.
func (h *PTYHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	container := r.URL.Query().Get("container")
	cmd := r.URL.Query().Get("cmd")
	if cmd == "" {
		cmd = defaultShell
	}

	// Parse and validate terminal size with bounds checking
	cols := defaultPTYCols
	rows := defaultPTYRows
	if c := r.URL.Query().Get("cols"); c != "" {
		if v, err := strconv.Atoi(c); err == nil && v > 0 && v <= maxPTYCols {
			cols = v
		}
	}
	if rw := r.URL.Query().Get("rows"); rw != "" {
		if v, err := strconv.Atoi(rw); err == nil && v > 0 && v <= maxPTYRows {
			rows = v
		}
	}

	// Upgrade to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", "error", err)
		return
	}
	defer func() { _ = conn.Close() }()

	// Mutex for concurrent WebSocket writes
	var writeMu sync.Mutex

	// Find container PID
	pid, err := h.fsh.findCachedContainerPID(container)
	if err != nil {
		h.sendPTYError(conn, &writeMu, "container not found")
		return
	}

	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		h.sendPTYError(conn, &writeMu, "failed to get container uid/gid")
		return
	}

	// Get working directory
	cwd, err := h.procFS.GetCwd(pid)
	if err != nil {
		h.logger.Warn("failed to get container cwd, using /", "error", err)
		cwd = "/"
	}

	// Get container's environment
	containerEnv, err := proc.GetEnviron(pid)
	if err != nil {
		h.logger.Warn("failed to read container environment, using minimal env", "error", err)
		containerEnv = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	}

	// Add TERM environment variable for proper terminal handling
	containerEnv = append(containerEnv, "TERM=xterm-256color")

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), ptyTimeout)
	defer cancel()

	// Build command using nsenter for namespace isolation
	// SECURITY: cmd must be quoted to prevent command injection
	nsenterArgs := []string{
		"-t", fmt.Sprintf("%d", pid),
		"-m", "-u", "-i", "-n", "-p",
		"--",
		"sh", "-c",
		fmt.Sprintf("cd %s && exec %s", shellQuote(cwd), shellQuote(cmd)),
	}

	execCmd := exec.CommandContext(ctx, "nsenter", nsenterArgs...)
	execCmd.Env = containerEnv
	execCmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
		Setsid: true, // Create new session for PTY
	}

	// Start PTY
	ptmx, err := pty.StartWithSize(execCmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		h.sendPTYError(conn, &writeMu, fmt.Sprintf("failed to start pty: %v", err))
		return
	}
	defer func() { _ = ptmx.Close() }()

	// Handle WebSocket -> PTY (input and resize)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}

			var msg PTYMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "input":
				if _, err := ptmx.Write([]byte(msg.Data)); err != nil {
					h.logger.Debug("failed to write to pty", "error", err)
					return
				}
			case "resize":
				if msg.Cols > 0 && msg.Cols <= maxPTYCols && msg.Rows > 0 && msg.Rows <= maxPTYRows {
					_ = pty.Setsize(ptmx, &pty.Winsize{
						Cols: uint16(msg.Cols),
						Rows: uint16(msg.Rows),
					})
				}
			}
		}
	}()

	// Handle PTY -> WebSocket (output)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				h.sendPTYMessage(conn, &writeMu, PTYMessage{
					Type: "output",
					Data: string(buf[:n]),
				})
			}
			if err != nil {
				if err != io.EOF {
					h.logger.Debug("pty read error", "error", err)
				}
				return
			}
		}
	}()

	// Wait for command to complete
	err = execCmd.Wait()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	// Send exit message
	h.sendPTYMessage(conn, &writeMu, PTYMessage{
		Type: "exit",
		Data: fmt.Sprintf("%d", exitCode),
	})
}

func (h *PTYHandler) sendPTYMessage(conn *websocket.Conn, mu *sync.Mutex, msg PTYMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("failed to marshal pty message", "error", err)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		h.logger.Debug("failed to write pty websocket message", "error", err)
	}
}

func (h *PTYHandler) sendPTYError(conn *websocket.Conn, mu *sync.Mutex, message string) {
	h.sendPTYMessage(conn, mu, PTYMessage{
		Type: "error",
		Data: message,
	})
}
