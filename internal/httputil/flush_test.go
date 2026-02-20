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

package httputil

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHTTPUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "HTTPUtil Suite")
}

// mockFlusherWriter is an io.Writer + http.Flusher that records calls.
type mockFlusherWriter struct {
	mu         sync.Mutex
	written    []byte
	flushCount int
	writeErr   error // injected error for Write
}

func (m *mockFlusherWriter) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return 0, m.writeErr
	}
	m.written = append(m.written, p...)
	return len(p), nil
}

func (m *mockFlusherWriter) Flush() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.flushCount++
}

func (m *mockFlusherWriter) getFlushCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.flushCount
}

func (m *mockFlusherWriter) getWritten() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte{}, m.written...)
}

var _ = Describe("TimedFlushWriter", func() {
	It("flushes after the interval elapses", func() {
		mock := &mockFlusherWriter{}
		fw := NewTimedFlushWriter(mock, 50*time.Millisecond)
		defer fw.Stop()

		n, err := fw.Write([]byte("hello"))
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(5))

		// Flush should not have happened yet
		Expect(mock.getFlushCount()).To(Equal(0))

		// Wait for the timer to fire
		Eventually(mock.getFlushCount, 200*time.Millisecond, 10*time.Millisecond).Should(Equal(1))
	})

	It("coalesces multiple writes into one flush", func() {
		mock := &mockFlusherWriter{}
		fw := NewTimedFlushWriter(mock, 50*time.Millisecond)
		defer fw.Stop()

		for i := 0; i < 10; i++ {
			_, err := fw.Write([]byte("x"))
			Expect(err).NotTo(HaveOccurred())
		}

		// All 10 bytes written
		Expect(mock.getWritten()).To(Equal(bytes.Repeat([]byte("x"), 10)))

		// Wait for flush — should be exactly 1 (coalesced)
		Eventually(mock.getFlushCount, 200*time.Millisecond, 10*time.Millisecond).Should(Equal(1))
	})

	It("Stop performs a final flush of pending data", func() {
		mock := &mockFlusherWriter{}
		fw := NewTimedFlushWriter(mock, time.Hour) // very long interval
		defer fw.Stop()

		_, err := fw.Write([]byte("data"))
		Expect(err).NotTo(HaveOccurred())

		// Timer hasn't fired (1 hour interval)
		Expect(mock.getFlushCount()).To(Equal(0))

		// Stop should flush immediately
		fw.Stop()
		Expect(mock.getFlushCount()).To(Equal(1))
	})

	It("Stop is a no-op when nothing is pending", func() {
		mock := &mockFlusherWriter{}
		fw := NewTimedFlushWriter(mock, 50*time.Millisecond)

		// No writes, just stop
		fw.Stop()
		Expect(mock.getFlushCount()).To(Equal(0))
	})

	It("works with a writer that does not implement http.Flusher", func() {
		var buf bytes.Buffer
		fw := NewTimedFlushWriter(&buf, 50*time.Millisecond)
		defer fw.Stop()

		n, err := fw.Write([]byte("hello"))
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(5))
		Expect(buf.String()).To(Equal("hello"))

		// No panic, no flush calls — just a plain writer
	})

	It("schedules flush even on write error (matches stdlib)", func() {
		mock := &mockFlusherWriter{writeErr: errors.New("broken")}
		fw := NewTimedFlushWriter(mock, 50*time.Millisecond)
		defer fw.Stop()

		_, err := fw.Write([]byte("hello"))
		Expect(err).To(MatchError("broken"))

		// Flush is still scheduled (stdlib does not check write error)
		Eventually(mock.getFlushCount, 200*time.Millisecond, 10*time.Millisecond).Should(Equal(1))
	})

	It("resumes flushing after timer fires", func() {
		mock := &mockFlusherWriter{}
		fw := NewTimedFlushWriter(mock, 50*time.Millisecond)
		defer fw.Stop()

		// First write + flush cycle
		_, _ = fw.Write([]byte("a"))
		Eventually(mock.getFlushCount, 200*time.Millisecond, 10*time.Millisecond).Should(Equal(1))

		// Second write should schedule a new flush
		_, _ = fw.Write([]byte("b"))
		Eventually(mock.getFlushCount, 200*time.Millisecond, 10*time.Millisecond).Should(Equal(2))
	})
})
