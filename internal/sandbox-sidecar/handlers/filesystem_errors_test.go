package handlers

import (
	"bytes"
	"log/slog"
	"net/http"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
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

func (m *errorMockProcFS) GetEnviron(pid int) ([]string, error) {
	return []string{"PATH=/usr/bin:/bin", "HOME=/root"}, nil
}

var _ = Describe("Filesystem error cases", func() {
	var errorAPI humatest.TestAPI

	Describe("container not found", func() {
		BeforeEach(func() {
			logger := slog.New(slog.NewTextHandler(GinkgoWriter, nil))

			mockProcFS := &errorMockProcFS{
				findPIDError: proc.ErrContainerNotFound,
			}

			_, errorAPI = humatest.New(GinkgoT(), huma.DefaultConfig("Error Test API", "1.0.0"))
			handler := NewFilesystemHandlers(logger, mockProcFS)
			RegisterFilesystemRoutes(errorAPI, handler)
		})

		It("returns 400 when container not found", func() {
			resp := errorAPI.Post("/filesystem?path=/tmp/test.txt", "application/octet-stream", bytes.NewReader([]byte("content")))

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})
	})
})
