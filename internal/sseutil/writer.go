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
	"bytes"
	"io"
	"strconv"
	"unicode/utf8"
)

var keepalive = []byte(": keepalive\n\n")

// Writer wraps an io.Writer and emits Server-Sent Events.
//
// The writer performs incremental UTF-8 validation, replacing invalid byte
// sequences with U+FFFD. Partial multi-byte sequences are buffered
// across WriteData calls.
//
// It only emits ids for bytes whose effect is already visible to the client,
// buffering ambiguous tails (for example an incomplete UTF-8 sequence
// or a trailing \r that might become part of a later \r\n).
type Writer struct {
	w       io.Writer
	offset  int64
	pending []byte // raw bytes withheld until they can be emitted with a stable resume offset
}

// NewWriter creates a new SSE writer wrapping w.
func NewWriter(w io.Writer) *Writer {
	return NewWriterAtOffset(w, 0)
}

// NewWriterAtOffset creates a new SSE writer whose first event id starts at offset.
func NewWriterAtOffset(w io.Writer, offset int64) *Writer {
	return &Writer{w: w, offset: offset}
}

// WriteData writes an SSE data event for the given raw bytes.
// The data is validated as UTF-8 with invalid sequences replaced by U+FFFD.
// Newlines (\n, \r\n, \r) split the payload into multiple data: lines per the SSE spec.
func (w *Writer) WriteData(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	w.offset += int64(len(data))

	var combined []byte
	if len(w.pending) == 0 {
		combined = data
	} else {
		combined = make([]byte, 0, len(w.pending)+len(data))
		combined = append(combined, w.pending...)
		combined = append(combined, data...)
	}

	safeLen := safePrefixLen(combined)
	w.pending = append(w.pending[:0], combined[safeLen:]...) // pending unsafe suffix
	if safeLen == 0 {
		return nil
	}

	sanitized := sanitizeUTF8(combined[:safeLen])

	var event bytes.Buffer
	writeDataLines(&event, sanitized)
	writeID(&event, w.offset-int64(len(w.pending)))
	_, err := w.w.Write(event.Bytes())
	return err
}

// Finish finalizes any buffered tail bytes now that no future data can
// disambiguate them. It does not close the underlying writer.
//
// Only call Finish when the input stream is genuinely complete (e.g. process
// exited and all output drained). Calling it mid-stream replaces incomplete
// UTF-8 tails with U+FFFD and commits the final byte offset as an SSE id.
// A client resuming from that id would skip the real bytes, corrupting the
// stream. On abnormal termination (client disconnect, write error), simply
// discard the writer — the uncommitted pending bytes will be re-read on
// the next resume.
func (w *Writer) Finish() error {
	if len(w.pending) == 0 {
		return nil
	}

	tail := incompleteUTF8Tail(w.pending)
	sanitized := sanitizeUTF8(w.pending[:len(w.pending)-tail])
	if tail > 0 {
		// An unfinished UTF-8 prefix at end-of-stream is one maximal subpart and
		// therefore becomes a single replacement character.
		sanitized += "\uFFFD"
	}
	w.pending = w.pending[:0]

	var event bytes.Buffer
	writeDataLines(&event, sanitized)
	writeID(&event, w.offset)
	_, err := w.w.Write(event.Bytes())
	return err
}

// WriteKeepalive writes an SSE comment line that keeps the connection alive
// through intermediate infrastructure without being visible to SSE clients.
func (w *Writer) WriteKeepalive() error {
	_, err := w.w.Write(keepalive)
	return err
}

// writeDataLine writes a single "data: <line>\n" into the buffer.
func writeDataLine(buf *bytes.Buffer, line string) {
	buf.WriteString("data: ")
	buf.WriteString(line)
	buf.WriteByte('\n')
}

// writeID writes "id: <offset>\n\n" into the buffer.
func writeID(buf *bytes.Buffer, offset int64) {
	buf.WriteString("id: ")
	buf.WriteString(strconv.FormatInt(offset, 10))
	buf.WriteString("\n\n")
}

// writeDataLines splits s on \n, \r\n, and bare \r into "data:" lines.
//
//	"hello"    -> data: hello
//	"a\nb"     -> data: a / data: b
//	"hello\n"  -> data: hello / data:
//
// The trailing-newline case matters: the SSE spec strips one trailing LF when
// concatenating data fields, so the extra empty "data:" line preserves the
// original \n. Without it the client receives "hello" instead of "hello\n",
// which breaks byte-offset resume (resuming with Last-Event-ID header).
func writeDataLines(buf *bytes.Buffer, s string) {
	for {
		line, rest, hasNewline := nextChunk(s)
		writeDataLine(buf, line)
		if !hasNewline {
			return
		}
		s = rest
	}
}

// nextChunk splits s at the first newline (\n, \r\n, or \r).
// Returns the text before it, the text after it, and whether one was found.
//
//	"a\nb"  -> ("a", "b", true)
//	"a\r\n" -> ("a", "",  true)
//	"hello" -> ("hello", "", false)
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

// safePrefixLen returns the number of bytes at the start of data that can be
// emitted immediately without advancing the resume offset past ambiguous bytes.
func safePrefixLen(data []byte) int {
	n := len(data) - incompleteUTF8Tail(data)
	// SSE line endings are \r\n, \r or \n. If we got \r,
	// we still don't know whether the next byte will be \n (ambiguous).
	if n > 0 && data[n-1] == '\r' {
		n--
	}
	return n
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

// sanitizeUTF8 returns s with invalid UTF-8 sequences replaced by U+FFFD.
func sanitizeUTF8(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}

	result := make([]byte, 0, len(data))
	for len(data) > 0 {
		// DecodeRune unpacks the first UTF-8 encoding in p and returns the rune
		firstRune, size := utf8.DecodeRune(data)
		invalidEncoding := firstRune == utf8.RuneError && size == 1
		if invalidEncoding {
			result = append(result, "\uFFFD"...)
		} else {
			result = append(result, data[:size]...)
		}
		data = data[size:] // skip the handled rune for next iteration
	}
	return string(result)
}
