package handlers

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isola-ai/isola-sb/internal/sandbox-sidecar/proc"
)

// dynamicProcMock simulates container processes that can crash and restart with new PIDs.
type dynamicProcMock struct {
	mu       sync.Mutex
	livePIDs map[int]string // PID -> container name for live processes

	rootDir string
	cwd     string
	uid     int
	gid     int

	// findMarkedCalls tracks the number of FindMarkedPID invocations.
	findMarkedCalls atomic.Int32
}

func newDynamicProcMock() *dynamicProcMock {
	return &dynamicProcMock{
		livePIDs: make(map[int]string),
		rootDir:  "/tmp/mock-root",
		cwd:      "/workspace",
	}
}

func (m *dynamicProcMock) addPID(pid int, containerName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.livePIDs[pid] = containerName
}

func (m *dynamicProcMock) removePID(pid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.livePIDs, pid)
}

func (m *dynamicProcMock) FindMarkedPID(containerName string) (int, error) {
	m.findMarkedCalls.Add(1)
	m.mu.Lock()
	defer m.mu.Unlock()

	if containerName != "" {
		for pid, name := range m.livePIDs {
			if name == containerName {
				return pid, nil
			}
		}
		return 0, proc.ErrContainerNotFound
	}

	// No container name: need exactly one
	if len(m.livePIDs) == 1 {
		for pid := range m.livePIDs {
			return pid, nil
		}
	}
	return 0, proc.ErrContainerNotFound
}

func (m *dynamicProcMock) GetContainerName(pid int) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name, ok := m.livePIDs[pid]
	return name, ok
}

func (m *dynamicProcMock) GetCwd(pid int) (string, error) { return m.cwd, nil }
func (m *dynamicProcMock) GetRoot(pid int) string          { return m.rootDir }
func (m *dynamicProcMock) GetUIDGID(pid int) (int, int, error) {
	return m.uid, m.gid, nil
}
func (m *dynamicProcMock) GetEnviron(pid int) ([]string, error) {
	return []string{"PATH=/usr/bin:/bin"}, nil
}

var _ = Describe("PIDResolver", func() {
	var (
		mock     *dynamicProcMock
		resolver *PIDResolver
	)

	BeforeEach(func() {
		mock = newDynamicProcMock()
		resolver = NewPIDResolver(mock)
		resolver.retryDelay = 5 * time.Millisecond // fast retries for tests
	})

	Describe("FindCachedContainerPID", func() {
		It("returns PID from fresh scan on first call", func() {
			mock.addPID(100, "main")

			pid, err := resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(100))
		})

		It("returns cached PID when still valid", func() {
			mock.addPID(100, "main")

			// First call populates cache
			pid, err := resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(100))

			callsBefore := mock.findMarkedCalls.Load()

			// Second call should use cache (validated via GetContainerName)
			pid, err = resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(100))

			// FindMarkedPID should not have been called again
			Expect(mock.findMarkedCalls.Load()).To(Equal(callsBefore))
		})

		It("detects stale PID and finds new one after container restart", func() {
			mock.addPID(100, "main")

			// First call caches PID 100
			pid, err := resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(100))

			// Simulate restart: PID 100 gone, PID 200 appears
			mock.removePID(100)
			mock.addPID(200, "main")

			// Should detect stale cache, scan again, find PID 200
			pid, err = resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(200))
		})

		It("retries when container is temporarily down after crash", func() {
			mock.addPID(100, "main")

			// Cache PID 100
			pid, err := resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(100))

			// Container crashes
			mock.removePID(100)

			// Container restarts with new PID after a brief delay
			go func() {
				time.Sleep(30 * time.Millisecond)
				mock.addPID(200, "main")
			}()

			// Should retry and eventually find PID 200
			pid, err = resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(200))
		})

		It("returns ErrContainerNotRunning when container stays down after retries", func() {
			mock.addPID(100, "main")
			resolver.maxRetries = 3

			// Cache PID 100
			pid, err := resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(100))

			// Container crashes and doesn't come back
			mock.removePID(100)

			_, err = resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).To(MatchError(ErrContainerNotRunning))
		})

		It("returns ErrContainerNotFound immediately when container was never seen", func() {
			// No PID in cache, no PID in proc — should fail immediately without retries
			callsBefore := mock.findMarkedCalls.Load()

			_, err := resolver.FindCachedContainerPID(context.Background(), "nonexistent")
			Expect(err).To(MatchError(proc.ErrContainerNotFound))

			// Should have called FindMarkedPID exactly once (no retries)
			Expect(mock.findMarkedCalls.Load()).To(Equal(callsBefore + 1))
		})

		It("respects context cancellation during retry", func() {
			mock.addPID(100, "main")
			resolver.maxRetries = 100 // many retries so it doesn't exhaust before cancel

			// Cache PID 100
			_, err := resolver.FindCachedContainerPID(context.Background(), "main")
			Expect(err).NotTo(HaveOccurred())

			// Container crashes
			mock.removePID(100)

			ctx, cancel := context.WithCancel(context.Background())
			// Cancel quickly
			go func() {
				time.Sleep(20 * time.Millisecond)
				cancel()
			}()

			_, err = resolver.FindCachedContainerPID(ctx, "main")
			Expect(err).To(MatchError(context.Canceled))
		})

		It("works with empty container name (single container)", func() {
			mock.addPID(100, "main")

			pid, err := resolver.FindCachedContainerPID(context.Background(), "")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(100))

			// Restart with new PID
			mock.removePID(100)
			mock.addPID(200, "main")

			pid, err = resolver.FindCachedContainerPID(context.Background(), "")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(200))
		})

		It("retries with empty container name after crash", func() {
			mock.addPID(100, "main")

			// Cache
			_, err := resolver.FindCachedContainerPID(context.Background(), "")
			Expect(err).NotTo(HaveOccurred())

			// Crash
			mock.removePID(100)

			// Restart after delay
			go func() {
				time.Sleep(30 * time.Millisecond)
				mock.addPID(200, "main")
			}()

			pid, err := resolver.FindCachedContainerPID(context.Background(), "")
			Expect(err).NotTo(HaveOccurred())
			Expect(pid).To(Equal(200))
		})
	})
})
