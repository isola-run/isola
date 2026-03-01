package httputil

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// deadlineCapture is an http.ResponseWriter that records SetWriteDeadline,
// SetReadDeadline, and Flush calls. http.ResponseController discovers the
// Set*Deadline methods via interface assertions on the ResponseWriter.
type deadlineCapture struct {
	httptest.ResponseRecorder
	mu             sync.Mutex
	writeDeadlines []time.Time
	readDeadlines  []time.Time
	flushCount     int
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
	d.flushCount++
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

func (d *deadlineCapture) getFlushCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.flushCount
}

// orderCaptureWriter records len(writeDeadlines) at each Write call,
// proving that the deadline was set before the inner Write executes.
type orderCaptureWriter struct {
	mock             *deadlineCapture
	deadlinesAtWrite []int
}

func (w *orderCaptureWriter) Write(p []byte) (int, error) {
	w.deadlinesAtWrite = append(w.deadlinesAtWrite, len(w.mock.getWriteDeadlines()))
	return w.mock.Write(p)
}

// orderCaptureReader records len(readDeadlines) and len(writeDeadlines) at
// each Read call, proving deadlines were set before the inner Read executes.
type orderCaptureReader struct {
	inner         io.Reader
	mock          *deadlineCapture
	readDLAtRead  []int
	writeDLAtRead []int
}

func (r *orderCaptureReader) Read(p []byte) (int, error) {
	r.readDLAtRead = append(r.readDLAtRead, len(r.mock.getReadDeadlines()))
	r.writeDLAtRead = append(r.writeDLAtRead, len(r.mock.getWriteDeadlines()))
	return r.inner.Read(p)
}

// errorReader always returns the given error.
type errorReader struct{ err error }

func (r *errorReader) Read([]byte) (int, error) { return 0, r.err }

// onlyReader hides io.WriterTo so io.Copy uses its 32KB buffer.
type onlyReader struct{ io.Reader }

// onlyWriter hides io.ReaderFrom so io.Copy uses its 32KB buffer.
type onlyWriter struct{ io.Writer }

var _ = Describe("DeadlineWriter", func() {
	It("sets write deadline before each Write", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		dw := NewDeadlineWriter(mock, rc, 10*time.Second)

		before := time.Now()
		n, err := dw.Write([]byte("hello"))
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(5))

		deadlines := mock.getWriteDeadlines()
		Expect(deadlines).To(HaveLen(1))
		// Deadline should be approximately now + 10s
		Expect(deadlines[0]).To(BeTemporally(">=", before.Add(10*time.Second), 100*time.Millisecond))
		Expect(deadlines[0]).To(BeTemporally("<=", before.Add(10*time.Second+200*time.Millisecond)))
	})

	It("Flush delegates to inner Flusher without clearing deadline", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		dw := NewDeadlineWriter(mock, rc, 10*time.Second).(*DeadlineWriter)

		dw.Flush()

		Expect(mock.getFlushCount()).To(Equal(1))
		// Flush should NOT set any write deadlines
		Expect(mock.getWriteDeadlines()).To(BeEmpty())
	})

	It("returns raw writer when rc is nil", func() {
		var buf bytes.Buffer
		w := NewDeadlineWriter(&buf, nil, 10*time.Second)
		// Should be the original buffer, not a DeadlineWriter
		Expect(w).To(BeIdenticalTo(&buf))
	})

	It("io.Copy extends write deadline on each chunk", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		dw := NewDeadlineWriter(mock, rc, 10*time.Second)

		// 96KB source → io.Copy uses 32KB buffer → 3 Write calls
		src := make([]byte, 96*1024)
		_, err := rand.Read(src)
		Expect(err).NotTo(HaveOccurred())

		// onlyReader strips WriterTo so io.Copy uses its 32KB buffer
		n, err := io.Copy(dw, &onlyReader{bytes.NewReader(src)})
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(int64(96 * 1024)))

		deadlines := mock.getWriteDeadlines()
		Expect(deadlines).To(HaveLen(3))
		for i := 1; i < len(deadlines); i++ {
			Expect(deadlines[i]).To(BeTemporally(">=", deadlines[i-1]))
		}
	})

	It("sets write deadline before delegating to inner writer", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		inner := &orderCaptureWriter{mock: mock}
		dw := NewDeadlineWriter(inner, rc, 10*time.Second)

		_, err := dw.Write([]byte("hello"))
		Expect(err).NotTo(HaveOccurred())

		// Inner writer should have seen 1 deadline already set when Write ran
		Expect(inner.deadlinesAtWrite).To(HaveLen(1))
		Expect(inner.deadlinesAtWrite[0]).To(Equal(1))
	})
})

var _ = Describe("DeadlineReader", func() {
	It("sets read and write deadlines before each Read", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		src := bytes.NewReader([]byte("hello"))
		dr := NewDeadlineReader(src, rc, 10*time.Second)

		before := time.Now()
		buf := make([]byte, 5)
		n, err := dr.Read(buf)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(5))
		Expect(string(buf)).To(Equal("hello"))

		readDeadlines := mock.getReadDeadlines()
		Expect(readDeadlines).To(HaveLen(1))
		Expect(readDeadlines[0]).To(BeTemporally(">=", before.Add(10*time.Second), 100*time.Millisecond))
		Expect(readDeadlines[0]).To(BeTemporally("<=", before.Add(10*time.Second+200*time.Millisecond)))

		writeDeadlines := mock.getWriteDeadlines()
		Expect(writeDeadlines).To(HaveLen(1))
		Expect(writeDeadlines[0]).To(BeTemporally(">=", before.Add(20*time.Second), 100*time.Millisecond))
		Expect(writeDeadlines[0]).To(BeTemporally("<=", before.Add(20*time.Second+200*time.Millisecond)))
	})

	It("returns raw reader when rc is nil", func() {
		src := bytes.NewReader([]byte("hello"))
		r := NewDeadlineReader(src, nil, 10*time.Second)
		Expect(r).To(BeIdenticalTo(src))
	})

	It("io.Copy extends both deadlines on each chunk", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)

		// 96KB source → io.Copy uses 32KB buffer → 3 data reads + 1 EOF read
		src := make([]byte, 96*1024)
		_, err := rand.Read(src)
		Expect(err).NotTo(HaveOccurred())

		dr := NewDeadlineReader(bytes.NewReader(src), rc, 10*time.Second)

		// onlyWriter strips ReaderFrom so io.Copy uses its 32KB buffer
		var buf bytes.Buffer
		n, err := io.Copy(&onlyWriter{&buf}, dr)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(int64(96 * 1024)))
		Expect(buf.Bytes()).To(Equal(src))

		readDeadlines := mock.getReadDeadlines()
		writeDeadlines := mock.getWriteDeadlines()
		// 3 data reads + 1 EOF read = 4
		Expect(readDeadlines).To(HaveLen(4))
		Expect(writeDeadlines).To(HaveLen(4))

		for i := 1; i < len(readDeadlines); i++ {
			Expect(readDeadlines[i]).To(BeTemporally(">=", readDeadlines[i-1]))
		}
		for i := 1; i < len(writeDeadlines); i++ {
			Expect(writeDeadlines[i]).To(BeTemporally(">=", writeDeadlines[i-1]))
		}
	})

	It("write deadline is exactly 2x read deadline from same time base", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		dr := NewDeadlineReader(bytes.NewReader([]byte("x")), rc, 7*time.Second)

		buf := make([]byte, 1)
		_, err := dr.Read(buf)
		Expect(err).NotTo(HaveOccurred())

		readDL := mock.getReadDeadlines()
		writeDL := mock.getWriteDeadlines()
		Expect(readDL).To(HaveLen(1))
		Expect(writeDL).To(HaveLen(1))
		Expect(writeDL[0].Sub(readDL[0])).To(Equal(7 * time.Second))
	})

	It("sets deadlines before delegating to inner reader", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		inner := &orderCaptureReader{inner: bytes.NewReader([]byte("hello")), mock: mock}
		dr := NewDeadlineReader(inner, rc, 10*time.Second)

		buf := make([]byte, 5)
		_, err := dr.Read(buf)
		Expect(err).NotTo(HaveOccurred())

		Expect(inner.readDLAtRead).To(HaveLen(1))
		Expect(inner.readDLAtRead[0]).To(Equal(1))
		Expect(inner.writeDLAtRead).To(HaveLen(1))
		Expect(inner.writeDLAtRead[0]).To(Equal(1))
	})

	It("sets deadlines even when inner reader returns an error", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		readErr := errors.New("read failed")
		dr := NewDeadlineReader(&errorReader{err: readErr}, rc, 10*time.Second)

		buf := make([]byte, 1)
		_, err := dr.Read(buf)
		Expect(err).To(MatchError(readErr))

		Expect(mock.getReadDeadlines()).To(HaveLen(1))
		Expect(mock.getWriteDeadlines()).To(HaveLen(1))
	})
})

var _ = Describe("DeadlineWriter + TimedFlushWriter composition", func() {
	It("write sets deadline, delayed flush does not clear it", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		dw := NewDeadlineWriter(mock, rc, 10*time.Second)
		fw := NewTimedFlushWriter(dw, 50*time.Millisecond)
		defer fw.Stop()

		_, err := fw.Write([]byte("data"))
		Expect(err).NotTo(HaveOccurred())

		// Write should have set a deadline
		deadlines := mock.getWriteDeadlines()
		Expect(deadlines).To(HaveLen(1))
		Expect(deadlines[0]).NotTo(Equal(time.Time{}))

		// After the flush timer fires, deadline count stays at 1 (Flush no longer clears)
		Eventually(func() int {
			return mock.getFlushCount()
		}, 200*time.Millisecond, 10*time.Millisecond).Should(Equal(1))

		Expect(mock.getWriteDeadlines()).To(HaveLen(1))
	})

	It("io.Copy through TimedFlushWriter extends deadline per chunk", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		dw := NewDeadlineWriter(mock, rc, 10*time.Second)
		fw := NewTimedFlushWriter(dw, 50*time.Millisecond)
		defer fw.Stop()

		// 96KB → 3 Write calls through TimedFlushWriter → DeadlineWriter
		src := make([]byte, 96*1024)
		// onlyReader strips WriterTo so io.Copy uses its 32KB buffer
		n, err := io.Copy(fw, &onlyReader{bytes.NewReader(src)})
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(int64(96 * 1024)))

		// 3 non-zero deadlines from the 3 writes; Flush no longer adds a zero-time entry
		deadlines := mock.getWriteDeadlines()
		Expect(deadlines).To(HaveLen(3))
		for _, d := range deadlines {
			Expect(d).NotTo(Equal(time.Time{}))
		}
	})

	It("Stop does not clear write deadline", func() {
		mock := &deadlineCapture{}
		rc := http.NewResponseController(mock)
		dw := NewDeadlineWriter(mock, rc, 10*time.Second)
		fw := NewTimedFlushWriter(dw, time.Hour) // timer never fires

		_, err := fw.Write([]byte("data"))
		Expect(err).NotTo(HaveOccurred())

		deadlines := mock.getWriteDeadlines()
		Expect(deadlines).To(HaveLen(1))
		Expect(deadlines[0]).NotTo(Equal(time.Time{}))

		fw.Stop()

		// Stop calls Flush which no longer clears the deadline
		Expect(mock.getWriteDeadlines()).To(HaveLen(1))
	})
})
