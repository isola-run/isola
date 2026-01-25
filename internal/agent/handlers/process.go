package handlers

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// ProcessState represents the state of a process.
type ProcessState string

const (
	ProcessStateRunning   ProcessState = "running"
	ProcessStateCompleted ProcessState = "completed"
	ProcessStateKilled    ProcessState = "killed"
	ProcessStateError     ProcessState = "error"
)

// Process represents a running or completed process.
type Process struct {
	ID        string       `json:"id"`
	Command   string       `json:"command"`
	Args      []string     `json:"args,omitempty"`
	State     ProcessState `json:"state"`
	ExitCode  *int         `json:"exitCode,omitempty"`
	StartedAt time.Time    `json:"startedAt"`
	EndedAt   *time.Time   `json:"endedAt,omitempty"`
	Error     string       `json:"error,omitempty"`

	// Internal fields (not serialized)
	cmd    *exec.Cmd
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	mu     sync.RWMutex
	cancel context.CancelFunc
}

// GetStdout returns the current stdout content.
func (p *Process) GetStdout() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.stdout == nil {
		return ""
	}
	return p.stdout.String()
}

// GetStderr returns the current stderr content.
func (p *Process) GetStderr() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.stderr == nil {
		return ""
	}
	return p.stderr.String()
}

// ProcessManager manages running processes.
type ProcessManager struct {
	processes map[string]*Process
	mu        sync.RWMutex
}

// NewProcessManager creates a new ProcessManager.
func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		processes: make(map[string]*Process),
	}
}

// StartProcess starts a new process and returns immediately.
func (pm *ProcessManager) StartProcess(id, command string, args []string, workDir string, env []string) (*Process, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	if len(env) > 0 {
		cmd.Env = env
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Set process group so we can kill all children
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	proc := &Process{
		ID:        id,
		Command:   command,
		Args:      args,
		State:     ProcessStateRunning,
		StartedAt: time.Now(),
		cmd:       cmd,
		stdout:    stdout,
		stderr:    stderr,
		cancel:    cancel,
	}

	pm.mu.Lock()
	pm.processes[id] = proc
	pm.mu.Unlock()

	if err := cmd.Start(); err != nil {
		proc.State = ProcessStateError
		proc.Error = err.Error()
		now := time.Now()
		proc.EndedAt = &now
		return proc, err
	}

	// Monitor process completion in background
	go pm.waitForProcess(proc)

	return proc, nil
}

// RunSync runs a command synchronously with a timeout.
func (pm *ProcessManager) RunSync(ctx context.Context, command string, args []string, workDir string, env []string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	if len(env) > 0 {
		cmd.Env = env
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			err = nil // Non-zero exit is not an error
		} else if ctx.Err() == context.DeadlineExceeded {
			return stdout, stderr, -1, ctx.Err()
		}
	}

	return stdout, stderr, exitCode, err
}

// RunSyncWithStdin runs a command synchronously with stdin input.
func (pm *ProcessManager) RunSyncWithStdin(ctx context.Context, command string, args []string, workDir string, env []string, stdin io.Reader, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, command, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	if len(env) > 0 {
		cmd.Env = env
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	cmd.Stdin = stdin

	err = cmd.Run()
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			err = nil
		} else if ctx.Err() == context.DeadlineExceeded {
			return stdout, stderr, -1, ctx.Err()
		}
	}

	return stdout, stderr, exitCode, err
}

func (pm *ProcessManager) waitForProcess(proc *Process) {
	err := proc.cmd.Wait()

	proc.mu.Lock()
	defer proc.mu.Unlock()

	now := time.Now()
	proc.EndedAt = &now

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode := exitErr.ExitCode()
			proc.ExitCode = &exitCode
			proc.State = ProcessStateCompleted
		} else {
			proc.State = ProcessStateError
			proc.Error = err.Error()
		}
	} else {
		exitCode := 0
		proc.ExitCode = &exitCode
		proc.State = ProcessStateCompleted
	}
}

// GetProcess returns a process by ID.
func (pm *ProcessManager) GetProcess(id string) *Process {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.processes[id]
}

// KillProcess kills a process by ID.
func (pm *ProcessManager) KillProcess(id string) error {
	pm.mu.RLock()
	proc := pm.processes[id]
	pm.mu.RUnlock()

	if proc == nil {
		return nil
	}

	proc.mu.Lock()
	defer proc.mu.Unlock()

	if proc.State != ProcessStateRunning {
		return nil
	}

	// Kill the entire process group
	if proc.cmd.Process != nil {
		// Kill process group (negative PID)
		_ = syscall.Kill(-proc.cmd.Process.Pid, syscall.SIGKILL)
	}

	proc.cancel()
	proc.State = ProcessStateKilled
	now := time.Now()
	proc.EndedAt = &now

	return nil
}

// CleanupOldProcesses removes completed processes older than the given duration.
func (pm *ProcessManager) CleanupOldProcesses(maxAge time.Duration) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, proc := range pm.processes {
		if proc.State != ProcessStateRunning && proc.EndedAt != nil && proc.EndedAt.Before(cutoff) {
			delete(pm.processes, id)
		}
	}
}
