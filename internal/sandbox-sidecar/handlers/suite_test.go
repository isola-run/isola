package handlers

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	testAPI          humatest.TestAPI
	testRootDir      string
	testCwd          string
	testExecHandlers *ExecHandlers
	testExecCtx      context.Context
)

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

func (m *MockProcFS) ReadEnviron(pid int) ([]string, error) {
	return []string{"PATH=/usr/bin", "HOME=/root"}, nil
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
	pidCache := NewPIDCache(mockProcFS)
	filesystemHandlers := NewFilesystemHandlers(logger, mockProcFS, pidCache)

	testExecHandlers = NewExecHandlers(logger, mockProcFS, pidCache)
	testExecCtx = context.Background()

	RegisterHealthRoutes(testAPI, healthHandlers)
	RegisterFilesystemRoutes(testAPI, filesystemHandlers)
	RegisterExecRoutes(testAPI, testExecHandlers)
})

func doPost(path string, body []byte) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	return testAPI.Post(path, "Content-Type: application/octet-stream", bodyReader)
}
