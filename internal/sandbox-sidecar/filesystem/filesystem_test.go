package filesystem

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sandboxsidecar "github.com/isola-ai/isola-sb/internal/sandbox-sidecar"
	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

var _ = Describe("Filesystem", func() {
	Describe("GET /filesystem", func() {
		It("reads file with absolute path", func() {
			content := []byte("hello world")
			hostPath := filepath.Join(testRootDir, "/tmp/readable.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/filesystem?path=/tmp/readable.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("reads file with relative path", func() {
			content := []byte("relative file content")
			hostPath := filepath.Join(testRootDir, testCwd, "myreadfile.txt")
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/filesystem?path=myreadfile.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("reads binary content correctly", func() {
			content := []byte{0x00, 0xFF, 0x80, 0x01, 0xFE, 0x7F}
			hostPath := filepath.Join(testRootDir, "/tmp/binary.dat")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/filesystem?path=/tmp/binary.dat")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("returns 404 for nonexistent file", func() {
			resp := doGet("/filesystem?path=/tmp/nonexistent.txt")

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 400 for directory path", func() {
			resp := doGet("/filesystem?path=/workspace")

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 400 for FIFO (named pipe)", func() {
			fifoPath := filepath.Join(testRootDir, "/tmp/test.fifo")
			Expect(os.MkdirAll(filepath.Dir(fifoPath), 0750)).To(Succeed())
			Expect(syscall.Mkfifo(fifoPath, 0600)).To(Succeed())

			resp := doGet("/filesystem?path=/tmp/test.fifo")

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("follows symlink to regular file", func() {
			content := []byte("symlink target content")
			targetPath := filepath.Join(testRootDir, "/tmp/symlink-target.txt")
			linkPath := filepath.Join(testRootDir, "/tmp/symlink.txt")
			Expect(os.MkdirAll(filepath.Dir(targetPath), 0750)).To(Succeed())
			Expect(os.WriteFile(targetPath, content, 0600)).To(Succeed())
			Expect(os.Symlink(targetPath, linkPath)).To(Succeed())

			resp := doGet("/filesystem?path=/tmp/symlink.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("returns 404 for dangling symlink", func() {
			linkPath := filepath.Join(testRootDir, "/tmp/dangling.txt")
			Expect(os.MkdirAll(filepath.Dir(linkPath), 0750)).To(Succeed())
			Expect(os.Symlink("/nonexistent/target", linkPath)).To(Succeed())

			resp := doGet("/filesystem?path=/tmp/dangling.txt")

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 422 when path is missing", func() {
			resp := doGet("/filesystem")

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

		It("returns 422 when path is empty", func() {
			resp := doGet("/filesystem?path=")

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

		It("returns 500 for null bytes in path", func() {
			resp := doGet("/filesystem?path=/tmp/evil%00file.txt")

			Expect(resp.Code).To(Equal(http.StatusInternalServerError))
		})

		It("succeeds with container specified", func() {
			content := []byte("container read test")
			hostPath := filepath.Join(testRootDir, "/tmp/container-read.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/filesystem?path=/tmp/container-read.txt&container=main")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("normalizes path with dot segments", func() {
			content := []byte("normalized content")
			hostPath := filepath.Join(testRootDir, "/tmp/normalized-read.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/filesystem?path=/tmp/../tmp/./normalized-read.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("reads empty file", func() {
			hostPath := filepath.Join(testRootDir, "/tmp/empty-read.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte{}, 0600)).To(Succeed())

			resp := doGet("/filesystem?path=/tmp/empty-read.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Len()).To(Equal(0))
		})

		It("sets Content-Type and Content-Length headers", func() {
			content := []byte("header check content")
			hostPath := filepath.Join(testRootDir, "/tmp/headers.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/filesystem?path=/tmp/headers.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Header().Get("Content-Type")).To(Equal("application/octet-stream"))
		})
	})

	Describe("POST /filesystem", func() {
		It("writes file with absolute path", func() {
			content := []byte("hello world")
			resp := doPost("/filesystem?path=/tmp/test.txt", content)

			Expect(resp.Code).To(Equal(http.StatusCreated))

			var body sidecarapi.FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.AbsolutePath).To(Equal("/tmp/test.txt"))
			Expect(body.BytesWritten).To(Equal(int64(len(content))))

			// Verify file was written
			hostPath := filepath.Join(testRootDir, "/tmp/test.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("writes file with relative path", func() {
			content := []byte("relative file content")
			resp := doPost("/filesystem?path=myfile.txt", content)

			Expect(resp.Code).To(Equal(http.StatusCreated))

			var body sidecarapi.FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.AbsolutePath).To(Equal("/workspace/myfile.txt"))
			Expect(body.BytesWritten).To(Equal(int64(len(content))))

			// Verify file was written
			hostPath := filepath.Join(testRootDir, "/workspace/myfile.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("creates parent directories", func() {
			content := []byte("nested file")
			resp := doPost("/filesystem?path=/deep/nested/dir/file.txt", content)

			Expect(resp.Code).To(Equal(http.StatusCreated))

			var body sidecarapi.FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.AbsolutePath).To(Equal("/deep/nested/dir/file.txt"))

			// Verify file was written
			hostPath := filepath.Join(testRootDir, "/deep/nested/dir/file.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("returns 422 when path is missing", func() {
			resp := doPost("/filesystem", []byte("some content"))

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

		It("returns 422 when path is empty", func() {
			resp := doPost("/filesystem?path=", []byte("some content"))

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

		It("succeeds when container is specified", func() {
			content := []byte("container test")
			resp := doPost("/filesystem?path=/tmp/container-test.txt&container=main", content)

			Expect(resp.Code).To(Equal(http.StatusCreated))

			var body sidecarapi.FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.AbsolutePath).To(Equal("/tmp/container-test.txt"))
		})

		It("writes empty file", func() {
			resp := doPost("/filesystem?path=/tmp/empty.txt", []byte{})

			Expect(resp.Code).To(Equal(http.StatusCreated))

			var body sidecarapi.FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.BytesWritten).To(Equal(int64(0)))

			// Verify file was created
			hostPath := filepath.Join(testRootDir, "/tmp/empty.txt")
			info, err := os.Stat(hostPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Size()).To(Equal(int64(0)))
		})

		It("normalizes path with dot segments", func() {
			content := []byte("normalized")
			resp := doPost("/filesystem?path=/tmp/../tmp/./normalized.txt", content)

			Expect(resp.Code).To(Equal(http.StatusCreated))

			var body sidecarapi.FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.AbsolutePath).To(Equal("/tmp/normalized.txt"))
		})

		It("succeeds with empty container name", func() {
			content := []byte("no container specified")
			resp := doPost("/filesystem?path=/tmp/no-container.txt", content)

			Expect(resp.Code).To(Equal(http.StatusCreated))

			var body sidecarapi.FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.AbsolutePath).To(Equal("/tmp/no-container.txt"))
		})

		It("returns 500 for null bytes in path", func() {
			content := []byte("malicious content")
			resp := doPost("/filesystem?path=/tmp/evil%00file.txt", content)

			Expect(resp.Code).To(Equal(http.StatusInternalServerError))
		})
	})
})

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

// deadlineCapture is an http.ResponseWriter that records Set*Deadline calls.
// http.ResponseController discovers the methods via interface assertions.
type deadlineCapture struct {
	httptest.ResponseRecorder
	mu             sync.Mutex
	writeDeadlines []time.Time
	readDeadlines  []time.Time
}

func (d *deadlineCapture) SetWriteDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writeDeadlines = append(d.writeDeadlines, t)
	return nil
}

func (d *deadlineCapture) SetReadDeadline(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.readDeadlines = append(d.readDeadlines, t)
	return nil
}

func (d *deadlineCapture) Flush() {
	d.mu.Lock()
	defer d.mu.Unlock()
}

func (d *deadlineCapture) getWriteDeadlines() []time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]time.Time{}, d.writeDeadlines...)
}

func (d *deadlineCapture) getReadDeadlines() []time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]time.Time{}, d.readDeadlines...)
}

var _ = Describe("Filesystem deadline wiring", func() {
	var (
		fsHandler http.Handler
		rootDir   string
	)

	BeforeEach(func() {
		var err error
		rootDir, err = os.MkdirTemp("", "deadline-test-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(rootDir) })

		Expect(os.MkdirAll(filepath.Join(rootDir, "/workspace"), 0750)).To(Succeed())

		mockProcFS := &MockProcFS{
			rootDir: rootDir,
			cwd:     "/workspace",
			uid:     os.Getuid(),
			gid:     os.Getgid(),
		}

		logger := slog.New(slog.NewTextHandler(GinkgoWriter, nil))
		var api humatest.TestAPI
		fsHandler, api = humatest.New(GinkgoT(), huma.DefaultConfig("Deadline Test API", "1.0.0"))
		h := New(logger, mockProcFS, sandboxsidecar.NewPIDResolver(mockProcFS))
		Register(api, h)
	})

	It("sets read and write deadlines during file upload", func() {
		body := make([]byte, 48*1024)
		_, err := rand.Read(body)
		Expect(err).NotTo(HaveOccurred())

		mock := &deadlineCapture{ResponseRecorder: *httptest.NewRecorder()}
		req := httptest.NewRequest("POST", "/filesystem?path=/tmp/deadline-upload.txt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/octet-stream")

		fsHandler.ServeHTTP(mock, req)

		Expect(mock.Code).To(Equal(http.StatusCreated))
		Expect(mock.getReadDeadlines()).NotTo(BeEmpty())
		Expect(mock.getWriteDeadlines()).NotTo(BeEmpty())

		hostPath := filepath.Join(rootDir, "/tmp/deadline-upload.txt")
		written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
		Expect(err).NotTo(HaveOccurred())
		Expect(written).To(Equal(body))
	})

	It("sets write deadlines during file download", func() {
		content := make([]byte, 48*1024)
		_, err := rand.Read(content)
		Expect(err).NotTo(HaveOccurred())

		hostPath := filepath.Join(rootDir, "/tmp/deadline-download.txt")
		Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
		Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

		mock := &deadlineCapture{ResponseRecorder: *httptest.NewRecorder()}
		req := httptest.NewRequest("GET", "/filesystem?path=/tmp/deadline-download.txt", nil)

		fsHandler.ServeHTTP(mock, req)

		Expect(mock.Code).To(Equal(http.StatusOK))
		Expect(mock.getWriteDeadlines()).NotTo(BeEmpty())
		Expect(mock.Body.Bytes()).To(Equal(content))
	})
})

var _ = Describe("Filesystem error cases", func() {
	var errorAPI humatest.TestAPI

	Describe("container not found", func() {
		BeforeEach(func() {
			logger := slog.New(slog.NewTextHandler(GinkgoWriter, nil))

			mockProcFS := &errorMockProcFS{
				findPIDError: proc.ErrContainerNotFound,
			}

			_, errorAPI = humatest.New(GinkgoT(), huma.DefaultConfig("Error Test API", "1.0.0"))
			handler := New(logger, mockProcFS, sandboxsidecar.NewPIDResolver(mockProcFS))
			Register(errorAPI, handler)
		})

		It("returns 400 when container not found", func() {
			resp := errorAPI.Post("/filesystem?path=/tmp/test.txt", "application/octet-stream", bytes.NewReader([]byte("content")))

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})
	})
})
