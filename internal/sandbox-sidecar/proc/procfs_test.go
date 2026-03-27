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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isola-ai/isola/internal/constants"
)

func TestProc(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Proc Suite")
}

var _ = Describe("splitOnNullBytes", func() {
	It("returns nil for empty data at EOF", func() {
		advance, token, err := splitOnNullBytes([]byte{}, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(advance).To(Equal(0))
		Expect(token).To(BeNil())
	})

	It("returns nil for empty data not at EOF", func() {
		advance, token, err := splitOnNullBytes([]byte{}, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(advance).To(Equal(0))
		Expect(token).To(BeNil())
	})

	It("splits on null byte", func() {
		data := []byte("FOO=bar\x00BAZ=qux\x00")
		advance, token, err := splitOnNullBytes(data, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(advance).To(Equal(8)) // len("FOO=bar\x00")
		Expect(string(token)).To(Equal("FOO=bar"))
	})

	It("returns remaining data at EOF without null terminator", func() {
		data := []byte("FOO=bar")
		advance, token, err := splitOnNullBytes(data, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(advance).To(Equal(7))
		Expect(string(token)).To(Equal("FOO=bar"))
	})

	It("requests more data when no null byte and not at EOF", func() {
		data := []byte("FOO=bar")
		advance, token, err := splitOnNullBytes(data, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(advance).To(Equal(0))
		Expect(token).To(BeNil())
	})

	It("handles a single null byte", func() {
		data := []byte("\x00")
		advance, token, err := splitOnNullBytes(data, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(advance).To(Equal(1))
		Expect(string(token)).To(Equal(""))
	})

	It("handles multiple sequential scans", func() {
		data := []byte("A=1\x00B=2\x00C=3")

		advance1, token1, err := splitOnNullBytes(data, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(token1)).To(Equal("A=1"))
		Expect(advance1).To(Equal(4))

		advance2, token2, err := splitOnNullBytes(data[advance1:], false)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(token2)).To(Equal("B=2"))
		Expect(advance2).To(Equal(4))

		advance3, token3, err := splitOnNullBytes(data[advance1+advance2:], true)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(token3)).To(Equal("C=3"))
		Expect(advance3).To(Equal(3))
	})
})

var _ = Describe("GetContainerName", func() {
	It("returns false for a nonexistent PID", func() {
		// PID 999999999 should not exist
		name, ok := GetContainerName(999999999)
		Expect(ok).To(BeFalse())
		Expect(name).To(BeEmpty())
	})

	It("returns false when the env var is not present", func() {
		// Current process should not have ISOLA_CONTAINER_NAME set
		pid := os.Getpid()
		name, ok := GetContainerName(pid)
		Expect(ok).To(BeFalse())
		Expect(name).To(BeEmpty())
	})
})

var _ = Describe("RealProcFS", func() {
	var procfs *RealProcFS

	BeforeEach(func() {
		procfs = &RealProcFS{}
	})

	Describe("GetRoot", func() {
		It("returns the correct /proc/<pid>/root path", func() {
			Expect(procfs.GetRoot(42)).To(Equal("/proc/42/root"))
		})

		It("returns the correct path for PID 1", func() {
			Expect(procfs.GetRoot(1)).To(Equal("/proc/1/root"))
		})

		It("returns the correct path for a large PID", func() {
			Expect(procfs.GetRoot(4194304)).To(Equal("/proc/4194304/root"))
		})
	})

	Describe("GetCwd", func() {
		It("reads the cwd of the current process", func() {
			pid := os.Getpid()
			cwd, err := procfs.GetCwd(pid)
			Expect(err).NotTo(HaveOccurred())

			// Should match our actual working directory
			expected, err := os.Getwd()
			Expect(err).NotTo(HaveOccurred())

			// Resolve symlinks for both since /proc/pid/cwd resolves symlinks
			expectedResolved, err := filepath.EvalSymlinks(expected)
			Expect(err).NotTo(HaveOccurred())
			cwdResolved, err := filepath.EvalSymlinks(cwd)
			Expect(err).NotTo(HaveOccurred())

			Expect(cwdResolved).To(Equal(expectedResolved))
		})

		It("returns an error for a nonexistent PID", func() {
			_, err := procfs.GetCwd(999999999)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("read cwd symlink"))
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
			Expect(err.Error()).To(ContainSubstring("stat /proc/999999999"))
		})
	})

	Describe("GetEnviron", func() {
		It("reads environ of the current process", func() {
			pid := os.Getpid()
			env, err := procfs.GetEnviron(pid)
			Expect(err).NotTo(HaveOccurred())
			Expect(env).NotTo(BeEmpty())

			// PATH should be in the environment
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
			Expect(err.Error()).To(ContainSubstring("read environ"))
		})
	})

	Describe("FindMarkedPID", func() {
		It("returns ErrContainerNotFound when no process has the marker", func() {
			_, err := procfs.FindMarkedPID("nonexistent-container")
			Expect(err).To(MatchError(ErrContainerNotFound))
		})

		It("returns ErrContainerNotFound with empty name when no containers exist", func() {
			// No process should have ISOLA_CONTAINER_NAME set in tests
			_, err := procfs.FindMarkedPID("")
			Expect(err).To(MatchError(ErrContainerNotFound))
		})
	})
})

// TestGetContainerNameWithFakeProc creates a fake /proc-like directory structure
// to test GetContainerName more thoroughly. Since GetContainerName hardcodes
// /proc/<pid>/environ, we cannot redirect it. Instead we test the underlying
// scanning logic via splitOnNullBytes and test GetContainerName behavior
// on the real /proc with known conditions.
var _ = Describe("environ file parsing", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "procfs-test-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(tmpDir) })
	})

	// Test the environ file format by writing and reading back through
	// the same null-byte parsing logic used by GetContainerName and GetEnviron.
	It("correctly parses environ format with ISOLA_CONTAINER_NAME", func() {
		envContent := fmt.Sprintf("PATH=/usr/bin\x00HOME=/root\x00%s=my-container\x00TERM=xterm\x00",
			constants.IsolaContainerNameEnv)

		environPath := filepath.Join(tmpDir, "environ")
		err := os.WriteFile(environPath, []byte(envContent), 0600)
		Expect(err).NotTo(HaveOccurred())

		// Read back and parse like GetEnviron does
		data, err := os.ReadFile(environPath)
		Expect(err).NotTo(HaveOccurred())

		// Parse using the same split function
		var entries []string
		remaining := data
		for len(remaining) > 0 {
			advance, token, splitErr := splitOnNullBytes(remaining, len(remaining) == len(data))
			Expect(splitErr).NotTo(HaveOccurred())
			if token != nil {
				entries = append(entries, string(token))
			}
			if advance == 0 {
				break
			}
			remaining = remaining[advance:]
		}

		Expect(entries).To(ContainElement("PATH=/usr/bin"))
		Expect(entries).To(ContainElement("HOME=/root"))
		Expect(entries).To(ContainElement(constants.IsolaContainerNameEnv + "=my-container"))
		Expect(entries).To(ContainElement("TERM=xterm"))
	})

	It("handles environ with no null terminator at end", func() {
		envContent := "KEY1=val1\x00KEY2=val2"

		environPath := filepath.Join(tmpDir, "environ")
		err := os.WriteFile(environPath, []byte(envContent), 0600)
		Expect(err).NotTo(HaveOccurred())

		data, err := os.ReadFile(environPath)
		Expect(err).NotTo(HaveOccurred())

		var entries []string
		remaining := data
		for len(remaining) > 0 {
			atEOF := true // simulate bufio.Scanner behavior at final chunk
			advance, token, splitErr := splitOnNullBytes(remaining, atEOF)
			Expect(splitErr).NotTo(HaveOccurred())
			if token != nil {
				entries = append(entries, string(token))
			}
			if advance == 0 {
				break
			}
			remaining = remaining[advance:]
		}

		Expect(entries).To(ConsistOf("KEY1=val1", "KEY2=val2"))
	})

	It("handles empty environ file", func() {
		environPath := filepath.Join(tmpDir, "environ")
		err := os.WriteFile(environPath, []byte{}, 0600)
		Expect(err).NotTo(HaveOccurred())

		data, err := os.ReadFile(environPath)
		Expect(err).NotTo(HaveOccurred())

		advance, token, splitErr := splitOnNullBytes(data, true)
		Expect(splitErr).NotTo(HaveOccurred())
		Expect(advance).To(Equal(0))
		Expect(token).To(BeNil())
	})
})

var _ = Describe("GetCwd with symlinks", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "procfs-symlink-test-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { _ = os.RemoveAll(tmpDir) })
	})

	It("resolves the cwd symlink correctly for the current process", func() {
		procfs := &RealProcFS{}
		pid := os.Getpid()

		// /proc/<pid>/cwd is a symlink to the process working directory
		expectedLink := fmt.Sprintf("/proc/%d/cwd", pid)
		expectedTarget, err := os.Readlink(expectedLink)
		Expect(err).NotTo(HaveOccurred())

		cwd, err := procfs.GetCwd(pid)
		Expect(err).NotTo(HaveOccurred())
		Expect(cwd).To(Equal(expectedTarget))
	})
})

var _ = Describe("FindMarkedPID edge cases", func() {
	It("scans /proc which includes non-numeric directories", func() {
		// This test verifies that FindMarkedPID gracefully skips non-numeric
		// entries in /proc (like /proc/self, /proc/meminfo, etc.)
		// It should not error, just return ErrContainerNotFound
		procfs := &RealProcFS{}
		_, err := procfs.FindMarkedPID("nonexistent")
		Expect(err).To(MatchError(ErrContainerNotFound))
	})
})

var _ = Describe("GetRoot path construction", func() {
	It("constructs path with filepath.Join semantics", func() {
		procfs := &RealProcFS{}

		// Verify the path format matches what the rest of the codebase expects
		pid := 12345
		root := procfs.GetRoot(pid)
		Expect(root).To(Equal("/proc/" + strconv.Itoa(pid) + "/root"))
	})
})
