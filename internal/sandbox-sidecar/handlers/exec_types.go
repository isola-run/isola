package handlers

// ExecInput represents a request to execute a command in a container.
type ExecInput struct {
	Container string   `query:"container" doc:"Container name (defaults to main container)"`
	Cmd       string   `json:"cmd" required:"true" doc:"Command to execute"`
	Args      []string `json:"args" doc:"Command arguments"`
	Cwd       string   `json:"cwd" doc:"Working directory (defaults to container cwd)"`
	Env       []string `json:"env" doc:"Additional environment variables (KEY=VALUE format)"`
	Timeout   int      `json:"timeout" doc:"Timeout in seconds (default 60, max 3600)"`
}

// ExecResponse represents the result of command execution.
type ExecResponse struct {
	ExitCode int    `json:"exit_code" doc:"Process exit code"`
	Stdout   string `json:"stdout" doc:"Standard output"`
	Stderr   string `json:"stderr" doc:"Standard error"`
}

// ExecOutput wraps ExecResponse for Huma.
type ExecOutput struct {
	Body ExecResponse
}

// ExecStreamInput represents a request to execute a command with streaming output.
type ExecStreamInput struct {
	Container string `query:"container" doc:"Container name (defaults to main container)"`
	Cmd       string `query:"cmd" required:"true" doc:"Command to execute"`
	Args      string `query:"args" doc:"Command arguments (comma-separated)"`
	Cwd       string `query:"cwd" doc:"Working directory (defaults to container cwd)"`
	Timeout   int    `query:"timeout" doc:"Timeout in seconds (default 60, max 3600)"`
}

// PTYInput represents a request to create an interactive PTY session.
type PTYInput struct {
	Container string `query:"container" doc:"Container name (defaults to main container)"`
	Cmd       string `query:"cmd" doc:"Command to execute (defaults to /bin/sh)"`
	Cols      int    `query:"cols" doc:"Terminal columns (default 80)"`
	Rows      int    `query:"rows" doc:"Terminal rows (default 24)"`
}

// PTYResizeInput represents a request to resize a PTY session.
type PTYResizeInput struct {
	SessionID string `path:"session_id" required:"true" doc:"PTY session ID"`
	Cols      int    `json:"cols" required:"true" doc:"New terminal columns"`
	Rows      int    `json:"rows" required:"true" doc:"New terminal rows"`
}

// PTYResizeOutput represents the response for PTY resize.
type PTYResizeOutput struct {
	Body struct {
		Success bool `json:"success"`
	}
}
