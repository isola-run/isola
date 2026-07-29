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
	"compress/bzip2"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	sidecarDirName  = "gvisor-bin"
	checkpointGofer = sidecarDirName + "/checkpointgofer"

	// Reserved: an archive supplying it could smuggle content past the
	// exact-set drift check.
	manifestFileName = ".manifest.json"
)

// The real 20260721.0 release has 5 entries and ~265MB extracted; neither of
// these binds on an honest archive. The byte cap is a var so tests can
// exercise it without writing gigabytes.
var maxExtractedTotalBytes int64 = 2 << 30

const maxArchiveEntries = 256

type extractedFile struct {
	SHA512 string `json:"sha512"`
	Mode   string `json:"mode"`
}

const sha512HexLen = sha512.Size * 2

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}

// A local bzip2 decode does not honor cancellation the way an HTTP body does.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// Unknown flat regular files are accepted so an upstream addition does not
// become a node-wide install outage. Structural changes (new directories,
// nesting under gvisor-bin/) are still rejected and need a code change.
func (i *Installer) extractArchive(ctx context.Context, archivePath, destDir string) (map[string]extractedFile, error) {
	f, err := os.Open(archivePath) //nolint:gosec // installer-created staging path
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return i.extractTar(bzip2.NewReader(contextReader{ctx, f}), destDir)
}

func (i *Installer) extractTar(r io.Reader, destDir string) (map[string]extractedFile, error) {
	files := make(map[string]extractedFile)
	var total int64
	entries := 0
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading archive: %w", err)
		}
		entries++
		if entries > maxArchiveEntries {
			return nil, fmt.Errorf("archive has more than %d entries", maxArchiveEntries)
		}
		name, err := normalizeEntryName(hdr.Name)
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			if name != sidecarDirName {
				return nil, fmt.Errorf("archive contains unexpected directory %q", name)
			}
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("archive entry %q is not a regular file (type %d)", name, hdr.Typeflag)
		}
		if name == sidecarDirName {
			return nil, fmt.Errorf("archive entry %q collides with the sidecar directory", name)
		}
		rec, n, err := writeEntry(destDir, name, entryMode(name, hdr.Mode), tr, maxExtractedTotalBytes-total)
		if err != nil {
			return nil, fmt.Errorf("extracting %q: %w", name, err)
		}
		total += n
		files[name] = rec
		if !isKnownEntry(name) {
			i.log.Info("archive contains entry unknown to this installer, installing it as-is", "name", name)
		}
	}

	for _, required := range []string{runscBinary, shimBinary, checkpointGofer} {
		if _, ok := files[required]; !ok {
			return nil, fmt.Errorf("archive is missing required entry %q", required)
		}
	}
	return files, nil
}

// Admitting only the two layout levels runsc resolves rejects traversal and
// nesting as a side effect. Components are validated raw, before any
// cleaning: "foo/../runsc" must be rejected, while a literal ".." inside a
// name ("foo..bar") is fine.
func normalizeEntryName(raw string) (string, error) {
	name := strings.TrimSuffix(strings.TrimPrefix(raw, "./"), "/")
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("archive entry %q has an unsafe path", raw)
		}
	}
	switch {
	case len(parts) == 1:
	case len(parts) == 2 && parts[0] == sidecarDirName:
	default:
		return "", fmt.Errorf("archive entry %q is outside the expected flat layout", raw)
	}
	if base := parts[len(parts)-1]; base == manifestFileName || strings.HasPrefix(base, tempPrefix) {
		return "", fmt.Errorf("archive entry %q collides with an installer-reserved name", raw)
	}
	return name, nil
}

// Unknown entries keep only the executable-or-not distinction: a future
// LICENSE must not become executable, a future helper must stay runnable.
func entryMode(name string, hdrMode int64) os.FileMode {
	if isKnownEntry(name) || strings.HasPrefix(name, sidecarDirName+"/") {
		return 0o755
	}
	if hdrMode&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

func isKnownEntry(name string) bool {
	return name == runscBinary || name == shimBinary || strings.HasPrefix(name, sidecarDirName+"/")
}

func writeEntry(destDir, name string, mode os.FileMode, r io.Reader, remaining int64) (extractedFile, int64, error) {
	if remaining <= 0 {
		return extractedFile{}, 0, fmt.Errorf("archive exceeds %d extracted bytes", maxExtractedTotalBytes)
	}
	dest := filepath.Join(destDir, filepath.FromSlash(name))
	// The Chmods defeat the process umask, which would silently narrow the
	// promised fixed modes.
	if dir := filepath.Dir(dest); dir != destDir {
		if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // layout must be world-readable for containerd and shims
			return extractedFile{}, 0, err
		}
		if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // layout must be world-readable for containerd and shims
			return extractedFile{}, 0, err
		}
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode) //nolint:gosec // staging dir the installer just created
	if err != nil {
		return extractedFile{}, 0, err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return extractedFile{}, 0, err
	}
	h := sha512.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, remaining+1))
	closeErr := f.Close()
	if copyErr == nil && n > remaining {
		copyErr = fmt.Errorf("archive exceeds %d extracted bytes", maxExtractedTotalBytes)
	}
	if copyErr == nil && n == 0 {
		copyErr = errors.New("empty file")
	}
	if copyErr != nil {
		return extractedFile{}, n, copyErr
	}
	if closeErr != nil {
		return extractedFile{}, n, closeErr
	}
	return extractedFile{SHA512: hex.EncodeToString(h.Sum(nil)), Mode: fmt.Sprintf("%#o", mode)}, n, nil
}
