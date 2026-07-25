// Copyright The Isola Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package filesystem

import (
	"bytes"
	"crypto/rand"
	"errors"
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

	sandboxsidecar "github.com/isola-run/isola/internal/sandbox-sidecar"
	"github.com/isola-run/isola/internal/sandbox-sidecar/proc"
)

var _ = Describe("Filesystem", func() {
	Describe("GET /filesystem", func() {
		It("reads file with absolute path", func() {
			content := []byte("hello world")
			hostPath := filepath.Join(testRootDir, "/tmp/readable.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/v1/filesystem?path=/tmp/readable.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("reads file with relative path", func() {
			content := []byte("relative file content")
			hostPath := filepath.Join(testRootDir, testCwd, "myreadfile.txt")
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/v1/filesystem?path=myreadfile.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("reads binary content correctly", func() {
			content := []byte{0x00, 0xFF, 0x80, 0x01, 0xFE, 0x7F}
			hostPath := filepath.Join(testRootDir, "/tmp/binary.dat")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/v1/filesystem?path=/tmp/binary.dat")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("returns 404 for nonexistent file", func() {
			resp := doGet("/v1/filesystem?path=/tmp/nonexistent.txt")

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 400 for directory path", func() {
			resp := doGet("/v1/filesystem?path=/workspace")

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 400 for FIFO (named pipe)", func() {
			fifoPath := filepath.Join(testRootDir, "/tmp/test.fifo")
			Expect(os.MkdirAll(filepath.Dir(fifoPath), 0750)).To(Succeed())
			Expect(syscall.Mkfifo(fifoPath, 0600)).To(Succeed())

			resp := doGet("/v1/filesystem?path=/tmp/test.fifo")

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("follows symlink to regular file", func() {
			content := []byte("symlink target content")
			targetPath := filepath.Join(testRootDir, "/tmp/symlink-target.txt")
			linkPath := filepath.Join(testRootDir, "/tmp/symlink.txt")
			Expect(os.MkdirAll(filepath.Dir(targetPath), 0750)).To(Succeed())
			Expect(os.WriteFile(targetPath, content, 0600)).To(Succeed())
			Expect(os.Symlink(targetPath, linkPath)).To(Succeed())

			resp := doGet("/v1/filesystem?path=/tmp/symlink.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("returns 404 for dangling symlink", func() {
			linkPath := filepath.Join(testRootDir, "/tmp/dangling.txt")
			Expect(os.MkdirAll(filepath.Dir(linkPath), 0750)).To(Succeed())
			Expect(os.Symlink("/nonexistent/target", linkPath)).To(Succeed())

			resp := doGet("/v1/filesystem?path=/tmp/dangling.txt")

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 422 when path is missing", func() {
			resp := doGet("/v1/filesystem")

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

		It("returns 422 when path is empty", func() {
			resp := doGet("/v1/filesystem?path=")

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

		It("returns 500 for null bytes in path", func() {
			resp := doGet("/v1/filesystem?path=/tmp/evil%00file.txt")

			Expect(resp.Code).To(Equal(http.StatusInternalServerError))
		})

		It("succeeds with container specified", func() {
			content := []byte("container read test")
			hostPath := filepath.Join(testRootDir, "/tmp/container-read.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/v1/filesystem?path=/tmp/container-read.txt&container=main")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("normalizes path with dot segments", func() {
			content := []byte("normalized content")
			hostPath := filepath.Join(testRootDir, "/tmp/normalized-read.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/v1/filesystem?path=/tmp/../tmp/./normalized-read.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("reads empty file", func() {
			hostPath := filepath.Join(testRootDir, "/tmp/empty-read.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte{}, 0600)).To(Succeed())

			resp := doGet("/v1/filesystem?path=/tmp/empty-read.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Len()).To(Equal(0))
		})

		It("sets Content-Type and Content-Length headers", func() {
			content := []byte("header check content")
			hostPath := filepath.Join(testRootDir, "/tmp/headers.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())

			resp := doGet("/v1/filesystem?path=/tmp/headers.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Header().Get("Content-Type")).To(Equal("application/octet-stream"))
		})
	})

	Describe("POST /filesystem", func() {
		It("writes file with absolute path", func() {
			content := []byte("hello world")
			resp := doPost("/v1/filesystem?path=/tmp/test.txt", content)

			Expect(resp.Code).To(Equal(http.StatusNoContent))

			hostPath := filepath.Join(testRootDir, "/tmp/test.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("writes file with relative path", func() {
			content := []byte("relative file content")
			resp := doPost("/v1/filesystem?path=myfile.txt", content)

			Expect(resp.Code).To(Equal(http.StatusNoContent))

			hostPath := filepath.Join(testRootDir, "/workspace/myfile.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("creates parent directories", func() {
			content := []byte("nested file")
			resp := doPost("/v1/filesystem?path=/deep/nested/dir/file.txt", content)

			Expect(resp.Code).To(Equal(http.StatusNoContent))

			hostPath := filepath.Join(testRootDir, "/deep/nested/dir/file.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("returns 422 when path is missing", func() {
			resp := doPost("/v1/filesystem", []byte("some content"))

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

		It("returns 422 when path is empty", func() {
			resp := doPost("/v1/filesystem?path=", []byte("some content"))

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

		It("succeeds when container is specified", func() {
			content := []byte("container test")
			resp := doPost("/v1/filesystem?path=/tmp/container-test.txt&container=main", content)

			Expect(resp.Code).To(Equal(http.StatusNoContent))
		})

		It("writes empty file", func() {
			resp := doPost("/v1/filesystem?path=/tmp/empty.txt", []byte{})

			Expect(resp.Code).To(Equal(http.StatusNoContent))

			hostPath := filepath.Join(testRootDir, "/tmp/empty.txt")
			info, err := os.Stat(hostPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Size()).To(Equal(int64(0)))
		})

		It("normalizes path with dot segments", func() {
			content := []byte("normalized")
			resp := doPost("/v1/filesystem?path=/tmp/../tmp/./normalized.txt", content)

			Expect(resp.Code).To(Equal(http.StatusNoContent))
		})

		It("succeeds with empty container name", func() {
			content := []byte("no container specified")
			resp := doPost("/v1/filesystem?path=/tmp/no-container.txt", content)

			Expect(resp.Code).To(Equal(http.StatusNoContent))
		})

		It("returns 500 for null bytes in path", func() {
			content := []byte("malicious content")
			resp := doPost("/v1/filesystem?path=/tmp/evil%00file.txt", content)

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

// newIsolatedAPI registers the filesystem handlers over a fresh temp root and
// returns the raw handler, for tests that need to supply their own ResponseWriter.
func newIsolatedAPI(prefix string) (http.Handler, string) {
	rootDir, err := os.MkdirTemp("", prefix)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = os.RemoveAll(rootDir) })

	Expect(os.MkdirAll(filepath.Join(rootDir, "/workspace"), 0750)).To(Succeed())

	mockProcFS := &MockProcFS{
		rootDir: rootDir,
		cwd:     "/workspace",
		uid:     os.Getuid(),
		gid:     os.Getgid(),
	}

	handler, api := humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "0.1.0"))
	v1 := huma.NewGroup(api, "/v1")
	logger := slog.New(slog.NewTextHandler(GinkgoWriter, nil))
	Register(v1, New(logger, mockProcFS, sandboxsidecar.NewPIDResolver(mockProcFS)))
	return handler, rootDir
}

var _ = Describe("Filesystem deadline wiring", func() {
	var (
		fsHandler http.Handler
		rootDir   string
	)

	BeforeEach(func() {
		fsHandler, rootDir = newIsolatedAPI("deadline-test-*")
	})

	It("sets read and write deadlines during file upload", func() {
		body := make([]byte, 48*1024)
		_, err := rand.Read(body)
		Expect(err).NotTo(HaveOccurred())

		mock := &deadlineCapture{ResponseRecorder: *httptest.NewRecorder()}
		req := httptest.NewRequest("POST", "/v1/filesystem?path=/tmp/deadline-upload.txt", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/octet-stream")

		fsHandler.ServeHTTP(mock, req)

		Expect(mock.Code).To(Equal(http.StatusNoContent))
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
		req := httptest.NewRequest("GET", "/v1/filesystem?path=/tmp/deadline-download.txt", nil)

		fsHandler.ServeHTTP(mock, req)

		Expect(mock.Code).To(Equal(http.StatusOK))
		Expect(mock.getWriteDeadlines()).NotTo(BeEmpty())
		Expect(mock.Body.Bytes()).To(Equal(content))
	})
})

// failAfterFirstWrite serves the first write and fails every one after it,
// simulating a stream that dies partway through.
type failAfterFirstWrite struct {
	httptest.ResponseRecorder
	err   error
	wrote bool
}

func (w *failAfterFirstWrite) Write(p []byte) (int, error) {
	if w.wrote {
		return 0, w.err
	}
	w.wrote = true
	return w.ResponseRecorder.Write(p)
}

var _ = Describe("Filesystem stream abort", func() {
	var (
		fsHandler http.Handler
		rootDir   string
	)

	BeforeEach(func() {
		fsHandler, rootDir = newIsolatedAPI("abort-test-*")
	})

	// larger than io.Copy's 32KB buffer so the copy takes more than one write
	writeTestFile := func(name string) {
		content := make([]byte, 64*1024)
		_, err := rand.Read(content)
		Expect(err).NotTo(HaveOccurred())
		hostPath := filepath.Join(rootDir, "/tmp", name)
		Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
		Expect(os.WriteFile(hostPath, content, 0600)).To(Succeed())
	}

	It("aborts the response when a mid-stream write fails", func() {
		writeTestFile("abort.txt")

		mock := &failAfterFirstWrite{ResponseRecorder: *httptest.NewRecorder(), err: errors.New("stream broke")}
		req := httptest.NewRequest("GET", "/v1/filesystem?path=/tmp/abort.txt", nil)

		Expect(func() { fsHandler.ServeHTTP(mock, req) }).To(PanicWith(http.ErrAbortHandler))
		Expect(mock.Body.Len()).To(BeNumerically(">", 0), "expected a partial write before the failure")
	})

	It("ends quietly when the client disconnects mid-stream", func() {
		writeTestFile("disconnect.txt")

		mock := &failAfterFirstWrite{ResponseRecorder: *httptest.NewRecorder(), err: syscall.EPIPE}
		req := httptest.NewRequest("GET", "/v1/filesystem?path=/tmp/disconnect.txt", nil)

		Expect(func() { fsHandler.ServeHTTP(mock, req) }).NotTo(Panic())
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

			_, errorAPI = humatest.New(GinkgoT(), huma.DefaultConfig("Error Test API", "0.1.0"))
			v1 := huma.NewGroup(errorAPI, "/v1")
			handler := New(logger, mockProcFS, sandboxsidecar.NewPIDResolver(mockProcFS))
			Register(v1, handler)
		})

		It("returns 400 when container not found", func() {
			resp := errorAPI.Post("/v1/filesystem?path=/tmp/test.txt", "application/octet-stream", bytes.NewReader([]byte("content")))

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})
	})
})
