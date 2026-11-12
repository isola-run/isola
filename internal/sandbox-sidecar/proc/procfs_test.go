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

package proc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isola-run/isola/internal/constants"
)

func TestProc(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proc Suite")
}

var _ = Describe("GetContainerName", func() {
	It("returns false when the env var is not present", func() {
		pid := os.Getpid()
		name, ok := GetContainerName(pid)
		Expect(ok).To(BeFalse())
		Expect(name).To(BeEmpty())
	})
})

var _ = Describe("containerNameFromEnviron", func() {
	marker := constants.IsolaContainerNameEnv + "=mybox"

	It("finds the marker in a simple environ", func() {
		environ := []byte("PATH=/usr/bin\x00" + marker + "\x00")
		name, ok := containerNameFromEnviron(environ)
		Expect(ok).To(BeTrue())
		Expect(name).To(Equal("mybox"))
	})

	It("finds the marker after an env value larger than 64 KiB", func() {
		huge := strings.Repeat("x", 128*1024)
		environ := []byte("BIG=" + huge + "\x00" + marker + "\x00")
		name, ok := containerNameFromEnviron(environ)
		Expect(ok).To(BeTrue())
		Expect(name).To(Equal("mybox"))
	})

	It("returns false when the marker is absent", func() {
		environ := []byte("PATH=/usr/bin\x00HOME=/root\x00")
		name, ok := containerNameFromEnviron(environ)
		Expect(ok).To(BeFalse())
		Expect(name).To(BeEmpty())
	})
})

var _ = Describe("RealProcFS", func() {
	var procfs *RealProcFS

	BeforeEach(func() {
		procfs = &RealProcFS{}
	})

	Describe("GetCwd", func() {
		It("reads the cwd of the current process", func() {
			pid := os.Getpid()
			cwd, err := procfs.GetCwd(pid)
			Expect(err).NotTo(HaveOccurred())

			expected, err := os.Getwd()
			Expect(err).NotTo(HaveOccurred())

			expectedResolved, err := filepath.EvalSymlinks(expected)
			Expect(err).NotTo(HaveOccurred())
			cwdResolved, err := filepath.EvalSymlinks(cwd)
			Expect(err).NotTo(HaveOccurred())

			Expect(cwdResolved).To(Equal(expectedResolved))
		})

		It("returns an error for a nonexistent PID", func() {
			_, err := procfs.GetCwd(999999999)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetUIDGID", func() {
		It("reads the UID and GID of the current process", func() {
			pid := os.Getpid()
			uid, gid, err := procfs.GetUIDGID(pid)
			Expect(err).NotTo(HaveOccurred())
			Expect(uid).To(Equal(os.Getuid()))
			Expect(gid).To(Equal(os.Getgid()))
		})

		It("returns an error for a nonexistent PID", func() {
			_, _, err := procfs.GetUIDGID(999999999)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetEnviron", func() {
		It("reads environ of the current process", func() {
			pid := os.Getpid()
			env, err := procfs.GetEnviron(pid)
			Expect(err).NotTo(HaveOccurred())
			Expect(env).NotTo(BeEmpty())

			found := false
			for _, e := range env {
				if len(e) > 5 && e[:5] == "PATH=" {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "expected PATH in environ")
		})

		It("returns an error for a nonexistent PID", func() {
			_, err := procfs.GetEnviron(999999999)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("FindMarkedPID", func() {
		It("returns ErrContainerNotFound when no process has the marker", func() {
			_, err := procfs.FindMarkedPID("nonexistent-container")
			Expect(err).To(MatchError(ErrContainerNotFound))
		})

		It("resolves the default container when one name spans multiple processes", func() {
			const name = "isola-test-multiproc"
			marker := append(os.Environ(), constants.IsolaContainerNameEnv+"="+name)

			start := func() *exec.Cmd {
				cmd := exec.Command("sleep", "30")
				cmd.Env = marker
				Expect(cmd.Start()).To(Succeed())
				return cmd
			}

			c1 := start()
			defer func() { _ = c1.Process.Kill(); _ = c1.Wait() }()
			c2 := start()
			defer func() { _ = c2.Process.Kill(); _ = c2.Wait() }()

			pid, err := procfs.FindMarkedPID("")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(BeElementOf(c1.Process.Pid, c2.Process.Pid))
		})

		It("stays ambiguous for the default lookup when distinct container names exist", func() {
			start := func(name string) *exec.Cmd {
				cmd := exec.Command("sleep", "30")
				cmd.Env = append(os.Environ(), constants.IsolaContainerNameEnv+"="+name)
				Expect(cmd.Start()).To(Succeed())
				return cmd
			}

			c1 := start("isola-test-alpha")
			defer func() { _ = c1.Process.Kill(); _ = c1.Wait() }()
			c2 := start("isola-test-beta")
			defer func() { _ = c2.Process.Kill(); _ = c2.Wait() }()

			_, err := procfs.FindMarkedPID("")
			Expect(err).To(MatchError(ErrContainerNotFound))
		})
	})
})
