package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Upload", func() {
	Describe("POST /files/upload", func() {
		It("uploads file with absolute path", func() {
			content := []byte("hello world")
			resp := doPost("/files/upload?path=/tmp/test.txt", content)
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body UploadResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Path).To(Equal("/tmp/test.txt"))
			Expect(body.BytesWritten).To(Equal(int64(len(content))))

			// Verify file was written
			hostPath := filepath.Join(testRootDir, "/tmp/test.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("uploads file with relative path", func() {
			content := []byte("relative file content")
			resp := doPost("/files/upload?path=myfile.txt", content)
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body UploadResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Path).To(Equal("/workspace/myfile.txt"))
			Expect(body.BytesWritten).To(Equal(int64(len(content))))

			// Verify file was written
			hostPath := filepath.Join(testRootDir, "/workspace/myfile.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("creates parent directories", func() {
			content := []byte("nested file")
			resp := doPost("/files/upload?path=/deep/nested/dir/file.txt", content)
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body UploadResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Path).To(Equal("/deep/nested/dir/file.txt"))

			// Verify file was written
			hostPath := filepath.Join(testRootDir, "/deep/nested/dir/file.txt")
			written, err := os.ReadFile(hostPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(written).To(Equal(content))
		})

		It("returns 400 when path is missing", func() {
			resp := doPost("/files/upload", []byte("some content"))
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusBadRequest))

			var body ErrorResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Message).To(Equal("path query parameter is required"))
		})

		It("includes container in response when specified", func() {
			content := []byte("container test")
			resp := doPost("/files/upload?path=/tmp/container-test.txt&container=main", content)
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body UploadResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Container).To(Equal("main"))
		})

		It("handles empty file upload", func() {
			resp := doPost("/files/upload?path=/tmp/empty.txt", []byte{})
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body UploadResponse
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
			resp := doPost("/files/upload?path=/tmp/../tmp/./normalized.txt", content)
			defer func() { _ = resp.Body.Close() }()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))

			var body UploadResponse
			err := json.NewDecoder(resp.Body).Decode(&body)
			Expect(err).NotTo(HaveOccurred())
			Expect(body.Path).To(Equal("/tmp/normalized.txt"))
		})
	})
})
