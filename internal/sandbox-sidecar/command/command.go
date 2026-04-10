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

package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/isola-run/isola/internal/constants"
	"github.com/isola-run/isola/internal/httputil"
	sandboxsidecar "github.com/isola-run/isola/internal/sandbox-sidecar"
	"github.com/isola-run/isola/internal/sandbox-sidecar/proc"
	sidecarapi "github.com/isola-run/isola/internal/sidecar-api"
	"github.com/isola-run/isola/internal/sseutil"
)

// time cmd.Wait() has to drain before giving up
// in our case, there should be nothing to drain
// but it's a safety precaution for infinitely blocking after process kill
const waitDelayGracePeriod = 5 * time.Second

// sseKeepaliveInterval controls how often keepalive comments are sent on SSE streams
// to prevent proxies from dropping idle connections. 15s gives margin below typical
// proxy idle timeouts (30-60s) while avoiding excessive overhead.
const sseKeepaliveInterval = 15 * time.Second

// CommandBuilder abstracts command construction for testability.
// The real implementation uses chroot via /proc/<pid>/root; tests use direct execution.
type CommandBuilder interface {
	Build(ctx context.Context, pid int, req sidecarapi.CreateCommandRequest, env []string, stdoutFile, stderrFile *os.File) (*exec.Cmd, error)
}

// ChrootCommandBuilder runs commands in the target container's filesystem view.
// Using chroot instead of the possible setns (with nsenter) for simplicity and compatibility with Go's concurrency model.
//
// setns changes the executing thread's namespace, and thus requires strategies like runtime.LockOSThread()
// to avoid affecting other goroutines. See also: github.com/containernetworking/plugins/blob/main/pkg/ns/README.md#namespace-switching
//
// With chroot-only the process stays in the sidecar's mount namespace — /proc/self/mounts and df(1)
// show the sidecar's mounts. For running user commands this should make no practical difference.
type ChrootCommandBuilder struct{}

func (b *ChrootCommandBuilder) Build(ctx context.Context, pid int, req sidecarapi.CreateCommandRequest, env []string, stdoutFile, stderrFile *os.File) (*exec.Cmd, error) {
	dir := req.Cwd
	if dir == "" { // default to the target container's cwd
		cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
		if err != nil {
			return nil, fmt.Errorf("read cwd for pid %d: %w", pid, err)
		}
		dir = cwd
	}

	// /bin/sh with exec "$@": the shell does PATH lookup using the container's PATH env,
	// handles both absolute (/usr/bin/python3) and bare (python3) command names, and
	// exec(1) replaces the shell (same PID) so SIGKILL reaches the user's process directly.
	// exec.CommandContext("/bin/sh", ...) does not call LookPath because the path contains
	// a slash (Go os/exec/exec.go:440). no parent-side stat is performed (PATH will be resolved in the destination container after chroot).
	cmd := exec.CommandContext(ctx, "/bin/sh", //nolint:gosec // args are constructed from validated request
		append([]string{"-c", `exec "$@"`, "--"}, req.Args...)...)
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = env
	cmd.Dir = dir
	cmd.WaitDelay = waitDelayGracePeriod
	// Chroot runs before chdir and execve in the child (Go syscall/exec_linux.go).
	// "/bin/sh" and Dir resolve inside the container's root after chroot is applied.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Chroot:     fmt.Sprintf("/proc/%d/root", pid),
		Credential: &syscall.Credential{Uid: 0, Gid: 0},
	}
	return cmd, nil
}

// --- Input/Output types ---

type CreateCommandInput struct {
	Container string `query:"container,omitempty" doc:"Container name. Defaults to the only container if there is one, otherwise it's required."`
	Body      sidecarapi.CreateCommandRequest
}

type CreateCommandOutput struct {
	Body sidecarapi.CreateCommandResponse
}

type GetCommandStatusInput struct {
	ID string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	// Higher than the api-gateway's max (25s) so the gateway always terminates first
	// also aligns with the safe (assuming possible proxies etc) long polling value according to https://datatracker.ietf.org/doc/html/rfc6202
	// and of course it must be lower than the server's WriteTimeout.
	WaitSeconds int `query:"waitSeconds,omitempty" minimum:"0" maximum:"30" doc:"Max seconds to wait for the command to exit. 0 or absent returns immediately."`
}

type GetCommandStatusOutput struct {
	Body sidecarapi.CommandStatusResponse
}

type GetCommandStreamInput struct {
	ID          string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	LastEventID string `header:"Last-Event-ID" doc:"Byte offset to resume from (SSE Last-Event-ID)"`
}

type PostCommandStdinInput struct {
	ID string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
	sandboxsidecar.BodyStream
}

type CloseCommandStdinInput struct {
	ID string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
}

type DeleteCommandInput struct {
	ID string `path:"id" pattern:"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$" doc:"Command identifier"`
}

// --- Handlers ---

type commandEntry struct {
	cmdID       string
	cmd         *exec.Cmd
	cancel      context.CancelFunc
	stdinPipe   io.WriteCloser
	stdinMu     sync.Mutex // serialize concurrent stdin writes
	stdinClosed bool
	exitCode    int // only valid after done is closed
	done        chan struct{}
	outputDir   string
}

type Handlers struct {
	logger      *slog.Logger
	procFS      proc.ProcFS
	pidResolver *sandboxsidecar.PIDResolver
	cmdBuilder  CommandBuilder

	cmdMu    sync.RWMutex
	commands map[string]*commandEntry
}

func New(logger *slog.Logger, procFS proc.ProcFS, pidResolver *sandboxsidecar.PIDResolver, cmdBuilder CommandBuilder) *Handlers {
	return &Handlers{
		logger:      logger,
		procFS:      procFS,
		pidResolver: pidResolver,
		cmdBuilder:  cmdBuilder,
		commands:    make(map[string]*commandEntry),
	}
}

func (h *Handlers) PostCommand(_ context.Context, input *CreateCommandInput) (*CreateCommandOutput, error) {
	pid, err := h.pidResolver.FindCachedContainerPID(input.Container)
	if err != nil {
		h.logger.Warn("failed to determine container pid", "error", err, "container", input.Container)
		return nil, huma.Error400BadRequest("failed to determine container pid")
	}

	entry, err := h.startCommand(pid, input)
	if err != nil {
		return nil, err
	}

	h.cmdMu.Lock()
	// since cmdID is a random UUID, there's no collision risk and hence
	// existence check is skipped for simplicity
	h.commands[entry.cmdID] = entry
	h.cmdMu.Unlock()

	go h.waitForExit(entry)

	return &CreateCommandOutput{Body: sidecarapi.CreateCommandResponse{ID: entry.cmdID}}, nil
}

// startCommand sets up output files, builds and starts the command.
// On error, all resources are cleaned up via defer. On success, ownership
// of file handles transfers to the returned entry (closed by waitForExit).
func (h *Handlers) startCommand(pid int, input *CreateCommandInput) (*commandEntry, error) {
	if cwd := input.Body.Cwd; cwd != "" {
		// Clean as absolute to collapse ".." at the root (matching chroot resolution),
		// then join under the container root so the stat can't escape it.
		hostPath := filepath.Join(h.procFS.GetRoot(pid), filepath.Clean("/"+cwd))
		info, err := os.Stat(hostPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, huma.Error400BadRequest(fmt.Sprintf("working directory does not exist: %s", cwd))
			}
			h.logger.Error("failed to stat working directory", "error", err, "cwd", cwd)
			return nil, huma.Error500InternalServerError("failed to validate working directory")
		}
		if !info.IsDir() {
			return nil, huma.Error400BadRequest(fmt.Sprintf("working directory is not a directory: %s", cwd))
		}
	}

	cmdID := uuid.New().String()

	// Create output directory on the target container rootfs, so the logs count against its ephemeral storage calculation.
	outputDir := filepath.Join(h.procFS.GetRoot(pid), "var", "run", "isola", "commands", cmdID)
	if err := os.MkdirAll(outputDir, 0755); err != nil { //nolint:gosec
		h.logger.Error("failed to create command output directory", "error", err, "path", outputDir)
		return nil, huma.Error500InternalServerError("failed to create command output directory")
	}

	stdoutFile, err := os.Create(filepath.Join(outputDir, "stdout")) //nolint:gosec
	if err != nil {
		h.logger.Error("failed to create stdout file", "error", err)
		return nil, huma.Error500InternalServerError("failed to create stdout file")
	}
	defer func() { _ = stdoutFile.Close() }()

	stderrFile, err := os.Create(filepath.Join(outputDir, "stderr")) //nolint:gosec
	if err != nil {
		h.logger.Error("failed to create stderr file", "error", err)
		return nil, huma.Error500InternalServerError("failed to create stderr file")
	}
	defer func() { _ = stderrFile.Close() }()

	containerEnv, envErr := h.procFS.GetEnviron(pid)
	if envErr != nil {
		h.logger.Warn("failed to read container environment", "error", envErr, "pid", pid)
		containerEnv = nil
	}
	cmdEnv := buildCmdEnv(containerEnv, input.Body.Env)

	var ctx context.Context
	var cancel context.CancelFunc
	if input.Body.TimeoutSeconds != nil && *input.Body.TimeoutSeconds > 0 {
		duration := time.Duration(*input.Body.TimeoutSeconds) * time.Second
		// will send SIGKILL to the process after timeout
		ctx, cancel = context.WithTimeout(context.Background(), duration)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}

	succeeded := false
	defer func() {
		if !succeeded {
			cancel()
			_ = os.RemoveAll(outputDir)
		}
	}()

	cmd, err := h.cmdBuilder.Build(ctx, pid, input.Body, cmdEnv, stdoutFile, stderrFile)
	if err != nil {
		h.logger.Error("failed to build command", "error", err)
		return nil, huma.Error500InternalServerError("failed to build command")
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		h.logger.Error("failed to create stdin pipe", "error", err)
		return nil, huma.Error500InternalServerError("failed to create stdin pipe")
	}

	if err := cmd.Start(); err != nil {
		h.logger.Error("failed to start command", "error", err, "args", input.Body.Args)
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to start command: %s", err.Error()))
	}

	succeeded = true
	return &commandEntry{
		cmdID:     cmdID,
		cmd:       cmd,
		cancel:    cancel,
		stdinPipe: stdinPipe,
		done:      make(chan struct{}),
		outputDir: outputDir,
	}, nil
}

// wait for the process to exit, populate the exitCode and clean up
// the entries context and done channel
func (h *Handlers) waitForExit(entry *commandEntry) {
	defer close(entry.done)
	defer entry.cancel()

	var exitCode int
	if err := entry.cmd.Wait(); err != nil {
		if errors.Is(err, exec.ErrWaitDelay) {
			// ErrWaitDelay is returned by [Cmd.Wait] if the process exits with a
			// successful status code but its output pipes are not closed before the
			// command's WaitDelay expires.
			exitCode = 0
			h.logger.Warn("command wait expired", "error", err, "cmdID", entry.cmdID)
		} else {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
				h.logger.Error("command wait failed", "error", err, "cmdID", entry.cmdID)
			}
		}
	} else {
		exitCode = 0
	}

	entry.exitCode = exitCode
}

func (h *Handlers) getCommandEntry(cmdID string) (*commandEntry, error) {
	h.cmdMu.RLock()
	entry, ok := h.commands[cmdID]
	h.cmdMu.RUnlock()

	if !ok {
		return nil, huma.Error404NotFound(fmt.Sprintf("command %q not found", cmdID))
	}
	return entry, nil
}

func (h *Handlers) GetCommandStatus(ctx context.Context, input *GetCommandStatusInput) (*GetCommandStatusOutput, error) {
	entry, err := h.getCommandEntry(input.ID)
	if err != nil {
		return nil, err
	}

	if input.WaitSeconds > 0 {
		timer := time.NewTimer(time.Duration(input.WaitSeconds) * time.Second)
		defer timer.Stop()
		select {
		case <-entry.done:
		case <-timer.C:
			return &GetCommandStatusOutput{Body: sidecarapi.CommandStatusResponse{ExitCode: nil}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else {
		select {
		case <-entry.done:
		default:
			return &GetCommandStatusOutput{Body: sidecarapi.CommandStatusResponse{ExitCode: nil}}, nil
		}
	}

	exitCode := entry.exitCode
	return &GetCommandStatusOutput{Body: sidecarapi.CommandStatusResponse{ExitCode: &exitCode}}, nil
}

func (h *Handlers) GetCommandStdout(_ context.Context, input *GetCommandStreamInput) (*huma.StreamResponse, error) {
	offset, err := parseLastEventID(input.LastEventID)
	if err != nil {
		return nil, err
	}
	return h.streamOutput(input.ID, offset, "stdout")
}

func (h *Handlers) GetCommandStderr(_ context.Context, input *GetCommandStreamInput) (*huma.StreamResponse, error) {
	offset, err := parseLastEventID(input.LastEventID)
	if err != nil {
		return nil, err
	}
	return h.streamOutput(input.ID, offset, "stderr")
}

func parseLastEventID(id string) (int64, error) {
	if id == "" {
		return 0, nil
	}
	offset, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest(fmt.Sprintf("invalid Last-Event-ID header %q: %v", id, err))
	}
	if offset < 0 {
		return 0, huma.Error400BadRequest(fmt.Sprintf("invalid Last-Event-ID header %q: must be non-negative", id))
	}
	return offset, nil
}

func (h *Handlers) streamOutput(cmdID string, offset int64, streamName string) (*huma.StreamResponse, error) {
	entry, err := h.getCommandEntry(cmdID)
	if err != nil {
		return nil, err
	}

	filePath := filepath.Join(entry.outputDir, streamName)

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "text/event-stream")
			// no-cache, since the stream changes over time
			// not ", private" in the context of the sandbox->api-gateway
			ctx.SetHeader("Cache-Control", "no-cache")
			// X-Accel-Buffering: no, disable nginx buffering (serve immediately)
			ctx.SetHeader("X-Accel-Buffering", "no")

			f, err := os.Open(filePath) //nolint:gosec
			if err != nil {
				h.logger.Error("failed to open output file for streaming", "error", err, "path", filePath)
				return
			}
			defer func() { _ = f.Close() }()

			if offset > 0 {
				if _, err := f.Seek(offset, io.SeekStart); err != nil {
					h.logger.Error("failed to seek output file", "error", err, "offset", offset)
					return
				}
			}

			rc := httputil.ResponseController(ctx)
			dw := httputil.NewDeadlineWriter(ctx.BodyWriter(), rc, httputil.StreamTimeout)
			fw := httputil.NewTimedFlushWriter(dw, 100*time.Millisecond)
			defer fw.Stop()

			sse := sseutil.NewWriterAtOffset(fw, offset)
			buf := make([]byte, 32*1024)
			keepaliveTicker := time.NewTicker(sseKeepaliveInterval)
			defer keepaliveTicker.Stop()

			processDone := false
			for {
				n, readErr := f.Read(buf)
				if n > 0 {
					if werr := sse.WriteData(buf[:n]); werr != nil {
						if isClientDisconnect(werr) {
							h.logger.Warn("client disconnected during command stream", "error", werr, "cmdID", cmdID)
						} else {
							h.logger.Error("failed to write SSE data", "error", werr, "cmdID", cmdID)
						}
						return
					}
					keepaliveTicker.Reset(sseKeepaliveInterval)
					continue // tight loop while data is flowing
				}

				if readErr != nil && readErr != io.EOF {
					h.logger.Error("failed to read command output", "error", readErr, "cmdID", cmdID)
					return
				}

				// EOF - no more data to read at this timee
				if processDone {
					if err := sse.Finish(); err != nil {
						if isClientDisconnect(err) {
							h.logger.Warn("client disconnected during command stream", "error", err, "cmdID", cmdID)
						} else {
							h.logger.Error("failed to finish SSE writer", "error", err, "cmdID", cmdID)
						}
					}
					return
				}

				select {
				case <-entry.done:
					processDone = true
				case <-ctx.Context().Done():
					h.logger.Warn("client disconnected during command stream", "cmdID", cmdID)
					return
				case <-keepaliveTicker.C:
					if werr := sse.WriteKeepalive(); werr != nil {
						if isClientDisconnect(werr) {
							h.logger.Warn("client disconnected during command stream", "error", werr, "cmdID", cmdID)
						} else {
							h.logger.Error("failed to write SSE keepalive", "error", werr, "cmdID", cmdID)
						}
						return
					}
				default:
					time.Sleep(20 * time.Millisecond)
				}
			}
		},
	}, nil
}

func (h *Handlers) PostCommandStdin(_ context.Context, input *PostCommandStdinInput) (*struct{}, error) {
	entry, err := h.getCommandEntry(input.ID)
	if err != nil {
		return nil, err
	}

	select {
	case <-entry.done:
		return nil, huma.Error409Conflict("command has already exited")
	default:
	}

	stream := httputil.NewDeadlineReader(input.Stream, input.ResponseController, httputil.StreamTimeout)

	entry.stdinMu.Lock()
	if entry.stdinClosed {
		entry.stdinMu.Unlock()
		return nil, huma.Error409Conflict("stdin is already closed")
	}
	written, err := io.Copy(entry.stdinPipe, stream)
	entry.stdinMu.Unlock()

	if err != nil {
		// EPIPE: process closed its stdin (or exited) — read end of pipe is gone.
		// ErrClosed: pipe write end was closed (by cmd.Wait or CloseCommandStdin).
		if errors.Is(err, syscall.EPIPE) {
			return nil, huma.Error409Conflict("command has already exited")
		}
		if errors.Is(err, os.ErrClosed) {
			return nil, huma.Error409Conflict("stdin has been closed")
		}
		h.logger.Error("failed to write to stdin", "error", err, "cmdID", input.ID)
		return nil, huma.Error500InternalServerError("failed to write to stdin")
	}

	h.logger.Debug("stdin write", "cmdID", input.ID, "bytes", written)
	return nil, nil
}

func (h *Handlers) CloseCommandStdin(_ context.Context, input *CloseCommandStdinInput) (*struct{}, error) {
	entry, err := h.getCommandEntry(input.ID)
	if err != nil {
		return nil, err
	}

	select {
	case <-entry.done:
		return nil, huma.Error409Conflict("command has already exited")
	default:
	}

	entry.stdinMu.Lock()
	defer entry.stdinMu.Unlock()

	if entry.stdinClosed {
		return nil, huma.Error409Conflict("stdin is already closed")
	}

	if err := entry.stdinPipe.Close(); err != nil {
		// Only realistic failure for a pipe fd: cmd.Wait() already closed it via
		// closeDescriptors (race between process exit and explicit stdin close).
		h.logger.Warn("failed to close stdin", "error", err, "cmdID", input.ID)
	}
	entry.stdinClosed = true

	h.logger.Debug("stdin closed", "cmdID", input.ID)
	return nil, nil
}

// todo benl: delete from commands map (eventually?) to constraint memory?
func (h *Handlers) DeleteCommand(_ context.Context, input *DeleteCommandInput) (*struct{}, error) {
	entry, err := h.getCommandEntry(input.ID)
	if err != nil {
		return nil, err
	}

	entry.cancel()

	return nil, nil
}

func isClientDisconnect(err error) bool {
	return errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, context.Canceled)
}

func buildCmdEnv(containerEnv []string, overrides map[string]string) []string {
	envMap := make(map[string]string, len(containerEnv)+len(overrides))
	for _, kv := range containerEnv {
		if k, v, ok := strings.Cut(kv, "="); ok {
			envMap[k] = v
		}
	}
	for k, v := range overrides {
		envMap[k] = v
	}
	// delete IsolaContainerNameEnv marker to avoid detecting child process as the container process
	// since child process may freely change the configured cwd etc that were configured in the OCI image
	// or during pod creation, which may be surprising
	delete(envMap, constants.IsolaContainerNameEnv)

	result := make([]string, 0, len(envMap))
	for k, v := range envMap {
		result = append(result, k+"="+v)
	}
	return result
}

func Register(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "createCommand",
		Method:        http.MethodPost,
		Path:          "/commands",
		Summary:       "Start a command in the sandbox",
		Description:   "Starts a new command in the sandbox container and returns a command ID for tracking. Commands always run as root (UID 0, GID 0).",
		Tags:          []string{"commands"},
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusBadRequest},
	}, h.PostCommand)

	huma.Register(api, huma.Operation{
		OperationID: "getCommandStatus",
		Method:      http.MethodGet,
		Path:        "/commands/{id}/status",
		Summary:     "Get command status",
		Description: "Returns the exit code of the command, or null if still running. Supports long-polling via ?waitSeconds=N to block until the command exits or the wait expires.",
		Tags:        []string{"commands"},
		Errors:      []int{http.StatusNotFound},
	}, h.GetCommandStatus)

	huma.Register(api, huma.Operation{
		OperationID: "getCommandStdout",
		Method:      http.MethodGet,
		Path:        "/commands/{id}/stdout",
		Summary:     "Stream command stdout",
		Description: "Streams the command's stdout as Server-Sent Events. The connection remains open until the command exits. Supports resuming via Last-Event-ID header.",
		Tags:        []string{"commands"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Command stdout stream",
				Content: map[string]*huma.MediaType{
					"text/event-stream": {
						Schema: &huma.Schema{Type: "string"},
					},
				},
			},
		},
		Errors: []int{http.StatusNotFound},
	}, h.GetCommandStdout)

	huma.Register(api, huma.Operation{
		OperationID: "getCommandStderr",
		Method:      http.MethodGet,
		Path:        "/commands/{id}/stderr",
		Summary:     "Stream command stderr",
		Description: "Streams the command's stderr as Server-Sent Events. The connection remains open until the command exits. Supports resuming via Last-Event-ID header.",
		Tags:        []string{"commands"},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Command stderr stream",
				Content: map[string]*huma.MediaType{
					"text/event-stream": {
						Schema: &huma.Schema{Type: "string"},
					},
				},
			},
		},
		Errors: []int{http.StatusNotFound},
	}, h.GetCommandStderr)

	huma.Register(api, huma.Operation{
		OperationID: "postCommandStdin",
		Method:      http.MethodPost,
		Path:        "/commands/{id}/stdin",
		Summary:     "Write to command stdin",
		Description: "Writes raw bytes to the command's stdin",
		Tags:        []string{"commands"},
		RequestBody: &huma.RequestBody{
			Required: true,
			Content: map[string]*huma.MediaType{
				"application/octet-stream": {
					Schema: &huma.Schema{Type: "string", Format: "binary"},
				},
			},
		},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound, http.StatusConflict},
	}, h.PostCommandStdin)

	huma.Register(api, huma.Operation{
		OperationID:   "closeCommandStdin",
		Method:        http.MethodPost,
		Path:          "/commands/{id}/stdin/close",
		Summary:       "Close command stdin",
		Description:   "Closes the command's stdin pipe",
		Tags:          []string{"commands"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound, http.StatusConflict},
	}, h.CloseCommandStdin)

	huma.Register(api, huma.Operation{
		OperationID:   "deleteCommand",
		Method:        http.MethodDelete,
		Path:          "/commands/{id}",
		Summary:       "Kill a command",
		Description:   "Kills the command process. Idempotent for already-exited commands.",
		Tags:          []string{"commands"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound},
	}, h.DeleteCommand)
}
