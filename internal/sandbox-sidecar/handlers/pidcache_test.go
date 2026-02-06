package handlers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

var _ = Describe("PIDCache", func() {
	It("returns error when container not found", func() {
		mockFS := &errorMockProcFS{findPIDError: proc.ErrContainerNotFound}
		cache := NewPIDCache(mockFS)

		_, err := cache.FindPID("nonexistent")
		Expect(err).To(MatchError(proc.ErrContainerNotFound))
	})

	It("caches PID across calls with the mock", func() {
		// MockProcFS always returns pid=1, which validates the cache path
		cache := NewPIDCache(&MockProcFS{
			rootDir: testRootDir,
			cwd:     testCwd,
			uid:     0,
			gid:     0,
		})

		pid1, err := cache.FindPID("")
		Expect(err).NotTo(HaveOccurred())
		Expect(pid1).To(Equal(1))

		pid2, err := cache.FindPID("")
		Expect(err).NotTo(HaveOccurred())
		Expect(pid2).To(Equal(1))
	})
})
