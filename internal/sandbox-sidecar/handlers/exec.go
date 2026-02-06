package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"golang.org/x/sync/errgroup"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

// Wire protocol channel bytes for framed mode.
const (
	channelStdin  = 0x00
	channelStdout = 0x01
	channelResize = 0x03
	channelExit   = 0x04
)

const wsReadLimit = 32 * 1024

type ExecHandlers struct {
	logger   *slog.Logger
	procFS   proc.ProcFS
	pidCache *PIDCache
}

func NewExecHandlers(logger *slog.Logger, procFS proc.ProcFS, pidCache *PIDCache) *ExecHandlers {
	return &ExecHandlers{
		logger:   logger,
		procFS:   procFS,
		pidCache: pidCache,
	}
}

type ExecInput struct {
	Container string `query:"container" doc:"Container name (optional if single container)"`
	Shell     string `query:"shell" default:"/bin/sh" doc:"Shell binary path (absolute)"`
	Raw       bool   `query:"raw" default:"false" doc:"Raw mode (no channel framing)"`
	Cols      uint16 `query:"cols" default:"80" doc:"Terminal columns (1-1000)"`
	Rows      uint16 `query:"rows" default:"24" doc:"Terminal rows (1-1000)"`
}

func RegisterExecRoutes(api huma.API, h *ExecHandlers) {
	huma.Register(api, huma.Operation{
		OperationID: "ws-exec",
		Method:      http.MethodGet,
		Path:        "/ws/exec",
		Summary:     "Interactive shell via WebSocket",
		Tags:        []string{"exec"},
		Responses: map[string]*huma.Response{
			"101": {Description: "Switching Protocols — WebSocket connection established"},
		},
	}, h.HandleExec)
}

type resizeMsg struct {
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

func (h *ExecHandlers) HandleExec(ctx context.Context, input *ExecInput) (*huma.StreamResponse, error) {
	if !filepath.IsAbs(input.Shell) || strings.Contains(input.Shell, "..") || strings.ContainsRune(input.Shell, 0) {
		return nil, huma.Error400BadRequest("shell must be an absolute path without '..' or null bytes")
	}
	if input.Cols < 1 || input.Cols > 1000 || input.Rows < 1 || input.Rows > 1000 {
		return nil, huma.Error400BadRequest("cols/rows must be 1-1000")
	}

	pid, err := h.pidCache.FindPID(input.Container)
	if err != nil {
		return nil, huma.Error404NotFound("container not found")
	}

	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		return nil, huma.Error500InternalServerError("failed to get container uid/gid")
	}

	environ, err := h.procFS.ReadEnviron(pid)
	if err != nil {
		h.logger.Error("failed to read container environ", "error", err, "pid", pid)
		return nil, huma.Error500InternalServerError("failed to read container environ")
	}

	// Layer TERM on top of inherited env
	environ = appendOrReplaceTERM(environ)

	return &huma.StreamResponse{
		Body: func(humaCtx huma.Context) {
			h.handleExecWS(humaCtx, input, pid, uid, gid, environ)
		},
	}, nil
}

func (h *ExecHandlers) handleExecWS(humaCtx huma.Context, input *ExecInput, pid, uid, gid int, environ []string) {
	r, w := humachi.Unwrap(humaCtx)

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.logger.Error("websocket accept failed", "error", err)
		return
	}
	defer conn.CloseNow() //nolint:errcheck

	conn.SetReadLimit(wsReadLimit)

	cmd := &exec.Cmd{
		Path: input.Shell,
		Args: []string{filepath.Base(input.Shell)},
		Dir:  "/",
		Env:  environ,
		SysProcAttr: &syscall.SysProcAttr{
			Chroot:     h.procFS.GetRoot(pid),
			Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}, //nolint:gosec // uid/gid from trusted /proc
		},
	}

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: input.Rows, Cols: input.Cols})
	if err != nil {
		h.logger.Error("pty start failed", "error", err, "shell", input.Shell)
		conn.Close(websocket.StatusInternalError, "failed to start shell") //nolint:errcheck
		return
	}
	defer func() { _ = ptmx.Close() }()

	connCtx := r.Context()

	if input.Raw {
		h.bridgeRaw(connCtx, conn, ptmx)
	} else {
		h.bridgeFramed(connCtx, conn, ptmx)
	}

	// Wait for process exit and send exit code in framed mode
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	if !input.Raw {
		exitJSON, _ := json.Marshal(map[string]int{"code": exitCode})
		msg := make([]byte, 1+len(exitJSON))
		msg[0] = channelExit
		copy(msg[1:], exitJSON)
		conn.Write(connCtx, websocket.MessageBinary, msg) //nolint:errcheck
	}

	conn.Close(websocket.StatusNormalClosure, "") //nolint:errcheck
}

func (h *ExecHandlers) bridgeRaw(ctx context.Context, conn *websocket.Conn, ptmx *os.File) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	nc := websocket.NetConn(ctx, conn, websocket.MessageBinary)

	g, _ := errgroup.WithContext(ctx)
	g.Go(func() error {
		defer cancel()
		_, err := io.Copy(nc, ptmx)
		return err
	})
	g.Go(func() error {
		defer cancel()
		_, err := io.Copy(ptmx, nc)
		return err
	})
	_ = g.Wait()
}

func (h *ExecHandlers) bridgeFramed(ctx context.Context, conn *websocket.Conn, ptmx *os.File) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, _ := errgroup.WithContext(ctx)

	// PTY → WS (stdout)
	g.Go(func() error {
		defer cancel()
		buf := make([]byte, 4096+1)
		buf[0] = channelStdout
		for {
			n, err := ptmx.Read(buf[1:])
			if n > 0 {
				if writeErr := conn.Write(ctx, websocket.MessageBinary, buf[:n+1]); writeErr != nil {
					return writeErr
				}
			}
			if err != nil {
				return err // EIO on child exit is expected
			}
		}
	})

	// WS → PTY/resize (stdin)
	g.Go(func() error {
		defer cancel()
		for {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				return err
			}
			if len(msg) == 0 {
				continue
			}
			switch msg[0] {
			case channelStdin:
				if _, err := ptmx.Write(msg[1:]); err != nil {
					return err
				}
			case channelResize:
				h.handleResize(ptmx, msg[1:])
			}
		}
	})

	_ = g.Wait()
}

func (h *ExecHandlers) handleResize(ptmx *os.File, data []byte) {
	var msg resizeMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		h.logger.Warn("invalid resize message", "error", err)
		return
	}
	if msg.Cols < 1 || msg.Cols > 1000 || msg.Rows < 1 || msg.Rows > 1000 {
		return
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: msg.Rows, Cols: msg.Cols}); err != nil {
		h.logger.Warn("pty resize failed", "error", err)
	}
}

func appendOrReplaceTERM(environ []string) []string {
	const entry = "TERM=xterm-256color"
	const prefix = "TERM="
	for i, e := range environ {
		if strings.HasPrefix(e, prefix) {
			environ[i] = entry
			return environ
		}
	}
	return append(environ, entry)
}
