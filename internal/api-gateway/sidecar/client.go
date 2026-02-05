package sidecar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// SidecarPort is the port where the sandbox sidecar listens.
	SidecarPort = 10032

	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = 60 * time.Second
)

// Client communicates with sandbox sidecars.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a new sidecar client.
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
	}
}

// NewClientWithTimeout creates a new sidecar client with a custom timeout.
func NewClientWithTimeout(timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// ExecRequest represents a command execution request.
type ExecRequest struct {
	Cmd       string   `json:"cmd"`
	Args      []string `json:"args,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	Env       []string `json:"env,omitempty"`
	Timeout   int      `json:"timeout,omitempty"`
	Container string   `json:"-"` // Sent as query param
}

// ExecResponse represents the result of command execution.
type ExecResponse struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

// buildSidecarURL constructs the URL for a sidecar endpoint.
func buildSidecarURL(podIP, path string, queryParams url.Values) string {
	u := url.URL{
		Scheme:   "http",
		Host:     fmt.Sprintf("%s:%d", podIP, SidecarPort),
		Path:     path,
		RawQuery: queryParams.Encode(),
	}
	return u.String()
}

// Exec executes a command in the sandbox and returns the result.
func (c *Client) Exec(ctx context.Context, podIP string, req *ExecRequest) (*ExecResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	query := url.Values{}
	if req.Container != "" {
		query.Set("container", req.Container)
	}

	sidecarURL := buildSidecarURL(podIP, "/exec", query)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sidecar returned status %d: %s", resp.StatusCode, string(body))
	}

	var execResp ExecResponse
	if err := json.NewDecoder(resp.Body).Decode(&execResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &execResp, nil
}

// WriteFile writes a file to the sandbox filesystem.
func (c *Client) WriteFile(ctx context.Context, podIP, path string, content io.Reader, container string) error {
	query := url.Values{}
	query.Set("path", path)
	if container != "" {
		query.Set("container", container)
	}

	sidecarURL := buildSidecarURL(podIP, "/filesystem", query)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, sidecarURL, content)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sidecar returned status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Health checks if the sidecar is healthy.
func (c *Client) Health(ctx context.Context, podIP string) error {
	sidecarURL := buildSidecarURL(podIP, "/health", nil)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sidecar returned status %d", resp.StatusCode)
	}

	return nil
}

// GetWebSocketURL returns the WebSocket URL for exec streaming.
func GetWebSocketURL(podIP, path string, queryParams url.Values) string {
	u := url.URL{
		Scheme:   "ws",
		Host:     fmt.Sprintf("%s:%d", podIP, SidecarPort),
		Path:     path,
		RawQuery: queryParams.Encode(),
	}
	return u.String()
}
