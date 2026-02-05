package handlers

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

const (
	defaultExecTimeout = 60
	maxExecTimeout     = 3600
)

// ExecHandlers handles command execution requests.
type ExecHandlers struct {
	logger *slog.Logger
	procFS proc.ProcFS
	fsh    *FilesystemHandlers // reuse PID caching
}

// NewExecHandlers creates a new ExecHandlers instance.
func NewExecHandlers(logger *slog.Logger, procFS proc.ProcFS, fsh *FilesystemHandlers) *ExecHandlers {
	return &ExecHandlers{
		logger: logger,
		procFS: procFS,
		fsh:    fsh,
	}
}

// PostExec executes a command in the container and returns the result.
func (h *ExecHandlers) PostExec(ctx context.Context, input *ExecInput) (*ExecOutput, error) {
	if input.Cmd == "" {
		return nil, huma.Error400BadRequest("cmd is required")
	}

	// Validate and set timeout
	timeout := input.Timeout
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}
	if timeout > maxExecTimeout {
		return nil, huma.Error400BadRequest(fmt.Sprintf("timeout cannot exceed %d seconds", maxExecTimeout))
	}

	pid, err := h.fsh.findCachedContainerPID(input.Container)
	if err != nil {
		return nil, huma.Error400BadRequest("container not found")
	}

	uid, gid, err := h.procFS.GetUIDGID(pid)
	if err != nil {
		h.logger.Error("failed to get container uid/gid", "error", err, "pid", pid)
		return nil, huma.Error500InternalServerError("failed to get container uid/gid")
	}

	// Get the container's root filesystem path
	rootPath := h.procFS.GetRoot(pid)

	// Resolve working directory
	cwd := input.Cwd
	if cwd == "" {
		cwd, err = h.procFS.GetCwd(pid)
		if err != nil {
			h.logger.Error("failed to get container cwd", "error", err, "pid", pid)
			cwd = "/"
		}
	}

	// Get container's environment
	containerEnv, err := proc.GetEnviron(pid)
	if err != nil {
		h.logger.Warn("failed to read container environment, using minimal env", "error", err)
		containerEnv = []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	}

	// Append user-provided environment variables
	if len(input.Env) > 0 {
		containerEnv = append(containerEnv, input.Env...)
	}

	// Create command with timeout
	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Use nsenter to enter the container's namespaces and run the command
	// This is more robust than chroot as it handles mount, pid, and other namespaces
	nsenterArgs := []string{
		"-t", fmt.Sprintf("%d", pid),
		"-m", "-u", "-i", "-n", "-p",
		"--",
		"sh", "-c",
		fmt.Sprintf("cd %s && exec %s", shellQuote(cwd), buildCommand(input.Cmd, input.Args)),
	}

	cmd := exec.CommandContext(execCtx, "nsenter", nsenterArgs...)
	cmd.Env = containerEnv
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}

	// If nsenter is not available, fall back to chroot
	if _, err := exec.LookPath("nsenter"); err != nil {
		cmd = h.buildChrootCommand(execCtx, rootPath, cwd, input.Cmd, input.Args, containerEnv, uid, gid)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if execCtx.Err() == context.DeadlineExceeded {
			return nil, huma.Error504GatewayTimeout("command execution timed out")
		} else {
			h.logger.Error("command execution failed", "error", err, "cmd", input.Cmd)
			return nil, huma.Error500InternalServerError(fmt.Sprintf("command execution failed: %v", err))
		}
	}

	return &ExecOutput{
		Body: ExecResponse{
			ExitCode: exitCode,
			Stdout:   stdout.String(),
			Stderr:   stderr.String(),
		},
	}, nil
}

func (h *ExecHandlers) buildChrootCommand(ctx context.Context, rootPath, cwd, cmdName string, args []string, env []string, uid, gid int) *exec.Cmd {
	// Build the full command string for chroot
	fullCmd := buildCommand(cmdName, args)
	chrootArgs := []string{rootPath, "sh", "-c", fmt.Sprintf("cd %s && exec %s", shellQuote(cwd), fullCmd)}

	cmd := exec.CommandContext(ctx, "chroot", chrootArgs...)
	cmd.Env = env
	cmd.Dir = filepath.Join(rootPath, cwd)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: uint32(uid),
			Gid: uint32(gid),
		},
	}

	return cmd
}

// buildCommand constructs a shell command string from command and arguments.
func buildCommand(cmd string, args []string) string {
	if len(args) == 0 {
		return shellQuote(cmd)
	}

	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(cmd))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

// shellQuote escapes a string for safe shell usage.
func shellQuote(s string) string {
	// If the string contains no special characters, return as-is
	if !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}();&|<>") {
		return s
	}
	// Use single quotes and escape any single quotes within
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
