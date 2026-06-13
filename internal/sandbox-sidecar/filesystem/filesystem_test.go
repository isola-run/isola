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

	sandboxsidecar "github.com/isola-run/isola/internal/sandbox-sidecar"
	"github.com/isola-run/isola/internal/sandbox-sidecar/proc"
	sidecarapi "github.com/isola-run/isola/internal/sidecar-api"
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

		It("returns 409 when a parent path component is a file", func() {
			hostPath := filepath.Join(testRootDir, "/tmp/parent-is-file.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("x"), 0600)).To(Succeed())

			resp := doPost("/v1/filesystem?path=/tmp/parent-is-file.txt/nested.txt", []byte("y"))

			Expect(resp.Code).To(Equal(http.StatusConflict))
		})
	})

	Describe("GET /filesystem/entries", func() {
		It("lists directory entries with metadata", func() {
			base := filepath.Join(testRootDir, "/listdir")
			Expect(os.MkdirAll(filepath.Join(base, "subdir"), 0750)).To(Succeed())
			filePath := filepath.Join(base, "file.txt")
			Expect(os.WriteFile(filePath, []byte("12345"), 0600)).To(Succeed())
			Expect(os.Chmod(filePath, 0644)).To(Succeed()) //nolint:gosec // exact mode asserted below
			Expect(os.Symlink("/listdir/file.txt", filepath.Join(base, "link.txt"))).To(Succeed())

			resp := doGet("/v1/filesystem/entries?path=/listdir")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.ListFilesystemEntriesResponse
			Expect(json.Unmarshal(resp.Body.Bytes(), &body)).To(Succeed())
			Expect(body.Entries).To(HaveLen(3))

			file := body.Entries[0]
			Expect(file.Name).To(Equal("file.txt"))
			Expect(file.Path).To(Equal("/listdir/file.txt"))
			Expect(file.Type).To(Equal("file"))
			Expect(file.Size).To(Equal(int64(5)))
			Expect(file.Permissions).To(Equal("0644"))
			Expect(file.UID).To(Equal(os.Getuid()))
			Expect(file.GID).To(Equal(os.Getgid()))
			Expect(file.ModifiedTime).NotTo(BeZero())
			Expect(file.SymlinkTarget).To(BeEmpty())

			link := body.Entries[1]
			Expect(link.Name).To(Equal("link.txt"))
			Expect(link.Type).To(Equal("symlink"))
			Expect(link.SymlinkTarget).To(Equal("/listdir/file.txt"))

			dir := body.Entries[2]
			Expect(dir.Name).To(Equal("subdir"))
			Expect(dir.Type).To(Equal("directory"))
		})

		It("lists directory with relative path", func() {
			base := filepath.Join(testRootDir, testCwd, "rellistdir")
			Expect(os.MkdirAll(base, 0750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(base, "a.txt"), []byte("a"), 0600)).To(Succeed())

			resp := doGet("/v1/filesystem/entries?path=rellistdir")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.ListFilesystemEntriesResponse
			Expect(json.Unmarshal(resp.Body.Bytes(), &body)).To(Succeed())
			Expect(body.Entries).To(HaveLen(1))
			Expect(body.Entries[0].Path).To(Equal("/workspace/rellistdir/a.txt"))
		})

		It("returns empty entries for empty directory", func() {
			Expect(os.MkdirAll(filepath.Join(testRootDir, "/emptydir"), 0750)).To(Succeed())

			resp := doGet("/v1/filesystem/entries?path=/emptydir")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body sidecarapi.ListFilesystemEntriesResponse
			Expect(json.Unmarshal(resp.Body.Bytes(), &body)).To(Succeed())
			Expect(body.Entries).NotTo(BeNil())
			Expect(body.Entries).To(BeEmpty())
		})

		It("returns 404 for nonexistent directory", func() {
			resp := doGet("/v1/filesystem/entries?path=/no-such-dir")

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 400 for file path", func() {
			hostPath := filepath.Join(testRootDir, "/tmp/list-a-file.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("x"), 0600)).To(Succeed())

			resp := doGet("/v1/filesystem/entries?path=/tmp/list-a-file.txt")

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 422 when path is missing", func() {
			resp := doGet("/v1/filesystem/entries")

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})
	})

	Describe("GET /filesystem/stat", func() {
		It("stats a regular file", func() {
			hostPath := filepath.Join(testRootDir, "/statdir/stat-me.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("123"), 0600)).To(Succeed())
			Expect(os.Chmod(hostPath, 0640)).To(Succeed()) //nolint:gosec // exact mode asserted below

			resp := doGet("/v1/filesystem/stat?path=/statdir/stat-me.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var entry sidecarapi.FilesystemEntry
			Expect(json.Unmarshal(resp.Body.Bytes(), &entry)).To(Succeed())
			Expect(entry.Name).To(Equal("stat-me.txt"))
			Expect(entry.Path).To(Equal("/statdir/stat-me.txt"))
			Expect(entry.Type).To(Equal("file"))
			Expect(entry.Size).To(Equal(int64(3)))
			Expect(entry.Permissions).To(Equal("0640"))
		})

		It("stats a directory", func() {
			Expect(os.MkdirAll(filepath.Join(testRootDir, "/statdir/sub"), 0750)).To(Succeed())

			resp := doGet("/v1/filesystem/stat?path=/statdir/sub")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var entry sidecarapi.FilesystemEntry
			Expect(json.Unmarshal(resp.Body.Bytes(), &entry)).To(Succeed())
			Expect(entry.Type).To(Equal("directory"))
		})

		It("stats a symlink without following it", func() {
			Expect(os.MkdirAll(filepath.Join(testRootDir, "/statdir"), 0750)).To(Succeed())
			linkPath := filepath.Join(testRootDir, "/statdir/stat-link")
			Expect(os.Symlink("/statdir/stat-me.txt", linkPath)).To(Succeed())

			resp := doGet("/v1/filesystem/stat?path=/statdir/stat-link")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var entry sidecarapi.FilesystemEntry
			Expect(json.Unmarshal(resp.Body.Bytes(), &entry)).To(Succeed())
			Expect(entry.Type).To(Equal("symlink"))
			Expect(entry.SymlinkTarget).To(Equal("/statdir/stat-me.txt"))
		})

		It("stats with relative path", func() {
			hostPath := filepath.Join(testRootDir, testCwd, "relstat.txt")
			Expect(os.WriteFile(hostPath, []byte("r"), 0600)).To(Succeed())

			resp := doGet("/v1/filesystem/stat?path=relstat.txt")

			Expect(resp.Code).To(Equal(http.StatusOK))
			var entry sidecarapi.FilesystemEntry
			Expect(json.Unmarshal(resp.Body.Bytes(), &entry)).To(Succeed())
			Expect(entry.Path).To(Equal("/workspace/relstat.txt"))
		})

		It("returns 404 for nonexistent path", func() {
			resp := doGet("/v1/filesystem/stat?path=/no-such-path")

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("DELETE /filesystem", func() {
		It("deletes a file", func() {
			hostPath := filepath.Join(testRootDir, "/deldir/del-me.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("x"), 0600)).To(Succeed())

			resp := doDelete("/v1/filesystem?path=/deldir/del-me.txt")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			_, err := os.Lstat(hostPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("deletes an empty directory", func() {
			hostPath := filepath.Join(testRootDir, "/deldir/empty-sub")
			Expect(os.MkdirAll(hostPath, 0750)).To(Succeed())

			resp := doDelete("/v1/filesystem?path=/deldir/empty-sub")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			_, err := os.Lstat(hostPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("returns 400 for non-empty directory without recursive", func() {
			hostPath := filepath.Join(testRootDir, "/deldir/full-sub")
			Expect(os.MkdirAll(hostPath, 0750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(hostPath, "f.txt"), []byte("x"), 0600)).To(Succeed())

			resp := doDelete("/v1/filesystem?path=/deldir/full-sub")

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("deletes a non-empty directory with recursive=true", func() {
			hostPath := filepath.Join(testRootDir, "/deldir/rec-sub")
			Expect(os.MkdirAll(filepath.Join(hostPath, "nested"), 0750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(hostPath, "nested/f.txt"), []byte("x"), 0600)).To(Succeed())

			resp := doDelete("/v1/filesystem?path=/deldir/rec-sub&recursive=true")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			_, err := os.Lstat(hostPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("deletes a dangling symlink", func() {
			Expect(os.MkdirAll(filepath.Join(testRootDir, "/deldir"), 0750)).To(Succeed())
			linkPath := filepath.Join(testRootDir, "/deldir/dangling")
			Expect(os.Symlink("/nonexistent/target", linkPath)).To(Succeed())

			resp := doDelete("/v1/filesystem?path=/deldir/dangling")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			_, err := os.Lstat(linkPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("returns 404 for nonexistent path", func() {
			resp := doDelete("/v1/filesystem?path=/no-such-path")

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("refuses to delete the filesystem root", func() {
			resp := doDelete("/v1/filesystem?path=/")

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("POST /filesystem/directories", func() {
		It("creates nested directories", func() {
			resp := doPostNoBody("/v1/filesystem/directories?path=/mkdir/deeply/nested")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			info, err := os.Stat(filepath.Join(testRootDir, "/mkdir/deeply/nested"))
			Expect(err).NotTo(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())
		})

		It("is idempotent for existing directory", func() {
			Expect(os.MkdirAll(filepath.Join(testRootDir, "/mkdir/existing"), 0750)).To(Succeed())

			resp := doPostNoBody("/v1/filesystem/directories?path=/mkdir/existing")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
		})

		It("creates directory with relative path", func() {
			resp := doPostNoBody("/v1/filesystem/directories?path=relmkdir")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			info, err := os.Stat(filepath.Join(testRootDir, testCwd, "relmkdir"))
			Expect(err).NotTo(HaveOccurred())
			Expect(info.IsDir()).To(BeTrue())
		})

		It("returns 409 when path exists as a file", func() {
			hostPath := filepath.Join(testRootDir, "/mkdir/taken.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("x"), 0600)).To(Succeed())

			resp := doPostNoBody("/v1/filesystem/directories?path=/mkdir/taken.txt")

			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("returns 409 when a parent component is a file", func() {
			hostPath := filepath.Join(testRootDir, "/mkdir/blocker.txt")
			Expect(os.MkdirAll(filepath.Dir(hostPath), 0750)).To(Succeed())
			Expect(os.WriteFile(hostPath, []byte("x"), 0600)).To(Succeed())

			resp := doPostNoBody("/v1/filesystem/directories?path=/mkdir/blocker.txt/sub")

			Expect(resp.Code).To(Equal(http.StatusConflict))
		})
	})

	Describe("POST /filesystem/move", func() {
		It("renames a file", func() {
			srcPath := filepath.Join(testRootDir, "/movedir/src.txt")
			Expect(os.MkdirAll(filepath.Dir(srcPath), 0750)).To(Succeed())
			Expect(os.WriteFile(srcPath, []byte("move me"), 0600)).To(Succeed())

			resp := doMove("/movedir/src.txt", "/movedir/dst.txt")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			_, err := os.Lstat(srcPath)
			Expect(os.IsNotExist(err)).To(BeTrue())
			content, err := os.ReadFile(filepath.Join(testRootDir, "/movedir/dst.txt")) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal([]byte("move me")))
		})

		It("creates destination parent directories", func() {
			srcPath := filepath.Join(testRootDir, "/movedir/parented.txt")
			Expect(os.MkdirAll(filepath.Dir(srcPath), 0750)).To(Succeed())
			Expect(os.WriteFile(srcPath, []byte("x"), 0600)).To(Succeed())

			resp := doMove("/movedir/parented.txt", "/movedir/new/deep/dst.txt")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			_, err := os.Stat(filepath.Join(testRootDir, "/movedir/new/deep/dst.txt"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("moves a directory", func() {
			srcDir := filepath.Join(testRootDir, "/movedir/srcdir")
			Expect(os.MkdirAll(srcDir, 0750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(srcDir, "f.txt"), []byte("x"), 0600)).To(Succeed())

			resp := doMove("/movedir/srcdir", "/movedir/dstdir")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			_, err := os.Stat(filepath.Join(testRootDir, "/movedir/dstdir/f.txt"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("overwrites an existing destination file", func() {
			srcPath := filepath.Join(testRootDir, "/movedir/over-src.txt")
			dstPath := filepath.Join(testRootDir, "/movedir/over-dst.txt")
			Expect(os.MkdirAll(filepath.Dir(srcPath), 0750)).To(Succeed())
			Expect(os.WriteFile(srcPath, []byte("new"), 0600)).To(Succeed())
			Expect(os.WriteFile(dstPath, []byte("old"), 0600)).To(Succeed())

			resp := doMove("/movedir/over-src.txt", "/movedir/over-dst.txt")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			content, err := os.ReadFile(dstPath) //nolint:gosec // test file path
			Expect(err).NotTo(HaveOccurred())
			Expect(content).To(Equal([]byte("new")))
		})

		It("resolves relative source and destination paths", func() {
			srcPath := filepath.Join(testRootDir, testCwd, "relmove-src.txt")
			Expect(os.WriteFile(srcPath, []byte("rel"), 0600)).To(Succeed())

			resp := doMove("relmove-src.txt", "relmove-dst.txt")

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			_, err := os.Stat(filepath.Join(testRootDir, testCwd, "relmove-dst.txt"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("returns 404 for nonexistent source", func() {
			resp := doMove("/no-such-source", "/movedir/whatever")

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 409 when destination is a non-empty directory", func() {
			srcDir := filepath.Join(testRootDir, "/movedir/conflict-src")
			dstDir := filepath.Join(testRootDir, "/movedir/conflict-dst")
			Expect(os.MkdirAll(srcDir, 0750)).To(Succeed())
			Expect(os.MkdirAll(dstDir, 0750)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(dstDir, "occupied.txt"), []byte("x"), 0600)).To(Succeed())

			resp := doMove("/movedir/conflict-src", "/movedir/conflict-dst")

			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("returns 409 when moving a file onto a directory", func() {
			srcPath := filepath.Join(testRootDir, "/movedir/type-src.txt")
			dstDir := filepath.Join(testRootDir, "/movedir/type-dst")
			Expect(os.MkdirAll(dstDir, 0750)).To(Succeed())
			Expect(os.WriteFile(srcPath, []byte("x"), 0600)).To(Succeed())

			resp := doMove("/movedir/type-src.txt", "/movedir/type-dst")

			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("refuses to move the filesystem root", func() {
			resp := doMove("/", "/movedir/root-copy")

			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 422 when sourcePath is missing", func() {
			resp := testAPI.Post("/v1/filesystem/move", map[string]any{"destinationPath": "/somewhere"})

			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
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
		fsHandler, api = humatest.New(GinkgoT(), huma.DefaultConfig("Deadline Test API", "0.1.0"))
		v1 := huma.NewGroup(api, "/v1")
		h := New(logger, mockProcFS, sandboxsidecar.NewPIDResolver(mockProcFS))
		Register(v1, h)
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
