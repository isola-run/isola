package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Filesystem", func() {
	Describe("POST /filesystem", func() {
		It("writes file with absolute path", func() {
			content := []byte("hello world")
			resp := doPost("/filesystem?path=/tmp/test.txt", content)
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body FilesystemWriteResponse
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
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body FilesystemWriteResponse
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
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.AbsolutePath).To(Equal("/deep/nested/dir/file.txt"))

			// Verify file was written
			hostPath := filepath.Join(testRootDir, "/deep/nested/dir/file.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("returns 400 when path is missing", func() {
			resp := doPost("/filesystem", []byte("some content"))
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))

			var body ErrorResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Message).To(Equal("path query parameter is required"))
		})

		It("includes container in response when specified", func() {
			content := []byte("container test")
			resp := doPost("/filesystem?path=/tmp/container-test.txt&container=main", content)
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Container).To(Equal("main"))
		})

		It("writes empty file", func() {
			resp := doPost("/filesystem?path=/tmp/empty.txt", []byte{})
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body FilesystemWriteResponse
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
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.AbsolutePath).To(Equal("/tmp/normalized.txt"))
		})

		It("succeeds with empty container name", func() {
			content := []byte("no container specified")
			resp := doPost("/filesystem?path=/tmp/no-container.txt", content)
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body FilesystemWriteResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Container).To(BeEmpty())
		})

		It("rejects path with null bytes", func() {
			content := []byte("malicious content")
			resp := doPost("/filesystem?path=/tmp/evil%00file.txt", content)
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))

			var body ErrorResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Message).To(Equal("path contains invalid characters"))
		})
	})
})
