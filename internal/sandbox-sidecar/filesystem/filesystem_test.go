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
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sandboxsidecar "github.com/isola-ai/isola/internal/sandbox-sidecar"
	"github.com/isola-ai/isola/internal/sandbox-sidecar/proc"
	sidecarapi "github.com/isola-ai/isola/internal/sidecar-api"
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

	Describe("GET /filesystem/list", func() {
		It("lists directory contents", func() {
			dir := filepath.Join(testRootDir, "/tmp/listdir")
			Expect(os.MkdirAll(dir, 0750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaa"), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bb"), 0600)).To(Succeed())
			Expect(os.Mkdir(filepath.Join(dir, "subdir"), 0750)).To(Succeed())

			resp := doGet("/filesystem/list?path=/tmp/listdir")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.FilesystemListResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Entries).To(HaveLen(3))

			names := make([]string, len(body.Entries))
			for i, e := range body.Entries {
				names[i] = e.Name
			}
			Expect(names).To(ContainElements("a.txt", "b.txt", "subdir"))
		})

		It("returns entries with correct metadata", func() {
			dir := filepath.Join(testRootDir, "/tmp/listmeta")
			Expect(os.MkdirAll(dir, 0750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0600)).To(Succeed())
			Expect(os.Mkdir(filepath.Join(dir, "dir"), 0750)).To(Succeed())

			resp := doGet("/filesystem/list?path=/tmp/listmeta")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.FilesystemListResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())

			for _, e := range body.Entries {
				if e.Name == "file.txt" {
					Expect(e.IsDir).To(BeFalse())
					Expect(e.Size).To(Equal(int64(5)))
					Expect(e.Path).To(Equal("/tmp/listmeta/file.txt"))
				}
				if e.Name == "dir" {
					Expect(e.IsDir).To(BeTrue())
					Expect(e.Path).To(Equal("/tmp/listmeta/dir"))
				}
			}
		})

		It("returns empty list for empty directory", func() {
			dir := filepath.Join(testRootDir, "/tmp/emptydir")
			Expect(os.MkdirAll(dir, 0750)).To(Succeed())

			resp := doGet("/filesystem/list?path=/tmp/emptydir")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.FilesystemListResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Entries).To(BeEmpty())
		})

		It("returns 404 for nonexistent directory", func() {
			resp := doGet("/filesystem/list?path=/tmp/nonexistent-dir")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 400 when path is a file", func() {
			hostPath := filepath.Join(testRootDir, "/tmp/list-not-dir.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("content"), 0600)).To(Succeed())

			resp := doGet("/filesystem/list?path=/tmp/list-not-dir.txt")
			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("supports relative paths", func() {
			dir := filepath.Join(testRootDir, testCwd, "rellistdir")
			Expect(os.MkdirAll(dir, 0750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "rel.txt"), []byte("r"), 0600)).To(Succeed())

			resp := doGet("/filesystem/list?path=rellistdir")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.FilesystemListResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Entries).To(HaveLen(1))
			Expect(body.Entries[0].Name).To(Equal("rel.txt"))
		})
	})

	Describe("GET /filesystem/stat", func() {
		It("returns info for a regular file", func() {
			hostPath := filepath.Join(testRootDir, "/tmp/statfile.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("stat content"), 0600)).To(Succeed())

			resp := doGet("/filesystem/stat?path=/tmp/statfile.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.FileInfo
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Name).To(Equal("statfile.txt"))
			Expect(body.Path).To(Equal("/tmp/statfile.txt"))
			Expect(body.IsDir).To(BeFalse())
			Expect(body.Size).To(Equal(int64(12)))
		})

		It("returns info for a directory", func() {
			dir := filepath.Join(testRootDir, "/tmp/statdir")
			Expect(os.MkdirAll(dir, 0750)).To(Succeed())

			resp := doGet("/filesystem/stat?path=/tmp/statdir")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.FileInfo
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Name).To(Equal("statdir"))
			Expect(body.IsDir).To(BeTrue())
		})

		It("returns 404 for nonexistent path", func() {
			resp := doGet("/filesystem/stat?path=/tmp/nonexistent-stat")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("POST /filesystem/mkdir", func() {
		It("creates a new directory", func() {
			resp := doPost("/filesystem/mkdir?path=/tmp/newmkdir", nil)

			Expect(resp.Code).To(Equal(http.StatusCreated))
			var body sidecarapi.FilesystemMkdirResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.AbsolutePath).To(Equal("/tmp/newmkdir"))

			hostPath := filepath.Join(testRootDir, "/tmp/newmkdir")
			info, err := os.Stat(hostPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())
		})

		It("creates nested directories", func() {
			resp := doPost("/filesystem/mkdir?path=/tmp/nested/deep/dir", nil)

			Expect(resp.Code).To(Equal(http.StatusCreated))

			hostPath := filepath.Join(testRootDir, "/tmp/nested/deep/dir")
			info, err := os.Stat(hostPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())
		})

		It("succeeds if directory already exists", func() {
			dir := filepath.Join(testRootDir, "/tmp/existing-mkdir")
			Expect(os.MkdirAll(dir, 0750)).To(Succeed())

			resp := doPost("/filesystem/mkdir?path=/tmp/existing-mkdir", nil)
			Expect(resp.Code).To(Equal(http.StatusCreated))
		})
	})

	Describe("POST /filesystem/rename", func() {
		It("renames a file", func() {
			hostPath := filepath.Join(testRootDir, "/tmp/rename-src.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("rename me"), 0600)).To(Succeed())

			resp := doPost("/filesystem/rename?path=/tmp/rename-src.txt&newPath=/tmp/rename-dst.txt", nil)

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.FilesystemRenameResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.AbsolutePath).To(Equal("/tmp/rename-dst.txt"))

			// Old path should not exist
			_, err := os.Stat(hostPath)
			Expect(os.IsNotExist(err)).To(BeTrue())

			// New path should exist
			dstPath := filepath.Join(testRootDir, "/tmp/rename-dst.txt")
			content, err := os.ReadFile(dstPath) //nolint:gosec
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal([]byte("rename me")))
		})

		It("moves a file to a new directory", func() {
			srcPath := filepath.Join(testRootDir, "/tmp/move-file.txt")
			Expect(os.MkdirAll(filepath.Dir(srcPath), 0750)).To(Succeed())
			Expect(os.WriteFile(srcPath, []byte("move me"), 0600)).To(Succeed())

			resp := doPost("/filesystem/rename?path=/tmp/move-file.txt&newPath=/tmp/move-dest/file.txt", nil)

			Expect(resp.Code).To(Equal(http.StatusOK))

			dstPath := filepath.Join(testRootDir, "/tmp/move-dest/file.txt")
			content, err := os.ReadFile(dstPath) //nolint:gosec
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal([]byte("move me")))
		})

		It("returns 404 for nonexistent source", func() {
			resp := doPost("/filesystem/rename?path=/tmp/nonexistent-rename.txt&newPath=/tmp/dest.txt", nil)
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("DELETE /filesystem", func() {
		It("removes a file", func() {
			hostPath := filepath.Join(testRootDir, "/tmp/remove-file.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("remove me"), 0600)).To(Succeed())

			resp := testAPI.Delete("/filesystem?path=/tmp/remove-file.txt")

			Expect(resp.Code).To(Equal(http.StatusNoContent))

			_, err := os.Stat(hostPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("removes a directory recursively", func() {
			dir := filepath.Join(testRootDir, "/tmp/remove-dir")
			Expect(os.MkdirAll(filepath.Join(dir, "subdir"), 0750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0600)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dir, "subdir/nested.txt"), []byte("nested"), 0600)).To(Succeed())

			resp := testAPI.Delete("/filesystem?path=/tmp/remove-dir")

			Expect(resp.Code).To(Equal(http.StatusNoContent))

			_, err := os.Stat(dir)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("returns 404 for nonexistent path", func() {
			resp := testAPI.Delete("/filesystem?path=/tmp/nonexistent-remove")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
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
