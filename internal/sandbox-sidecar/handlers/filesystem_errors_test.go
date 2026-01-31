package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

// errorMockProcFS returns errors for FindMarkedPID.
type errorMockProcFS struct {
	findPIDError error
}

func (m *errorMockProcFS) FindMarkedPID(containerName string) (int, error) {
	return 0, m.findPIDError
}

func (m *errorMockProcFS) GetCwd(pid int) (string, error) {
	return "/workspace", nil
}

func (m *errorMockProcFS) GetRoot(pid int) string {
	return "/tmp"
}

func (m *errorMockProcFS) GetUIDGID(pid int) (int, int, error) {
	return os.Getuid(), os.Getgid(), nil
}

var _ = Describe("Filesystem error cases", func() {
	var (
		server *httptest.Server
		client *http.Client
	)

	doPostWithClient := func(c *http.Client, serverURL, path string, body string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, serverURL+path, strings.NewReader(body))
		Expect(err).NotTo(HaveOccurred())
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := c.Do(req)
		Expect(err).NotTo(HaveOccurred())
		return resp
	}

	Describe("container not found", func() {
		BeforeEach(func() {
			gin.SetMode(gin.TestMode)
			logger := slog.New(slog.NewTextHandler(GinkgoWriter, nil))

			mockProcFS := &errorMockProcFS{
				findPIDError: proc.ErrContainerNotFound,
			}

			r := gin.New()
			r.Use(gin.Recovery())
			handler := NewFilesystemHandler(logger, mockProcFS)
			r.POST("/filesystem", handler.PostFilesystem)

			server = httptest.NewServer(r)
			client = server.Client()
			DeferCleanup(server.Close)
		})

		It("returns 400 when container not found", func() {
			resp := doPostWithClient(client, server.URL, "/filesystem?path=/tmp/test.txt", "content")
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))

			var body ErrorResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Message).To(Equal("container not found"))
		})
	})
})
