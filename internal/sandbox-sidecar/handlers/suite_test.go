package handlers

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

var (
	testAPI     humatest.TestAPI
	testRootDir string
	testCwd     string
)

// DirectCommandBuilder runs commands directly without nsenter (for testing).
type DirectCommandBuilder struct{}

func (b *DirectCommandBuilder) Build(ctx context.Context, _ int, req sidecarapi.CreateCommandRequest, env []string, stdoutFile, stderrFile *os.File) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, req.Cmd, req.Args...) //nolint:gosec // test-only builder; inputs are test fixtures
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = env
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	return cmd, nil
}

// MockProcFS implements proc.ProcFS for testing.
type MockProcFS struct {
	rootDir string
	cwd     string
	uid     int
	gid     int
}

func (m *MockProcFS) FindMarkedPID(containerName string) (int, error) {
	return 1, nil
}

func (m *MockProcFS) GetCwd(pid int) (string, error) {
	return m.cwd, nil
}

func (m *MockProcFS) GetRoot(pid int) string {
	return m.rootDir
}

func (m *MockProcFS) GetUIDGID(pid int) (int, int, error) {
	return m.uid, m.gid, nil
}

func (m *MockProcFS) GetEnviron(pid int) ([]string, error) {
	return []string{"PATH=/usr/bin:/bin", "HOME=/root"}, nil
}

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sidecar Handlers Suite")
}

var _ = BeforeSuite(func() {
	logger := slog.New(slog.NewTextHandler(GinkgoWriter, nil))

	// Create temp directories for testing
	var err error
	testRootDir, err = os.MkdirTemp("", "sidecar-test-root-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = os.RemoveAll(testRootDir) })

	testCwd = "/workspace"
	// Create the cwd directory in the mock root
	err = os.MkdirAll(testRootDir+testCwd, 0750)
	Expect(err).NotTo(HaveOccurred())

	mockProcFS := &MockProcFS{
		rootDir: testRootDir,
		cwd:     testCwd,
		uid:     os.Getuid(),
		gid:     os.Getgid(),
	}

	_, testAPI = humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "1.0.0"))

	healthHandlers := NewHealthHandlers()
	filesystemHandlers := NewFilesystemHandlers(logger, mockProcFS)

	RegisterHealthRoutes(testAPI, healthHandlers)
	RegisterFilesystemRoutes(testAPI, filesystemHandlers)
})

func doPost(path string, body []byte) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	return testAPI.Post(path, "Content-Type: application/octet-stream", bodyReader)
}

func doGet(path string) *httptest.ResponseRecorder {
	return testAPI.Get(path)
}
