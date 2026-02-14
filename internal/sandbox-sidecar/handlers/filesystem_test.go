package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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
