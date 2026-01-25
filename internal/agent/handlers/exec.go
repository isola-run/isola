package handlers

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ExecRequest represents a command execution request.
type ExecRequest struct {
	// Command to execute (required). Can be a full command string or just the binary.
	Command string `json:"command" binding:"required"`

	// Args are optional arguments when Command is just a binary name.
	Args []string `json:"args,omitempty"`

	// WorkDir is the working directory for the command. Defaults to "/".
	WorkDir string `json:"workDir,omitempty"`

	// Env are additional environment variables in KEY=VALUE format.
	Env []string `json:"env,omitempty"`

	// Timeout in seconds for sync execution. 0 means no timeout. Default: 60.
	Timeout int `json:"timeout,omitempty"`

	// Background runs the command asynchronously and returns immediately.
	Background bool `json:"background,omitempty"`

	// Stdin is optional input to pass to the command's stdin.
	Stdin string `json:"stdin,omitempty"`
}

// ExecResponse represents a command execution response.
type ExecResponse struct {
	// ID is set for background processes.
	ID string `json:"id,omitempty"`

	// Stdout from the command (for sync execution or completed background).
	Stdout string `json:"stdout,omitempty"`

	// Stderr from the command.
	Stderr string `json:"stderr,omitempty"`

	// ExitCode of the command. Nil if still running.
	ExitCode *int `json:"exitCode,omitempty"`

	// State of the process (for background execution).
	State ProcessState `json:"state,omitempty"`

	// Error message if execution failed.
	Error string `json:"error,omitempty"`
}

// GetExecResponse represents the response for getting process status.
type GetExecResponse struct {
	ID        string       `json:"id"`
	Command   string       `json:"command"`
	Args      []string     `json:"args,omitempty"`
	State     ProcessState `json:"state"`
	Stdout    string       `json:"stdout"`
	Stderr    string       `json:"stderr"`
	ExitCode  *int         `json:"exitCode,omitempty"`
	StartedAt time.Time    `json:"startedAt"`
	EndedAt   *time.Time   `json:"endedAt,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// KillExecResponse represents the response for killing a process.
type KillExecResponse struct {
	ID     string       `json:"id"`
	State  ProcessState `json:"state"`
	Killed bool         `json:"killed"`
}

// Exec handles POST /exec requests.
func (h *Handler) Exec(c *gin.Context) {
	var req ExecRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	// Set default timeout for sync execution
	timeout := time.Duration(req.Timeout) * time.Second
	if req.Timeout == 0 && !req.Background {
		timeout = 60 * time.Second
	}

	// Resolve working directory via procfs
	workDir := req.WorkDir
	if workDir == "" {
		workDir = "/"
	}
	resolvedWorkDir, err := h.procFS.ResolvePath(workDir)
	if err != nil {
		log.Printf("Failed to resolve workDir: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to resolve working directory: " + err.Error()})
		return
	}

	// Parse command - if no args provided, use shell to interpret
	command := req.Command
	args := req.Args

	if len(args) == 0 {
		// Use shell to interpret the command
		args = []string{"-c", command}
		command = "/bin/sh"
	}

	if req.Background {
		// Async execution
		id := uuid.New().String()[:8]
		proc, err := h.processManager.StartProcess(id, command, args, resolvedWorkDir, req.Env)
		if err != nil {
			log.Printf("Failed to start background process: %v", err)
			c.JSON(http.StatusInternalServerError, ExecResponse{
				ID:    id,
				State: ProcessStateError,
				Error: err.Error(),
			})
			return
		}

		log.Printf("Started background process %s: %s", id, req.Command)
		c.JSON(http.StatusAccepted, ExecResponse{
			ID:    proc.ID,
			State: proc.State,
		})
		return
	}

	// Sync execution
	var stdout, stderr string
	var exitCode int

	if req.Stdin != "" {
		stdout, stderr, exitCode, err = h.processManager.RunSyncWithStdin(
			c.Request.Context(),
			command,
			args,
			resolvedWorkDir,
			req.Env,
			strings.NewReader(req.Stdin),
			timeout,
		)
	} else {
		stdout, stderr, exitCode, err = h.processManager.RunSync(
			c.Request.Context(),
			command,
			args,
			resolvedWorkDir,
			req.Env,
			timeout,
		)
	}

	if err != nil {
		log.Printf("Failed to execute command: %v", err)
		c.JSON(http.StatusInternalServerError, ExecResponse{
			Stdout:   stdout,
			Stderr:   stderr,
			ExitCode: &exitCode,
			State:    ProcessStateError,
			Error:    err.Error(),
		})
		return
	}

	log.Printf("Executed command: %s (exit=%d)", truncate(req.Command, 50), exitCode)
	c.JSON(http.StatusOK, ExecResponse{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: &exitCode,
		State:    ProcessStateCompleted,
	})
}

// GetExec handles GET /exec/:id requests.
func (h *Handler) GetExec(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "id is required"})
		return
	}

	proc := h.processManager.GetProcess(id)
	if proc == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "process not found"})
		return
	}

	c.JSON(http.StatusOK, GetExecResponse{
		ID:        proc.ID,
		Command:   proc.Command,
		Args:      proc.Args,
		State:     proc.State,
		Stdout:    proc.GetStdout(),
		Stderr:    proc.GetStderr(),
		ExitCode:  proc.ExitCode,
		StartedAt: proc.StartedAt,
		EndedAt:   proc.EndedAt,
		Error:     proc.Error,
	})
}

// KillExec handles POST /exec/:id/kill requests.
func (h *Handler) KillExec(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "id is required"})
		return
	}

	proc := h.processManager.GetProcess(id)
	if proc == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "process not found"})
		return
	}

	wasRunning := proc.State == ProcessStateRunning
	if err := h.processManager.KillProcess(id); err != nil {
		log.Printf("Failed to kill process %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to kill process: " + err.Error()})
		return
	}

	// Refresh state after kill
	proc = h.processManager.GetProcess(id)
	log.Printf("Killed process %s (wasRunning=%v)", id, wasRunning)

	c.JSON(http.StatusOK, KillExecResponse{
		ID:     id,
		State:  proc.State,
		Killed: wasRunning,
	})
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
