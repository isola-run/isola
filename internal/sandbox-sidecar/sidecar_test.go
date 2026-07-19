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

package sandboxsidecar_test

import (
	"context"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isola-run/isola/internal/constants"
	sandboxsidecar "github.com/isola-run/isola/internal/sandbox-sidecar"
	"github.com/isola-run/isola/internal/sandbox-sidecar/proc"
)

func TestSidecar(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sidecar Suite")
}

// markedResult is one scripted return value for scriptedProcFS.FindMarkedPID.
type markedResult struct {
	pid int
	err error
}

// scriptedProcFS implements proc.ProcFS. FindMarkedPID replays a scripted
// sequence of results (falling back to the last one), letting a test drive the
// resolver through a changing /proc without touching the real filesystem. The
// remaining methods are unused stubs.
type scriptedProcFS struct {
	markedCalls   int
	markedResults []markedResult
}

func (m *scriptedProcFS) FindMarkedPID(string) (int, error) {
	i := m.markedCalls
	m.markedCalls++
	if i >= len(m.markedResults) {
		i = len(m.markedResults) - 1
	}
	return m.markedResults[i].pid, m.markedResults[i].err
}

func (m *scriptedProcFS) GetCwd(int) (string, error)       { return "", nil }
func (m *scriptedProcFS) GetRoot(int) string               { return "" }
func (m *scriptedProcFS) GetUIDGID(int) (int, int, error)  { return 0, 0, nil }
func (m *scriptedProcFS) GetEnviron(int) ([]string, error) { return nil, nil }

// markedProcess starts a real, long-lived child process tagged with
// ISOLA_CONTAINER_NAME so that proc.GetContainerName resolves its PID to name.
// The resolver's cache-hit validation reads the real /proc, so exercising it
// requires a genuine marked process rather than a fabricated PID.
func markedProcess(name string) int {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "sleep", "300")
	cmd.Env = []string{constants.IsolaContainerNameEnv + "=" + name}
	Expect(cmd.Start()).To(Succeed())
	DeferCleanup(func() {
		cancel()
		_, _ = cmd.Process.Wait()
	})
	return cmd.Process.Pid
}

var _ = Describe("PIDResolver", func() {
	Describe("FindCachedContainerPID", func() {
		It("re-resolves the empty name so a second container makes it ambiguous", func() {
			// One marked container is up; a later scan would see two and must
			// fail rather than keep serving the first from a stale cache entry.
			pid := markedProcess("c1")
			procFS := &scriptedProcFS{markedResults: []markedResult{
				{pid: pid, err: nil},
				{pid: 0, err: proc.ErrContainerNotFound},
			}}
			resolver := sandboxsidecar.NewPIDResolver(procFS)

			got, err := resolver.FindCachedContainerPID("")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(pid))

			_, err = resolver.FindCachedContainerPID("")
			Expect(err).To(MatchError(proc.ErrContainerNotFound))
		})

		It("caches a named container so repeated lookups skip the scan", func() {
			pid := markedProcess("c1")
			procFS := &scriptedProcFS{markedResults: []markedResult{{pid: pid, err: nil}}}
			resolver := sandboxsidecar.NewPIDResolver(procFS)

			first, err := resolver.FindCachedContainerPID("c1")
			Expect(err).NotTo(HaveOccurred())
			Expect(first).To(Equal(pid))

			second, err := resolver.FindCachedContainerPID("c1")
			Expect(err).NotTo(HaveOccurred())
			Expect(second).To(Equal(pid))

			Expect(procFS.markedCalls).To(Equal(1))
		})
	})
})
