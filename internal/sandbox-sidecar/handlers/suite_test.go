package handlers

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	testServer  *httptest.Server
	testClient  *http.Client
	testRootDir string
	testCwd     string
)

// MockProcFS implements proc.ProcFS for testing.
type MockProcFS struct {
	rootDir string
	cwd     string
}

func (m *MockProcFS) FindMarkedPID() (int, error) {
	return 1, nil
}

func (m *MockProcFS) GetCwd(pid int) (string, error) {
	return m.cwd, nil
}

func (m *MockProcFS) GetRoot(pid int) string {
	return m.rootDir
}

func TestHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sidecar Handlers Suite")
}

var _ = BeforeSuite(func() {
	gin.SetMode(gin.TestMode)
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
	}

	r := gin.New()
	r.Use(gin.Recovery())

	handler := NewHandler(logger, mockProcFS)
	r.GET("/health", handler.GetHealth)
	r.POST("/files/upload", handler.PostUpload)

	testServer = httptest.NewServer(r)
	DeferCleanup(testServer.Close)

	testClient = testServer.Client()
})

// doGet performs a GET request and returns the response.
// Caller is responsible for closing the response body.
func doGet(path string) *http.Response {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, testServer.URL+path, nil)
	Expect(err).NotTo(HaveOccurred())

	resp, err := testClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}

// doPost performs a POST request with the given body and returns the response.
// Caller is responsible for closing the response body.
func doPost(path string, body []byte) *http.Response {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, testServer.URL+path, bodyReader)
	Expect(err).NotTo(HaveOccurred())
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := testClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	return resp
}
