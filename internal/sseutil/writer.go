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

package sseutil

import (
	"bufio"
	"io"
	"strconv"
	"unicode/utf8"
)

var keepalive = []byte(": keepalive\n\n")

// Writer wraps an io.Writer and emits Server-Sent Events.
//
// Data events carry raw stdout/stderr text with an id: field for resume
// via Last-Event-ID. Keepalive comments (": keepalive") keep connections
// alive through intermediate infrastructure with idle timeouts.
//
// The writer performs incremental UTF-8 validation, replacing invalid byte
// sequences with U+FFFD (�). Partial multi-byte sequences are buffered
// across WriteData calls.
//
// All parts of an SSE event (data: lines, id: line, blank terminator)
// are accumulated in a bufio.Writer and flushed as a single Write call
// to minimize overhead through the deadline/flush writer chain.
type Writer struct {
	w       *bufio.Writer
	partial []byte // incomplete UTF-8 multi-byte sequence from previous WriteData call
}

// NewWriter creates a new SSE writer wrapping w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: bufio.NewWriter(w)}
}

// WriteData writes an SSE data event with the given raw bytes and byte offset as the event id.
// The data is validated as UTF-8 with invalid sequences replaced by U+FFFD.
// Newlines (\n, \r\n, \r) split the payload into multiple data: lines per the SSE spec.
// If the payload ends with a newline, an extra empty data: line is emitted so the
// SSE parser's trailing-LF-stripping preserves it.
// Zero-byte writes are ignored (no event emitted).
func (w *Writer) WriteData(data []byte, offset int64) error {
	if len(data) == 0 {
		return nil
	}

	// Prepend any buffered partial multi-byte sequence from previous call
	if len(w.partial) > 0 {
		data = append(w.partial, data...)
		w.partial = w.partial[:0]
	}

	// Check for incomplete UTF-8 sequence at end of data
	if tail := incompleteUTF8Tail(data); tail > 0 {
		w.partial = append(w.partial[:0], data[len(data)-tail:]...)
		data = data[:len(data)-tail]
		if len(data) == 0 {
			return nil
		}
	}

	// Validate UTF-8, replacing invalid sequences with U+FFFD
	validated := validateUTF8(data)

	// Write data: lines and id into the bufio buffer, then flush once
	w.writeDataLines(validated)
	w.writeID(offset)
	return w.w.Flush()
}

// Flush writes any buffered partial UTF-8 sequence as U+FFFD replacement characters.
// Call this when the stream ends to flush any incomplete multi-byte sequences.
func (w *Writer) Flush(offset int64) error {
	if len(w.partial) == 0 {
		return nil
	}
	// Replace each byte of the incomplete sequence with U+FFFD
	replacement := make([]byte, 0, len(w.partial)*3)
	for range w.partial {
		replacement = append(replacement, "\uFFFD"...)
	}
	w.partial = w.partial[:0]

	w.writeDataLine(string(replacement))
	w.writeID(offset)
	return w.w.Flush()
}

// WriteKeepalive writes an SSE comment line that keeps the connection alive
// through intermediate infrastructure without being visible to SSE clients.
func (w *Writer) WriteKeepalive() error {
	w.w.Write(keepalive) //nolint:errcheck // error is sticky; returned by Flush
	return w.w.Flush()
}

// writeDataLine writes a single "data: <line>\n" into the buffer.
func (w *Writer) writeDataLine(line string) {
	w.w.WriteString("data: ") //nolint:errcheck // error is sticky; returned by Flush
	w.w.WriteString(line)     //nolint:errcheck // error is sticky; returned by Flush
	w.w.WriteByte('\n')       //nolint:errcheck // error is sticky; returned by Flush
}

// writeID writes "id: <offset>\n\n" into the buffer.
func (w *Writer) writeID(offset int64) {
	w.w.WriteString("id: ")                        //nolint:errcheck // error is sticky; returned by Flush
	w.w.WriteString(strconv.FormatInt(offset, 10)) //nolint:errcheck // error is sticky; returned by Flush
	w.w.WriteString("\n\n")                        //nolint:errcheck // error is sticky; returned by Flush
}

// writeDataLines splits s on \n, \r\n, and bare \r into "data:" lines.
// If s ends with a newline, an extra empty "data:" line is written so the
// SSE parser's trailing-LF-stripping preserves it.
func (w *Writer) writeDataLines(s string) {
	for {
		line, rest, hasNewline := nextChunk(s)
		w.writeDataLine(line)
		if !hasNewline {
			return
		}
		s = rest
		if s == "" {
			// Input ended with a newline — emit extra empty data: line
			w.writeDataLine("")
			return
		}
	}
}

// nextChunk returns the text before the first newline (\n, \r\n, or \r),
// the remaining text after it, and whether a newline was found.
func nextChunk(s string) (chunk, remaining string, hasNewline bool) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			return s[:i], s[i+1:], true
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				return s[:i], s[i+2:], true
			}
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

// incompleteUTF8Tail returns the number of bytes at the end of data that form
// an incomplete multi-byte UTF-8 sequence. Returns 0 if the data ends with
// complete sequences or only invalid bytes (which validateUTF8 handles).
func incompleteUTF8Tail(data []byte) int {
	for i := len(data) - 1; i >= 0 && i >= len(data)-3; i-- {
		if !utf8.RuneStart(data[i]) {
			continue
		}
		if utf8.FullRune(data[i:]) {
			return 0
		}
		return len(data) - i
	}
	return 0
}

// validateUTF8 returns s with invalid UTF-8 sequences replaced by U+FFFD.
func validateUTF8(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}

	result := make([]byte, 0, len(data))
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			result = append(result, "\uFFFD"...)
		} else {
			result = append(result, data[:size]...)
		}
		data = data[size:]
	}
	return string(result)
}
