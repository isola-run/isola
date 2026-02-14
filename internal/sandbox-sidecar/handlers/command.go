package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"github.com/isola-ai/isola-sb/internal/httputil"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

// time cmd.Wait() has to drain before giving up
// in our case, there should be nothing to drain
// but it's a safety precaution for infinitely blocking after process kill
const waitDelayGracePeriod = 5 * time.Second

// CommandBuilder abstracts command construction for testability.
// The real implementation uses nsenter; tests use direct execution.
type CommandBuilder interface {
	Build(ctx context.Context, pid int, req sidecarapi.CreateCommandRequest, env []string, stdoutFile, stderrFile *os.File) (*exec.Cmd, error)
}

// NsenterCommandBuilder constructs nsenter commands that enter the target container's namespaces.
type NsenterCommandBuilder struct{}

func (b *NsenterCommandBuilder) Build(ctx context.Context, pid int, req sidecarapi.CreateCommandRequest, env []string, stdoutFile, stderrFile *os.File) (*exec.Cmd, error) {
	args := []string{
		// https://man7.org/linux/man-pages/man1/nsenter.1.html
		"--target", strconv.Itoa(pid), // target process to get namespaces from
		"--all",     // enter all usable namespaces (see nsenter.c is_usable_namespace())
		"--no-fork", // prevent nsenter's implicit fork when entering PID namespace (execvp directly)
		"--root",    // chroot to /proc/<pid>/root
		// Execute as root:
		"--setuid=0",
		"--setgid=0",
		// --no-fork is critical: without it, nsenter forks when entering a PID namespace,
		// creating an intermediate parent in a waitpid loop. SIGKILL would kill that parent
		// and orphan the actual command. With --no-fork, nsenter calls execvp() directly,
		// so SIGKILL reaches the user's process.
		//
		// --no-fork means the exec'd process itself doesn't join the target PID namespace (only its children would).
		// This is safe because the sandbox pod has shareProcessNamespace: true,
		// so caller and target already share the same PID namespace.
		//
		// --all (vs explicit flags) gracefully handles namespaces that can't be entered
		// (e.g. the caller's own user namespace, which setns(2) forbids reentering).
		// No --env: we build the env ourselves and must not inherit the sidecar's.
	}
	if req.Cwd != "" {
		args = append(args, "--wdns="+req.Cwd)
	} else {
		args = append(args, "--wd") // effectively /proc/<pid>/cwd
	}
	args = append(args, "--")
	args = append(args, req.Cmd)
	args = append(args, req.Args...)

	cmd := exec.CommandContext(ctx, "nsenter", args...) //nolint:gosec // args are constructed from validated request
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = env
	cmd.WaitDelay = waitDelayGracePeriod
	return cmd, nil
}

type commandEntry struct {
	cmdID     string
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	stdinPipe io.WriteCloser
	stdinMu   sync.Mutex // serialize concurrent stdin writes
	exitCode  int        // only valid after done is closed
	done      chan struct{}
	outputDir string
}

type CommandHandlers struct {
	logger      *slog.Logger
	procFS      proc.ProcFS
	pidResolver *PIDResolver
	cmdBuilder  CommandBuilder

	cmdMu    sync.RWMutex
	commands map[string]*commandEntry
}

func NewCommandHandlers(logger *slog.Logger, procFS proc.ProcFS, pidResolver *PIDResolver, cmdBuilder CommandBuilder) *CommandHandlers {
	return &CommandHandlers{
		logger:      logger,
		procFS:      procFS,
		pidResolver: pidResolver,
		cmdBuilder:  cmdBuilder,
		commands:    make(map[string]*commandEntry),
	}
}

func (h *CommandHandlers) PostCommand(_ context.Context, input *CreateCommandInput) (*CreateCommandOutput, error) {
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

	return &CreateCommandOutput{Body: sidecarapi.CreateCommandResponse{CommandID: entry.cmdID}}, nil
}

// startCommand sets up output files, builds and starts the command.
// On error, all resources are cleaned up via defer. On success, ownership
// of file handles transfers to the returned entry (closed by waitForExit).
func (h *CommandHandlers) startCommand(pid int, input *CreateCommandInput) (*commandEntry, error) {
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
	if input.Body.Timeout != nil && *input.Body.Timeout > 0 {
		duration := time.Duration(*input.Body.Timeout) * time.Second
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
		h.logger.Error("failed to start command", "error", err, "cmd", input.Body.Cmd)
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
func (h *CommandHandlers) waitForExit(entry *commandEntry) {
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

func (h *CommandHandlers) getCommandEntry(cmdID string) (*commandEntry, error) {
	h.cmdMu.RLock()
	entry, ok := h.commands[cmdID]
	h.cmdMu.RUnlock()

	if !ok {
		return nil, huma.Error404NotFound(fmt.Sprintf("command %q not found", cmdID))
	}
	return entry, nil
}

func (h *CommandHandlers) GetCommandStatus(_ context.Context, input *GetCommandStatusInput) (*GetCommandStatusOutput, error) {
	entry, err := h.getCommandEntry(input.CmdID)
	if err != nil {
		return nil, err
	}

	select {
	case <-entry.done:
		exitCode := entry.exitCode
		return &GetCommandStatusOutput{Body: sidecarapi.CommandStatusResponse{ExitCode: &exitCode}}, nil
	default: // return immediately if cmd not done, indicating "still running"
		return &GetCommandStatusOutput{Body: sidecarapi.CommandStatusResponse{ExitCode: nil}}, nil
	}
}

func (h *CommandHandlers) GetCommandStdout(_ context.Context, input *GetCommandStreamInput) (*huma.StreamResponse, error) {
	return h.streamOutput(input.CmdID, input.Offset, "stdout")
}

func (h *CommandHandlers) GetCommandStderr(_ context.Context, input *GetCommandStreamInput) (*huma.StreamResponse, error) {
	return h.streamOutput(input.CmdID, input.Offset, "stderr")
}

func (h *CommandHandlers) streamOutput(cmdID string, offset int64, streamName string) (*huma.StreamResponse, error) {
	entry, err := h.getCommandEntry(cmdID)
	if err != nil {
		return nil, err
	}

	filePath := filepath.Join(entry.outputDir, streamName)

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "application/octet-stream")
			// no-cache, since the stream change over time
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

			fw := httputil.NewTimedFlushWriter(ctx.BodyWriter(), 100*time.Millisecond)
			defer fw.Stop()

			for {
				written, err := io.Copy(fw, f)
				if err != nil {
					if isClientDisconnect(err) {
						h.logger.Warn("client disconnected during command stream", "error", err, "cmdID", cmdID)
					} else {
						h.logger.Error("unexpected error streaming command output", "error", err, "cmdID", cmdID)
					}
					return
				}
				eof := written == 0 && err == nil
				if !eof {
					continue
				}
				// EOF - check if process is done or wait for more data
				select {
				case <-entry.done:
					// process exited; drain anything written between the read and the channel check
					if _, err := io.Copy(fw, f); err != nil {
						if isClientDisconnect(err) {
							h.logger.Warn("client disconnected during command stream", "error", err, "cmdID", cmdID)
						} else {
							h.logger.Error("error during final drain of command output", "error", err, "cmdID", cmdID)
						}
					}
					return
				case <-ctx.Context().Done():
					h.logger.Warn("client disconnected during command stream", "cmdID", cmdID)
					return
				default:
					// we could have used inotify to watch writes to the file, but it would complicate the solution
					// and introduce possible file descriptor saturation issues, and waking up a few times a second
					// to poll the file isn't that bad nor we need ~0 latency on sudden writes to the file
					time.Sleep(20 * time.Millisecond)
				}
			}
		},
	}, nil
}

func (h *CommandHandlers) PostCommandStdin(_ context.Context, input *PostCommandStdinInput) (*struct{}, error) {
	entry, err := h.getCommandEntry(input.CmdID)
	if err != nil {
		return nil, err
	}

	select {
	case <-entry.done:
		return nil, huma.Error409Conflict("command has already exited")
	default:
	}

	entry.stdinMu.Lock()
	written, err := io.Copy(entry.stdinPipe, input.Stream)
	entry.stdinMu.Unlock()

	if err != nil {
		// Process may have exited between the check and the write
		if errors.Is(err, syscall.EPIPE) || errors.Is(err, os.ErrClosed) {
			return nil, huma.Error409Conflict("command has already exited")
		}
		h.logger.Error("failed to write to stdin", "error", err, "cmdID", input.CmdID)
		return nil, huma.Error500InternalServerError("failed to write to stdin")
	}

	h.logger.Debug("stdin write", "cmdID", input.CmdID, "bytes", written)
	return nil, nil
}

// todo benl: delete from commands map (eventually?) to constraint memory?
func (h *CommandHandlers) DeleteCommand(_ context.Context, input *DeleteCommandInput) (*struct{}, error) {
	entry, err := h.getCommandEntry(input.CmdID)
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
