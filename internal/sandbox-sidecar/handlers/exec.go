package handlers

import (
	"context"
	"encoding/json"
	"errors"
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

	h.logger.Info("exec session started", "container", input.Container, "shell", input.Shell, "raw", input.Raw, "cols", input.Cols, "rows", input.Rows)

	conn.SetReadLimit(wsReadLimit)

	ptmx, tty, err := pty.Open()
	if err != nil {
		h.logger.Error("pty open failed", "error", err)
		conn.Close(websocket.StatusInternalError, "failed to open pty") //nolint:errcheck
		return
	}
	defer func() { _ = tty.Close() }()

	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: input.Rows, Cols: input.Cols}); err != nil {
		h.logger.Error("pty setsize failed", "error", err)
		_ = ptmx.Close()
		conn.Close(websocket.StatusInternalError, "failed to set terminal size") //nolint:errcheck
		return
	}

	cmd := &exec.Cmd{
		Path:       input.Shell,
		Args:       []string{filepath.Base(input.Shell)},
		Dir:        "/",
		Env:        environ,
		Stdin:      tty,
		Stdout:     tty,
		Stderr:     tty,
		ExtraFiles: []*os.File{tty},
		SysProcAttr: &syscall.SysProcAttr{
			Chroot:     h.procFS.GetRoot(pid),
			Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}, //nolint:gosec // uid/gid from trusted /proc
			Setsid:     true,
			Setctty:    true,
			Ctty:       3, // fd 3 = ExtraFiles[0] = tty
		},
	}

	err = cmd.Start()
	if err != nil {
		h.logger.Error("shell start failed", "error", err, "shell", input.Shell)
		conn.Close(websocket.StatusInternalError, "failed to start shell") //nolint:errcheck
		return
	}
	defer func() { _ = ptmx.Close() }()

	h.logger.Info("shell process started", "pid", cmd.Process.Pid, "shell", input.Shell)

	connCtx := r.Context()

	if input.Raw {
		h.bridgeRaw(connCtx, conn, ptmx)
	} else {
		h.bridgeFramed(connCtx, conn, ptmx)
	}

	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	h.logger.Info("exec session ended", "exitCode", exitCode)

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

	g, _ := errgroup.WithContext(ctx)

	// PTY → WS
	g.Go(func() error {
		defer cancel()
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if writeErr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); writeErr != nil {
					h.logger.Error("failed writing pty output to websocket", "error", writeErr)
					return writeErr
				}
			}
			if err != nil {
				if !isExpectedPTYClose(err) {
					h.logger.Error("failed reading from pty", "error", err)
				}
				return err
			}
		}
	})

	// WS → PTY (accept any message type — clients may send text or binary)
	g.Go(func() error {
		defer cancel()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				if !isExpectedWSClose(err) {
					h.logger.Error("failed reading from websocket", "error", err)
				}
				return err
			}
			if _, err := ptmx.Write(data); err != nil {
				h.logger.Error("failed writing websocket input to pty", "error", err)
				return err
			}
		}
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
					h.logger.Error("failed writing pty output to websocket", "error", writeErr)
					return writeErr
				}
			}
			if err != nil {
				if !isExpectedPTYClose(err) {
					h.logger.Error("failed reading from pty", "error", err)
				}
				return err
			}
		}
	})

	// WS → PTY/resize (stdin)
	g.Go(func() error {
		defer cancel()
		for {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				if !isExpectedWSClose(err) {
					h.logger.Error("failed reading from websocket", "error", err)
				}
				return err
			}
			if len(msg) == 0 {
				continue
			}
			switch msg[0] {
			case channelStdin:
				if _, err := ptmx.Write(msg[1:]); err != nil {
					h.logger.Error("failed writing websocket input to pty", "error", err)
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

// isExpectedPTYClose returns true for errors that are normal when the shell exits.
func isExpectedPTYClose(err error) bool {
	return errors.Is(err, syscall.EIO) || errors.Is(err, os.ErrClosed)
}

// isExpectedWSClose returns true for normal client disconnections.
func isExpectedWSClose(err error) bool {
	status := websocket.CloseStatus(err)
	return status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway
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
