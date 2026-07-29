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

package gvisorinstaller

import (
	"archive/tar"
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	typeflag byte
	mode     int64
	data     string
	linkname string
}

func regEntry(name, data string) tarEntry {
	return tarEntry{name: name, typeflag: tar.TypeReg, mode: 0o755, data: data}
}

// validLayout is the minimal accepted archive shape.
func validLayout() []tarEntry {
	return []tarEntry{
		regEntry("runsc", "#!/bin/sh\necho runsc\n"),
		regEntry("containerd-shim-runsc-v1", "#!/bin/sh\nexit 0\n"),
		{name: "gvisor-bin/", typeflag: tar.TypeDir, mode: 0o755},
		regEntry("gvisor-bin/checkpointgofer", "#!/bin/sh\nexit 0\n"),
	}
}

func tarStream(t *testing.T, entries []tarEntry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typeflag, Mode: e.mode, Linkname: e.linkname}
		if e.typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.data))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func extractInstaller() *Installer {
	return &Installer{log: slog.New(slog.DiscardHandler)}
}

func TestExtractTarAcceptsValidLayout(t *testing.T) {
	entries := append(validLayout(),
		regEntry("gvisor-bin/future-sidecar", "#!/bin/sh\nexit 0\n"),
		tarEntry{name: "LICENSE", typeflag: tar.TypeReg, mode: 0o644, data: "Apache"},
		regEntry("future-helper", "#!/bin/sh\nexit 0\n"),
		tarEntry{name: "./dot-prefixed", typeflag: tar.TypeReg, mode: 0o644, data: "ok"},
	)
	dir := t.TempDir()
	files, err := extractInstaller().extractTar(tarStream(t, entries), dir)
	if err != nil {
		t.Fatal(err)
	}
	wantModes := map[string]string{
		"runsc": "0755", "containerd-shim-runsc-v1": "0755",
		"gvisor-bin/checkpointgofer": "0755", "gvisor-bin/future-sidecar": "0755",
		"LICENSE": "0644", "future-helper": "0755", "dot-prefixed": "0644",
	}
	if len(files) != len(wantModes) {
		t.Fatalf("extracted %d files, want %d: %v", len(files), len(wantModes), files)
	}
	for name, wantMode := range wantModes {
		rec, ok := files[name]
		if !ok {
			t.Fatalf("%s missing from result", name)
		}
		if rec.Mode != wantMode {
			t.Errorf("%s mode = %s, want %s", name, rec.Mode, wantMode)
		}
		fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s not on disk: %v", name, err)
		}
		if got := fmt.Sprintf("%#o", fi.Mode().Perm()); got != wantMode {
			t.Errorf("%s on-disk mode = %s, want %s", name, got, wantMode)
		}
	}
}

func TestExtractTarRejections(t *testing.T) {
	tests := []struct {
		name    string
		entries []tarEntry
		wantErr string
	}{
		{"traversal", append(validLayout(), regEntry("../escape", "x")), "unsafe path"},
		{"absolute", append(validLayout(), regEntry("/etc/passwd", "x")), "unsafe path"},
		{"nested traversal", append(validLayout(), regEntry("gvisor-bin/../../escape", "x")), "unsafe path"},
		{"traversal that cleans to a valid name", append(validLayout(), regEntry("foo/../runsc", "x")), "unsafe path"},
		{"dot component", append(validLayout(), regEntry("gvisor-bin/./x", "x")), "unsafe path"},
		{"symlink", append(validLayout(), tarEntry{name: "link", typeflag: tar.TypeSymlink, linkname: "runsc"}), "not a regular file"},
		{"hardlink", append(validLayout(), tarEntry{name: "link", typeflag: tar.TypeLink, linkname: "runsc"}), "not a regular file"},
		{"fifo", append(validLayout(), tarEntry{name: "pipe", typeflag: tar.TypeFifo}), "not a regular file"},
		{"duplicate", append(validLayout(), regEntry("runsc", "again")), "exists"},
		{"nested dir", append(validLayout(), tarEntry{name: "gvisor-bin/sub/", typeflag: tar.TypeDir, mode: 0o755}), "unexpected directory"},
		{"nested file", append(validLayout(), regEntry("gvisor-bin/sub/x", "x")), "flat layout"},
		{"unexpected dir", append(validLayout(), tarEntry{name: "other/", typeflag: tar.TypeDir, mode: 0o755}), "unexpected directory"},
		{"file at sidecar dir name", []tarEntry{regEntry("runsc", "x"), regEntry("containerd-shim-runsc-v1", "x"), regEntry("gvisor-bin", "x")}, "collides with the sidecar directory"},
		{"empty file", append(validLayout(), tarEntry{name: "empty", typeflag: tar.TypeReg, mode: 0o644}), "empty file"},
		{"reserved manifest name", append(validLayout(), regEntry(".manifest.json", "{}")), "installer-reserved"},
		{"reserved temp prefix", append(validLayout(), regEntry(tempPrefix+"x", "x")), "installer-reserved"},
		{"missing runsc", validLayout()[1:], `missing required entry "runsc"`},
		{"missing checkpointgofer", validLayout()[:3], "checkpointgofer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extractInstaller().extractTar(tarStream(t, tt.entries), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestExtractTarEntryCountCap(t *testing.T) {
	entries := validLayout()
	for n := range maxArchiveEntries {
		entries = append(entries, regEntry(fmt.Sprintf("f%d", n), "x"))
	}
	_, err := extractInstaller().extractTar(tarStream(t, entries), t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatalf("expected entry cap error, got: %v", err)
	}
}

func TestExtractTarByteCap(t *testing.T) {
	restore := maxExtractedTotalBytes
	t.Cleanup(func() { maxExtractedTotalBytes = restore })

	maxExtractedTotalBytes = 40
	if _, err := extractInstaller().extractTar(tarStream(t, validLayout()), t.TempDir()); err == nil || !strings.Contains(err.Error(), "extracted bytes") {
		t.Fatalf("expected total cap error, got: %v", err)
	}
}

func TestNormalizeEntryName(t *testing.T) {
	for raw, want := range map[string]string{
		"runsc":                      "runsc",
		"./runsc":                    "runsc",
		"gvisor-bin/checkpointgofer": "gvisor-bin/checkpointgofer",
		"gvisor-bin/":                "gvisor-bin",
		"foo..bar":                   "foo..bar",
	} {
		got, err := normalizeEntryName(raw)
		if err != nil || got != want {
			t.Errorf("normalizeEntryName(%q) = (%q, %v), want %q", raw, got, err, want)
		}
	}
}
