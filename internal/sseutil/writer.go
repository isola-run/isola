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
	"io"
	"strconv"
	"unicode/utf8"
)

var (
	dataPrefix = []byte("data: ")
	idPrefix   = []byte("id: ")
	newline    = []byte{'\n'}
	keepalive  = []byte(": keepalive\n\n")
)

// Writer wraps an io.Writer and emits Server-Sent Events.
//
// Data events carry raw stdout/stderr text with an id: field for resume
// via Last-Event-ID. Keepalive comments (": keepalive") keep connections
// alive through intermediate infrastructure with idle timeouts.
//
// The writer performs incremental UTF-8 validation, replacing invalid byte
// sequences with U+FFFD (�). Partial multi-byte sequences are buffered
// across WriteData calls.
type Writer struct {
	w   io.Writer
	buf []byte // partial UTF-8 multi-byte sequence from previous WriteData call
}

// NewWriter creates a new SSE writer wrapping w.
func NewWriter(w io.Writer) *Writer {
	return &Writer{w: w}
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
	if len(w.buf) > 0 {
		data = append(w.buf, data...)
		w.buf = w.buf[:0]
	}

	// Check for incomplete UTF-8 sequence at end of data
	if tail := incompleteUTF8Tail(data); tail > 0 {
		w.buf = append(w.buf[:0], data[len(data)-tail:]...)
		data = data[:len(data)-tail]
		if len(data) == 0 {
			return nil
		}
	}

	// Validate UTF-8, replacing invalid sequences with U+FFFD
	validated := validateUTF8(data)

	// Write data: lines by iterating through newlines inline (no slice allocation)
	if err := w.writeDataLines(validated); err != nil {
		return err
	}

	// Write the id and terminate the event
	return w.writeID(offset)
}

// Flush writes any buffered partial UTF-8 sequence as U+FFFD replacement characters.
// Call this when the stream ends to flush any incomplete multi-byte sequences.
func (w *Writer) Flush(offset int64) error {
	if len(w.buf) == 0 {
		return nil
	}
	// Replace each byte of the incomplete sequence with U+FFFD
	replacement := make([]byte, 0, len(w.buf)*3)
	for range w.buf {
		replacement = append(replacement, "\uFFFD"...)
	}
	w.buf = w.buf[:0]

	if err := w.writeDataLine(string(replacement)); err != nil {
		return err
	}
	return w.writeID(offset)
}

// WriteKeepalive writes an SSE comment line that keeps the connection alive
// through intermediate infrastructure without being visible to SSE clients.
func (w *Writer) WriteKeepalive() error {
	_, err := w.w.Write(keepalive)
	return err
}

// writeDataLine writes a single "data: <line>\n".
func (w *Writer) writeDataLine(line string) error {
	if _, err := w.w.Write(dataPrefix); err != nil {
		return err
	}
	if _, err := io.WriteString(w.w, line); err != nil {
		return err
	}
	_, err := w.w.Write(newline)
	return err
}

// writeID writes "id: <offset>\n\n" to terminate an event.
func (w *Writer) writeID(offset int64) error {
	if _, err := w.w.Write(idPrefix); err != nil {
		return err
	}
	if _, err := io.WriteString(w.w, strconv.FormatInt(offset, 10)); err != nil {
		return err
	}
	if _, err := w.w.Write(newline); err != nil {
		return err
	}
	_, err := w.w.Write(newline)
	return err
}

// writeDataLines splits s on \n, \r\n, and bare \r into "data:" lines.
// If s ends with a newline, an extra empty "data:" line is emitted so the
// SSE parser's trailing-LF-stripping preserves it.
func (w *Writer) writeDataLines(s string) error {
	for {
		line, rest, hasNewline := nextChunk(s)
		if err := w.writeDataLine(line); err != nil {
			return err
		}
		if !hasNewline {
			return nil
		}
		s = rest
		if s == "" {
			// Input ended with a newline — emit extra empty data: line
			return w.writeDataLine("")
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
// complete sequences.
func incompleteUTF8Tail(data []byte) int {
	if len(data) == 0 {
		return 0
	}

	// Check the last 1-3 bytes for a start byte without enough continuation bytes
	for i := 1; i <= 3 && i <= len(data); i++ {
		b := data[len(data)-i]
		if b < 0x80 {
			return 0
		}
		if b&0xC0 == 0xC0 {
			var expected int
			switch {
			case b&0xE0 == 0xC0:
				expected = 2
			case b&0xF0 == 0xE0:
				expected = 3
			case b&0xF8 == 0xF0:
				expected = 4
			default:
				return 0
			}
			if i < expected {
				return i
			}
			return 0
		}
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
