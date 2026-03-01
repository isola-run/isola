package httputil

import (
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// Deadline errors are deliberately ignored throughout this file.
// ResponseController.Set{Read,Write}Deadline fails when the connection is
// already unusable (fd closed, nil conn) — the next Read/Write surfaces the
// real error — or returns ErrNotSupported if the ResponseWriter doesn't
// implement deadlines (silently disabling timeout protection). HTTP/2 never
// errors; HTTP/1.1 delegates to the kernel via net.Conn.

// StreamTimeout is the per-operation deadline for streaming I/O.
// Active streams extend the deadline before each operation; stalled
// connections are killed when the deadline fires.
const StreamTimeout = 10 * time.Second

// DeadlineWriter wraps an io.Writer, setting a write deadline before each
// Write. The deadline from the last Write stays active between flushes,
// providing continuous timeout protection for idle connections.
type DeadlineWriter struct {
	w       io.Writer
	rc      *http.ResponseController
	timeout time.Duration
}

// NewDeadlineWriter returns a DeadlineWriter that sets a write deadline before
// each Write. If rc is nil (ResponseController unavailable), the raw writer is
// returned unchanged.
func NewDeadlineWriter(w io.Writer, rc *http.ResponseController, timeout time.Duration) io.Writer {
	if rc == nil {
		return w
	}
	return &DeadlineWriter{w: w, rc: rc, timeout: timeout}
}

// Write extends write deadline and delegates to the inner writer's Write.
func (dw *DeadlineWriter) Write(p []byte) (int, error) {
	_ = dw.rc.SetWriteDeadline(time.Now().Add(dw.timeout))
	return dw.w.Write(p)
}

// Flush delegates to the inner writer's Flush (if available).
func (dw *DeadlineWriter) Flush() {
	if f, ok := dw.w.(http.Flusher); ok {
		f.Flush()
	}
}

// DeadlineReader wraps an io.Reader, setting a read deadline and a rolling
// write deadline before each Read. The write deadline is set at 2x the read
// timeout, giving the server headroom to write an error response after a read
// timeout fires (inspired by tusd's body_reader.go).
type DeadlineReader struct {
	r       io.Reader
	rc      *http.ResponseController
	timeout time.Duration
}

// NewDeadlineReader returns a DeadlineReader that sets a read deadline before
// each Read. If rc is nil, the raw reader is returned unchanged.
func NewDeadlineReader(r io.Reader, rc *http.ResponseController, timeout time.Duration) io.Reader {
	if rc == nil {
		return r
	}
	return &DeadlineReader{r: r, rc: rc, timeout: timeout}
}

// Read extends read and write deadlines and delegates to the inner reader's Read.
func (dr *DeadlineReader) Read(p []byte) (int, error) {
	now := time.Now()
	_ = dr.rc.SetReadDeadline(now.Add(dr.timeout))
	// the write deadline is always 2*timeout - timeout = timeout ahead of the read deadline
	// to ensure we have enough time to both read (without server timing out) and writing
	// the response back when reading ends.
	// similar pattern to github.com/tus/tusd/blob/main/pkg/handler/unrouted_handler.go
	_ = dr.rc.SetWriteDeadline(now.Add(2 * dr.timeout))
	return dr.r.Read(p)
}

// ResponseController extracts an *http.ResponseController from a huma.Context.
// Returns nil if the underlying writer is not an http.ResponseWriter.
func ResponseController(ctx huma.Context) *http.ResponseController {
	if rw, ok := ctx.BodyWriter().(http.ResponseWriter); ok {
		return http.NewResponseController(rw)
	}
	return nil
}
