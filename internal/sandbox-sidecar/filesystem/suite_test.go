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
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sandboxsidecar "github.com/isola-run/isola/internal/sandbox-sidecar"
)

var (
	testAPI     humatest.TestAPI
	testRootDir string
	testCwd     string
)

// MockProcFS implements proc.ProcFS for testing.
type MockProcFS struct {
	rootDir       string
	cwd           string
	uid           int
	gid           int
	findMarkedErr error
	getEnvironErr error
}

func (m *MockProcFS) FindMarkedPID(containerName string) (int, error) {
	if m.findMarkedErr != nil {
		return 0, m.findMarkedErr
	}
	return 1, nil
}

func (m *MockProcFS) GetCwd(pid int) (string, error) {
	return m.cwd, nil
}

func (m *MockProcFS) GetRoot(pid int) string {
	return m.rootDir
}

func (m *MockProcFS) GetUIDGID(pid int) (int, int, error) {
	return m.uid, m.gid, nil
}

func (m *MockProcFS) GetEnviron(pid int) ([]string, error) {
	if m.getEnvironErr != nil {
		return nil, m.getEnvironErr
	}
	return []string{"PATH=/usr/bin:/bin", "HOME=/root"}, nil
}

func TestFilesystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sidecar Filesystem Suite")
}

var _ = BeforeSuite(func() {
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
		uid:     os.Getuid(),
		gid:     os.Getgid(),
	}

	_, testAPI = humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "0.1.0"))

	v1 := huma.NewGroup(testAPI, "/v1")
	pidResolver := sandboxsidecar.NewPIDResolver(mockProcFS)
	h := New(logger, mockProcFS, pidResolver)
	Register(v1, h)
})

func doPost(path string, body []byte) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	return testAPI.Post(path, "Content-Type: application/octet-stream", bodyReader)
}

func doGet(path string) *httptest.ResponseRecorder {
	return testAPI.Get(path)
}

func doDelete(path string) *httptest.ResponseRecorder {
	return testAPI.Delete(path)
}

func doPostNoBody(path string) *httptest.ResponseRecorder {
	return testAPI.Post(path)
}

func doMove(sourcePath, destinationPath string) *httptest.ResponseRecorder {
	return testAPI.Post("/v1/filesystem/move", map[string]any{
		"sourcePath":      sourcePath,
		"destinationPath": destinationPath,
	})
}
