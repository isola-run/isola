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
	"slices"
	"strings"
	"testing"
)

func extractSSEData(body string) string {
	var result strings.Builder
	var dataParts []string

	for line := range strings.SplitSeq(body, "\n") {
		switch {
		case strings.HasPrefix(line, "data: "):
			dataParts = append(dataParts, line[6:])
		case line == "data:":
			dataParts = append(dataParts, "")
		case line == "":
			if len(dataParts) > 0 {
				result.WriteString(strings.Join(dataParts, "\n"))
				dataParts = dataParts[:0]
			}
		}
	}

	return result.String()
}

func extractSSEIDs(body string) []string {
	var ids []string
	for line := range strings.SplitSeq(body, "\n") {
		if id, ok := strings.CutPrefix(line, "id: "); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestWriteData_Simple(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("hello world")); err != nil {
		t.Fatal(err)
	}

	want := "data: hello world\nid: 11\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_Multiline(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("line1\nline2")); err != nil {
		t.Fatal(err)
	}

	want := "data: line1\ndata: line2\nid: 11\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_TrailingNewline(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// "hello\n" should produce data: hello\ndata: \n so the parser yields "hello\n"
	if err := w.WriteData([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}

	want := "data: hello\ndata: \nid: 6\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_CRLF(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("a\r\nb")); err != nil {
		t.Fatal(err)
	}

	want := "data: a\ndata: b\nid: 4\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_BareCarriageReturn(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("a\rb")); err != nil {
		t.Fatal(err)
	}

	want := "data: a\ndata: b\nid: 3\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_LeadingSpace(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("  indented")); err != nil {
		t.Fatal(err)
	}

	// Space after colon + data's leading spaces must be preserved
	want := "data:   indented\nid: 10\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_EmptyLines(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("a\n\nb")); err != nil {
		t.Fatal(err)
	}

	want := "data: a\ndata: \ndata: b\nid: 4\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_ZeroByte(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte{}); err != nil {
		t.Fatal(err)
	}

	if got := buf.String(); got != "" {
		t.Errorf("expected empty output for zero-byte write, got %q", got)
	}
}

func TestWriteData_NilSlice(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData(nil); err != nil {
		t.Fatal(err)
	}

	if got := buf.String(); got != "" {
		t.Errorf("expected empty output for nil write, got %q", got)
	}
}

func TestWriteData_InvalidUTF8(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// 0xFF is not a valid UTF-8 byte
	if err := w.WriteData([]byte("hello\xffworld")); err != nil {
		t.Fatal(err)
	}

	want := "data: hello\uFFFDworld\nid: 11\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_SplitMultibyte(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// é = 0xC3 0xA9 — split across two WriteData calls.
	// The writer emits the valid prefix "caf" but holds the resume id at byte 3
	// until the buffered 0xC3 can be completed on a later write.
	if err := w.WriteData([]byte("caf\xc3")); err != nil {
		t.Fatal(err)
	}
	want1 := "data: caf\nid: 3\n\n"
	if got := buf.String(); got != want1 {
		t.Errorf("after first write: got %q, want %q", got, want1)
	}

	if err := w.WriteData([]byte("\xa9!")); err != nil {
		t.Fatal(err)
	}

	want := "data: caf\nid: 3\n\ndata: é!\nid: 6\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_SplitMultibyteAtReadBoundary(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// Simulate a 32 KiB file read that ends with the first byte of a 2-byte rune.
	chunk := append(bytes.Repeat([]byte{'a'}, 32767), 0xC3)
	if err := w.WriteData(chunk); err != nil {
		t.Fatal(err)
	}

	if got := extractSSEData(buf.String()); got != strings.Repeat("a", 32767) {
		t.Errorf("got data len %d, want %d", len(got), 32767)
	}
	if got := extractSSEIDs(buf.String()); !slices.Equal(got, []string{"32767"}) {
		t.Errorf("got ids %v, want [32767]", got)
	}
}

func TestNewWriterAtOffset_ResumeFromSplitMultibyteBoundary(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriterAtOffset(&buf, 32767)

	// Resume from the last committed byte and deliver the buffered rune plus the
	// following ASCII byte as one event.
	if err := w.WriteData([]byte{0xC3, 0xA9, '!'}); err != nil {
		t.Fatal(err)
	}

	if got := extractSSEData(buf.String()); got != "é!" {
		t.Errorf("got data %q, want %q", got, "é!")
	}
	if got := extractSSEIDs(buf.String()); !slices.Equal(got, []string{"32770"}) {
		t.Errorf("got ids %v, want [32770]", got)
	}
}

func TestWriteData_SplitMultibyte_OnlyPartial(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// Write only the first byte of a 2-byte sequence — nothing should be emitted
	if err := w.WriteData([]byte("\xc3")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "" {
		t.Errorf("expected empty output for partial-only multibyte, got %q", got)
	}

	// Complete the sequence
	if err := w.WriteData([]byte("\xa9")); err != nil {
		t.Fatal(err)
	}
	want := "data: é\nid: 2\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteKeepalive(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteKeepalive(); err != nil {
		t.Fatal(err)
	}

	want := ": keepalive\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_OffsetTracking(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteData([]byte("cd")); err != nil {
		t.Fatal(err)
	}

	want := "data: ab\nid: 2\n\ndata: cd\nid: 4\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestNewWriterAtOffset(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriterAtOffset(&buf, 10)

	if err := w.WriteData([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteData([]byte("cd")); err != nil {
		t.Fatal(err)
	}

	want := "data: ab\nid: 12\n\ndata: cd\nid: 14\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_TrailingCR(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("hello\r")); err != nil {
		t.Fatal(err)
	}

	want := "data: hello\nid: 5\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	want = "data: hello\nid: 5\n\ndata: \ndata: \nid: 6\n\n"
	if got := buf.String(); got != want {
		t.Errorf("after flush: got %q, want %q", got, want)
	}
}

func TestWriteData_TrailingCRLF(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("hello\r\n")); err != nil {
		t.Fatal(err)
	}

	want := "data: hello\ndata: \nid: 7\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFlush_IncompleteSequence(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// Buffer a partial multi-byte sequence
	if err := w.WriteData([]byte("\xc3")); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "" {
		t.Errorf("expected empty output for partial multibyte, got %q", got)
	}

	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	want := "data: \uFFFD\nid: 1\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFlush_NothingBuffered(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	if got := buf.String(); got != "" {
		t.Errorf("expected empty output for flush with nothing buffered, got %q", got)
	}
}

func TestWriteData_NullByte(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	// U+0000 is allowed in SSE field values. The parser only rejects NUL inside
	// id fields; data fields keep it as payload.
	if err := w.WriteData([]byte("a\x00b")); err != nil {
		t.Fatal(err)
	}

	want := "data: a\x00b\nid: 3\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_MultipleConsecutiveNewlines(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("\n\n")); err != nil {
		t.Fatal(err)
	}

	want := "data: \ndata: \ndata: \nid: 2\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- Spec compliance tests (https://html.spec.whatwg.org/multipage/server-sent-events.html) ---

func TestWriteData_MixedNewlineStyles(t *testing.T) {
	// go-sse parser_test: "sarmale cu\nghimbir\r\nsunt\rsuper\n\ngenial sincer\r\n"
	// All three newline styles (\n, \r\n, \r) must split into separate data: lines.
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("line1\nline2\r\nline3\rline4")); err != nil {
		t.Fatal(err)
	}

	want := "data: line1\ndata: line2\ndata: line3\ndata: line4\nid: 24\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_DataContainingColons(t *testing.T) {
	// Per spec, the parser splits at the FIRST colon only.
	// "data: key: value" → field name "data", value "key: value".
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("key: value")); err != nil {
		t.Fatal(err)
	}

	want := "data: key: value\nid: 10\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_ParserSpaceStripping(t *testing.T) {
	// Spec: "If value starts with a U+0020 SPACE character, remove it from value."
	// Our "data: " prefix provides the padding space that gets stripped.
	// Data with a leading space should produce "data:  X" so the parser
	// strips one space and preserves the original leading space.
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte(" x")); err != nil {
		t.Fatal(err)
	}

	// "data:  x" → parser strips first space → " x" ✓
	want := "data:  x\nid: 2\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_BOM(t *testing.T) {
	// U+FEFF BOM is valid UTF-8 (3 bytes: EF BB BF). It should pass through
	// as regular data. The spec's BOM handling only applies to the stream start,
	// not within data fields.
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("\xEF\xBB\xBFhello")); err != nil {
		t.Fatal(err)
	}

	want := "data: \uFEFFhello\nid: 8\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_StartsWithNewline(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("\nhello")); err != nil {
		t.Fatal(err)
	}

	want := "data: \ndata: hello\nid: 6\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_OnlyNewline(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("\n")); err != nil {
		t.Fatal(err)
	}

	// "\n" → two data: lines (empty + empty for trailing newline preservation)
	want := "data: \ndata: \nid: 1\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// --- UTF-8 edge cases ---

func TestWriteData_Emoji4Byte(t *testing.T) {
	// 🎉 = F0 9F 8E 89 (4-byte UTF-8)
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("🎉🚀✨")); err != nil {
		t.Fatal(err)
	}

	want := "data: 🎉🚀✨\nid: 11\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_CJK3Byte(t *testing.T) {
	// CJK characters are 3-byte UTF-8 (U+4E16 = E4 B8 96, etc.)
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("世界你好")); err != nil {
		t.Fatal(err)
	}

	want := "data: 世界你好\nid: 12\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_MixedASCIIAndMultibyte(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("hello 世界!\n🎉 done")); err != nil {
		t.Fatal(err)
	}

	want := "data: hello 世界!\ndata: 🎉 done\nid: 23\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_Split4ByteEmoji(t *testing.T) {
	// 🎉 = F0 9F 8E 89 — split across calls at every boundary
	emoji := []byte{0xF0, 0x9F, 0x8E, 0x89}

	tests := []struct {
		name   string
		splits [][]byte
	}{
		{"1+3", [][]byte{emoji[:1], emoji[1:]}},
		{"2+2", [][]byte{emoji[:2], emoji[2:]}},
		{"3+1", [][]byte{emoji[:3], emoji[3:]}},
		{"1+1+1+1", [][]byte{emoji[:1], emoji[1:2], emoji[2:3], emoji[3:]}},
		{"1+2+1", [][]byte{emoji[:1], emoji[1:3], emoji[3:]}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewWriter(&buf)

			for _, part := range tc.splits {
				if err := w.WriteData(part); err != nil {
					t.Fatal(err)
				}
			}

			got := buf.String()
			// The final event must contain the complete emoji
			if !bytes.Contains([]byte(got), []byte("🎉")) {
				t.Errorf("emoji not found in output: %q", got)
			}
		})
	}
}

func TestWriteData_Split3ByteCJK(t *testing.T) {
	// 世 = E4 B8 96 — split across calls
	char := []byte{0xE4, 0xB8, 0x96}

	tests := []struct {
		name   string
		splits [][]byte
	}{
		{"1+2", [][]byte{char[:1], char[1:]}},
		{"2+1", [][]byte{char[:2], char[2:]}},
		{"1+1+1", [][]byte{char[:1], char[1:2], char[2:]}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewWriter(&buf)

			for _, part := range tc.splits {
				if err := w.WriteData(part); err != nil {
					t.Fatal(err)
				}
			}

			got := buf.String()
			if !bytes.Contains([]byte(got), []byte("世")) {
				t.Errorf("CJK char not found in output: %q", got)
			}
		})
	}
}

func TestWriteData_InvalidUTF8_Positions(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  string
	}{
		{
			"at start",
			[]byte("\xffhello"),
			"data: \uFFFDhello\nid: 6\n\n",
		},
		{
			"at end",
			[]byte("hello\xff"),
			"data: hello\uFFFD\nid: 6\n\n",
		},
		{
			"multiple consecutive",
			[]byte("\xff\xfe\xfd"),
			"data: \uFFFD\uFFFD\uFFFD\nid: 3\n\n",
		},
		{
			"between valid multibyte",
			[]byte("é\xffé"),
			"data: é\uFFFDé\nid: 5\n\n",
		},
		{
			"invalid continuation without start byte",
			[]byte("\x80\x81\x82"),
			"data: \uFFFD\uFFFD\uFFFD\nid: 3\n\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewWriter(&buf)

			if err := w.WriteData(tc.input); err != nil {
				t.Fatal(err)
			}

			if got := buf.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteData_OverlongUTF8(t *testing.T) {
	// Overlong encoding of '/' (U+002F): C0 AF — must be rejected as invalid UTF-8
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("\xc0\xaf")); err != nil {
		t.Fatal(err)
	}

	want := "data: \uFFFD\uFFFD\nid: 2\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_SurrogateHalf(t *testing.T) {
	// UTF-16 surrogate halves (U+D800..U+DFFF) are invalid in UTF-8.
	// ED A0 80 = U+D800 encoded (invalid)
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("\xed\xa0\x80")); err != nil {
		t.Fatal(err)
	}

	want := "data: \uFFFD\uFFFD\uFFFD\nid: 3\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_ValidMultibyteOnNewlineBoundary(t *testing.T) {
	// Multi-byte character immediately before and after a newline
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("é\né")); err != nil {
		t.Fatal(err)
	}

	want := "data: é\ndata: é\nid: 5\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_LargePayload(t *testing.T) {
	// Inspired by go-sse parser_test: long string (5018 bytes)
	// Ensure the writer handles large payloads without issues.
	large := bytes.Repeat([]byte("abcdefghijklmnopqrstuvwxyz"), 193)
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData(large); err != nil {
		t.Fatal(err)
	}

	got := buf.String()
	wantPrefix := "data: "
	wantSuffix := "\nid: 5018\n\n"
	if !bytes.HasPrefix([]byte(got), []byte(wantPrefix)) {
		t.Errorf("missing data: prefix")
	}
	if !bytes.HasSuffix([]byte(got), []byte(wantSuffix)) {
		t.Errorf("missing id/terminator suffix, got suffix: %q", got[len(got)-20:])
	}
}

func TestWriteData_KeepaliveDoesNotInterfereWithEvents(t *testing.T) {
	// Interleave keepalive with data events
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteKeepalive(); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteData([]byte("second")); err != nil {
		t.Fatal(err)
	}

	want := "data: first\nid: 5\n\n: keepalive\n\ndata: second\nid: 11\n\n"
	if got := buf.String(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWriteData_CRLFAtBoundaryOfWrites(t *testing.T) {
	// The parser sees one continuous byte stream, not two logical records, so a
	// trailing \r followed by a later \n still forms one CRLF line ending.
	var buf bytes.Buffer
	w := NewWriter(&buf)

	if err := w.WriteData([]byte("hello\r")); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteData([]byte("\nworld")); err != nil {
		t.Fatal(err)
	}

	if got := extractSSEData(buf.String()); got != "hello\nworld" {
		t.Errorf("got data %q, want %q", got, "hello\nworld")
	}
	if got := extractSSEIDs(buf.String()); !slices.Equal(got, []string{"5", "12"}) {
		t.Errorf("got ids %v, want [5 12]", got)
	}
}
