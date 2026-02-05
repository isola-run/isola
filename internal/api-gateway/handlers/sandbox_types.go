package handlers

// SandboxExecInput represents a request to execute a command in a sandbox.
type SandboxExecInput struct {
	SandboxName string   `path:"sandbox_name" required:"true" doc:"Name of the sandbox"`
	Container   string   `query:"container" doc:"Container name (defaults to main container)"`
	Cmd         string   `json:"cmd" required:"true" doc:"Command to execute"`
	Args        []string `json:"args" doc:"Command arguments"`
	Cwd         string   `json:"cwd" doc:"Working directory (defaults to container cwd)"`
	Env         []string `json:"env" doc:"Additional environment variables (KEY=VALUE format)"`
	Timeout     int      `json:"timeout" doc:"Timeout in seconds (default 60, max 3600)"`
}

// SandboxExecResponse represents the result of command execution.
type SandboxExecResponse struct {
	ExitCode int    `json:"exit_code" doc:"Process exit code"`
	Stdout   string `json:"stdout" doc:"Standard output"`
	Stderr   string `json:"stderr" doc:"Standard error"`
}

// SandboxExecOutput wraps SandboxExecResponse for Huma.
type SandboxExecOutput struct {
	Body SandboxExecResponse
}

// SandboxExecStreamInput represents a request to stream command execution.
type SandboxExecStreamInput struct {
	SandboxName string `path:"sandbox_name" required:"true" doc:"Name of the sandbox"`
	Container   string `query:"container" doc:"Container name (defaults to main container)"`
	Cmd         string `query:"cmd" required:"true" doc:"Command to execute"`
	Args        string `query:"args" doc:"Command arguments (comma-separated)"`
	Cwd         string `query:"cwd" doc:"Working directory (defaults to container cwd)"`
	Timeout     int    `query:"timeout" doc:"Timeout in seconds (default 60, max 3600)"`
}

// SandboxPTYInput represents a request to create an interactive PTY session.
type SandboxPTYInput struct {
	SandboxName string `path:"sandbox_name" required:"true" doc:"Name of the sandbox"`
	Container   string `query:"container" doc:"Container name (defaults to main container)"`
	Cmd         string `query:"cmd" doc:"Command to execute (defaults to /bin/sh)"`
	Cols        int    `query:"cols" doc:"Terminal columns (default 80)"`
	Rows        int    `query:"rows" doc:"Terminal rows (default 24)"`
}
