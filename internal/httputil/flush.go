package httputil

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// TimedFlushWriter wraps an io.Writer and an http.Flusher, flushing buffered
// data to the client at most every `interval`. Modeled on Go stdlib's
// maxLatencyWriter in net/http/httputil/reverseproxy.go.
//
// This prevents excessive per-write syscalls while keeping streaming latency bounded.
type TimedFlushWriter struct {
	w        io.Writer
	flusher  http.Flusher
	interval time.Duration

	mu           sync.Mutex
	t            *time.Timer
	flushPending bool
}

func NewTimedFlushWriter(w io.Writer, interval time.Duration) *TimedFlushWriter {
	fw := &TimedFlushWriter{
		w:        w,
		interval: interval,
	}
	if f, ok := w.(http.Flusher); ok {
		fw.flusher = f
	}
	return fw
}

func (fw *TimedFlushWriter) Write(p []byte) (n int, err error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	n, err = fw.w.Write(p)
	if fw.flusher == nil {
		return
	}
	if fw.flushPending {
		return
	}
	if fw.t == nil {
		fw.t = time.AfterFunc(fw.interval, fw.delayedFlush)
	} else {
		fw.t.Reset(fw.interval)
	}
	fw.flushPending = true
	return
}

func (fw *TimedFlushWriter) delayedFlush() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if !fw.flushPending {
		return
	}
	fw.flusher.Flush()
	fw.flushPending = false
}

// Stop cancels the pending flush timer and performs a final flush so the
// client receives any remaining buffered bytes immediately, without relying
// on the framework to finalize the response.
// The final flush behaviour differs from stdlib's reverseproxy behaviour
// which does not flush the remaining bytes on stop.
func (fw *TimedFlushWriter) Stop() {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if fw.t != nil {
		fw.t.Stop()
	}
	if fw.flusher != nil && fw.flushPending {
		fw.flusher.Flush()
	}
	fw.flushPending = false
}
