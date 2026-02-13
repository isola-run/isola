package handlers

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

	"github.com/isola-ai/isola-sb/internal/constants"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

// CommandBuilder abstracts command construction for testability.
// The real implementation uses nsenter; tests use direct execution.
type CommandBuilder interface {
	Build(pid int, req sidecarapi.CreateCommandRequest, env []string, stdoutFile, stderrFile *os.File) (*exec.Cmd, error)
}

// nsenterCommandBuilder constructs nsenter commands that enter the target container's namespaces.
type nsenterCommandBuilder struct{}

func (b *nsenterCommandBuilder) Build(pid int, req sidecarapi.CreateCommandRequest, env []string, stdoutFile, stderrFile *os.File) (*exec.Cmd, error) {
	args := []string{
		"--target", strconv.Itoa(pid),
		"--mount", "--pid", "--ipc", "--uts",
	}
	if req.Cwd != "" {
		args = append(args, "--wd="+req.Cwd)
	} else {
		// --wd with no argument sets CWD to the target process's working directory
		args = append(args, "--wd")
	}
	args = append(args, "--")
	args = append(args, req.Cmd)
	args = append(args, req.Args...)

	cmd := exec.CommandContext(context.Background(), "nsenter", args...) //nolint:gosec // args are constructed from validated request
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = env
	return cmd, nil
}

// directCommandBuilder runs commands directly without nsenter (for testing).
type directCommandBuilder struct{}

func (b *directCommandBuilder) Build(_ int, req sidecarapi.CreateCommandRequest, env []string, stdoutFile, stderrFile *os.File) (*exec.Cmd, error) {
	cmd := exec.CommandContext(context.Background(), req.Cmd, req.Args...) //nolint:gosec // test-only builder
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = env
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	return cmd, nil
}

type commandEntry struct {
	id         string
	cmd        *exec.Cmd
	stdinPipe  io.WriteCloser
	timer      *time.Timer
	mu         sync.Mutex
	exitCode   *int
	exited     bool
	done       chan struct{}
	stdoutPath string
	stderrPath string
}

type CommandHandlers struct {
	logger     *slog.Logger
	procFS     proc.ProcFS
	cmdBuilder CommandBuilder

	pidMu      sync.RWMutex
	cachedPIDs map[string]int

	cmdMu    sync.RWMutex
	commands map[string]*commandEntry
}

func NewCommandHandlers(logger *slog.Logger, procFS proc.ProcFS) *CommandHandlers {
	return &CommandHandlers{
		logger:     logger,
		procFS:     procFS,
		cmdBuilder: &nsenterCommandBuilder{},
		cachedPIDs: make(map[string]int),
		commands:   make(map[string]*commandEntry),
	}
}

// NewCommandHandlersForTest creates CommandHandlers with a direct command builder for testing.
func NewCommandHandlersForTest(logger *slog.Logger, procFS proc.ProcFS) *CommandHandlers {
	return &CommandHandlers{
		logger:     logger,
		procFS:     procFS,
		cmdBuilder: &directCommandBuilder{},
		cachedPIDs: make(map[string]int),
		commands:   make(map[string]*commandEntry),
	}
}

func (h *CommandHandlers) findCachedContainerPID(containerName string) (int, error) {
	h.pidMu.RLock()
	pid, ok := h.cachedPIDs[containerName]
	h.pidMu.RUnlock()

	if ok {
		if name, found := proc.GetContainerName(pid); found && (containerName == "" || name == containerName) {
			return pid, nil
		}
	}

	newPID, err := h.procFS.FindMarkedPID(containerName)
	if err != nil {
		return 0, err
	}

	h.pidMu.Lock()
	h.cachedPIDs[containerName] = newPID
	h.pidMu.Unlock()

	return newPID, nil
}

func (h *CommandHandlers) PostCommand(_ context.Context, input *CreateCommandInput) (*CreateCommandOutput, error) {
	pid, err := h.findCachedContainerPID(input.Container)
	if err != nil {
		h.logger.Warn("failed to determine container pid", "error", err, "container", input.Container)
		return nil, huma.Error400BadRequest("failed to determine container pid")
	}

	id := uuid.New().String()

	// Create output directory on the container rootfs
	outputDir := filepath.Join(h.procFS.GetRoot(pid), "var", "run", "isola", "commands", id)
	if err := os.MkdirAll(outputDir, 0755); err != nil { //nolint:gosec // intentional permissions for container access
		h.logger.Error("failed to create command output directory", "error", err, "path", outputDir)
		return nil, huma.Error500InternalServerError("failed to create command output directory")
	}

	stdoutPath := filepath.Join(outputDir, "stdout")
	stderrPath := filepath.Join(outputDir, "stderr")

	stdoutFile, err := os.Create(stdoutPath) //nolint:gosec
	if err != nil {
		h.logger.Error("failed to create stdout file", "error", err)
		return nil, huma.Error500InternalServerError("failed to create stdout file")
	}

	stderrFile, err := os.Create(stderrPath) //nolint:gosec
	if err != nil {
		_ = stdoutFile.Close()
		h.logger.Error("failed to create stderr file", "error", err)
		return nil, huma.Error500InternalServerError("failed to create stderr file")
	}

	// Build environment: container env + per-command overrides
	containerEnv, envErr := h.procFS.GetEnviron(pid)
	if envErr != nil {
		h.logger.Warn("failed to read container environment", "error", envErr, "pid", pid)
		containerEnv = nil
	}
	cmdEnv := buildEnv(containerEnv, input.Body.Env)

	cmd, err := h.cmdBuilder.Build(pid, input.Body, cmdEnv, stdoutFile, stderrFile)
	if err != nil {
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
		h.logger.Error("failed to build command", "error", err)
		return nil, huma.Error500InternalServerError("failed to build command")
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
		h.logger.Error("failed to create stdin pipe", "error", err)
		return nil, huma.Error500InternalServerError("failed to create stdin pipe")
	}

	if err := cmd.Start(); err != nil {
		_ = stdoutFile.Close()
		_ = stderrFile.Close()
		_ = stdinPipe.Close()
		h.logger.Error("failed to start command", "error", err, "cmd", input.Body.Cmd)
		return nil, huma.Error500InternalServerError(fmt.Sprintf("failed to start command: %s", err.Error()))
	}

	entry := &commandEntry{
		id:         id,
		cmd:        cmd,
		stdinPipe:  stdinPipe,
		done:       make(chan struct{}),
		stdoutPath: stdoutPath,
		stderrPath: stderrPath,
	}

	// Timeout enforcement
	if input.Body.Timeout != nil && *input.Body.Timeout > 0 {
		duration := time.Duration(*input.Body.Timeout) * time.Second
		entry.timer = time.AfterFunc(duration, func() {
			entry.mu.Lock()
			exited := entry.exited
			entry.mu.Unlock()
			if !exited {
				_ = cmd.Process.Kill()
			}
		})
	}

	h.cmdMu.Lock()
	h.commands[id] = entry
	h.cmdMu.Unlock()

	// Wait goroutine: owns writer file handles, closes them on exit
	go func() {
		defer close(entry.done)

		_ = cmd.Wait()

		entry.mu.Lock()
		entry.exited = true
		exitCode := cmd.ProcessState.ExitCode()
		entry.exitCode = &exitCode
		entry.mu.Unlock()

		_ = stdoutFile.Close()
		_ = stderrFile.Close()
		_ = entry.stdinPipe.Close()
		if entry.timer != nil {
			entry.timer.Stop()
		}
	}()

	return &CreateCommandOutput{Body: sidecarapi.CreateCommandResponse{CommandID: id}}, nil
}

func (h *CommandHandlers) GetCommandStatus(_ context.Context, input *GetCommandStatusInput) (*GetCommandStatusOutput, error) {
	h.cmdMu.RLock()
	entry, ok := h.commands[input.CmdID]
	h.cmdMu.RUnlock()

	if !ok {
		return nil, huma.Error404NotFound(fmt.Sprintf("command %q not found", input.CmdID))
	}

	entry.mu.Lock()
	exitCode := entry.exitCode
	entry.mu.Unlock()

	return &GetCommandStatusOutput{Body: sidecarapi.CommandStatusResponse{ExitCode: exitCode}}, nil
}

func (h *CommandHandlers) GetCommandStdout(_ context.Context, input *GetCommandStreamInput) (*huma.StreamResponse, error) {
	return h.streamOutput(input.CmdID, input.Offset, true)
}

func (h *CommandHandlers) GetCommandStderr(_ context.Context, input *GetCommandStreamInput) (*huma.StreamResponse, error) {
	return h.streamOutput(input.CmdID, input.Offset, false)
}

func (h *CommandHandlers) streamOutput(cmdID string, offset int64, isStdout bool) (*huma.StreamResponse, error) {
	h.cmdMu.RLock()
	entry, ok := h.commands[cmdID]
	h.cmdMu.RUnlock()

	if !ok {
		return nil, huma.Error404NotFound(fmt.Sprintf("command %q not found", cmdID))
	}

	filePath := entry.stderrPath
	if isStdout {
		filePath = entry.stdoutPath
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "application/octet-stream")
			ctx.SetHeader("Cache-Control", "no-cache")
			ctx.SetHeader("X-Accel-Buffering", "no")

			f, err := os.Open(filePath) //nolint:gosec // path from trusted internal state
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

			w := ctx.BodyWriter()
			flusher, canFlush := w.(http.Flusher)
			buf := make([]byte, 4096)
			pos := offset

			for {
				// Inner drain loop: read all available bytes before checking exit
				drained := false
				for !drained {
					n, readErr := f.Read(buf)
					if n > 0 {
						if _, writeErr := w.Write(buf[:n]); writeErr != nil {
							if isClientDisconnect(writeErr) {
								h.logger.Warn("client disconnected during command stream", "error", writeErr, "cmdId", cmdID)
							} else {
								h.logger.Error("unexpected error streaming command output", "error", writeErr, "cmdId", cmdID)
							}
							return
						}
						pos += int64(n)
						if canFlush {
							flusher.Flush()
						}
					}
					if readErr != nil {
						if readErr != io.EOF {
							h.logger.Error("failed to read command output file", "error", readErr, "cmdId", cmdID)
							return
						}
						drained = true
					}
				}

				// Check if process has exited
				select {
				case <-entry.done:
					// Final drain: process exited, read any remaining bytes
					for {
						n, readErr := f.Read(buf)
						if n > 0 {
							if _, writeErr := w.Write(buf[:n]); writeErr != nil {
								if isClientDisconnect(writeErr) {
									h.logger.Warn("client disconnected during command stream", "error", writeErr, "cmdId", cmdID)
								} else {
									h.logger.Error("unexpected error streaming command output", "error", writeErr, "cmdId", cmdID)
								}
								return
							}
							if canFlush {
								flusher.Flush()
							}
						}
						if readErr != nil {
							if readErr != io.EOF {
								h.logger.Error("failed to read command output file", "error", readErr, "cmdId", cmdID)
							}
							return
						}
					}
				default:
					time.Sleep(100 * time.Millisecond)
				}
			}
		},
	}, nil
}

func (h *CommandHandlers) PostCommandStdin(_ context.Context, input *PostCommandStdinInput) (*struct{}, error) {
	h.cmdMu.RLock()
	entry, ok := h.commands[input.CmdID]
	h.cmdMu.RUnlock()

	if !ok {
		return nil, huma.Error404NotFound(fmt.Sprintf("command %q not found", input.CmdID))
	}

	entry.mu.Lock()
	exited := entry.exited
	entry.mu.Unlock()

	if exited {
		return nil, huma.Error409Conflict("command has already exited")
	}

	if _, err := io.Copy(entry.stdinPipe, input.Stream); err != nil {
		// Process may have exited between the check and the write
		if strings.Contains(err.Error(), "broken pipe") || strings.Contains(err.Error(), "file already closed") {
			return nil, huma.Error409Conflict("command has already exited")
		}
		h.logger.Error("failed to write to stdin", "error", err, "cmdId", input.CmdID)
		return nil, huma.Error500InternalServerError("failed to write to stdin")
	}

	return nil, nil
}
// todo benl: delete from commands map (eventually?) to constraint memory?
func (h *CommandHandlers) DeleteCommand(_ context.Context, input *DeleteCommandInput) (*struct{}, error) {
	h.cmdMu.RLock()
	entry, ok := h.commands[input.CmdID]
	h.cmdMu.RUnlock()

	if !ok {
		return nil, huma.Error404NotFound(fmt.Sprintf("command %q not found", input.CmdID))
	}

	entry.mu.Lock()
	exited := entry.exited
	entry.mu.Unlock()

	if !exited {
		_ = entry.cmd.Process.Kill()
	}

	return nil, nil
}

func isClientDisconnect(err error) bool {
	return errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, context.Canceled)
}

// buildEnv merges container env with per-command overrides and strips
// ISOLA_CONTAINER_NAME so child processes aren't mistaken for container markers.
func buildEnv(containerEnv []string, overrides map[string]string) []string {
	envMap := make(map[string]string, len(containerEnv)+len(overrides))
	for _, kv := range containerEnv {
		if k, v, ok := strings.Cut(kv, "="); ok {
			envMap[k] = v
		}
	}
	for k, v := range overrides {
		envMap[k] = v
	}
	delete(envMap, constants.IsolaContainerNameEnv)

	result := make([]string, 0, len(envMap))
	for k, v := range envMap {
		result = append(result, k+"="+v)
	}
	return result
}
