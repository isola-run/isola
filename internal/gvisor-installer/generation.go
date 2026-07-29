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
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Generation is one installed release under releases/<version>-<digest>.
// Its payload only ever changes back towards its manifest, never to different
// content: running sandboxes resolve runsc and sidecars through this path for
// their whole lifetime.
type Generation struct {
	// Host-view absolute, as stored in state and rendered into configs.
	Path          string
	Version       string
	ArchiveSHA512 string
	Files         map[string]extractedFile
}

// Written last during creation, so a directory containing a manifest is
// always a complete extraction.
type installManifest struct {
	SchemaVersion int                      `json:"schemaVersion"`
	Version       string                   `json:"version"`
	ArchiveSHA512 string                   `json:"archiveSHA512"`
	Files         map[string]extractedFile `json:"files"`
}

const manifestSchemaVersion = 1

func generationDirName(version, archiveSHA512 string) string {
	return version + "-" + archiveSHA512
}

func (g Generation) runscPath() string { return path.Join(g.Path, runscBinary) }
func (g Generation) shimPath() string  { return path.Join(g.Path, shimBinary) }

// hostDir converts the stored host-view path to one this process can open.
func (g Generation) hostDir(cfg Config) string { return cfg.hostPath(g.Path) }

func (i *Installer) writeManifest(dir string, g Generation) error {
	return writeJSONFile(filepath.Join(dir, manifestFileName), installManifest{
		SchemaVersion: manifestSchemaVersion,
		Version:       g.Version,
		ArchiveSHA512: g.ArchiveSHA512,
		Files:         g.Files,
	})
}

func (i *Installer) loadGeneration(genPath string) (Generation, error) {
	var m installManifest
	dir := i.cfg.hostPath(genPath)
	// The directory and manifest must be the real thing, not links into
	// content this generation does not own.
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return Generation{}, fmt.Errorf("generation %s is not a directory", genPath)
	}
	if fi, err := os.Lstat(filepath.Join(dir, manifestFileName)); err != nil || !fi.Mode().IsRegular() {
		return Generation{}, fmt.Errorf("generation %s has no regular manifest", genPath)
	}
	if err := readJSONFile(filepath.Join(dir, manifestFileName), &m); err != nil {
		return Generation{}, err
	}
	if m.SchemaVersion != manifestSchemaVersion {
		return Generation{}, fmt.Errorf("manifest %s has unsupported schema %d", genPath, m.SchemaVersion)
	}
	if err := validateManifestFiles(m.Files); err != nil {
		return Generation{}, fmt.Errorf("manifest %s: %w", genPath, err)
	}
	if path.Base(genPath) != generationDirName(m.Version, m.ArchiveSHA512) {
		return Generation{}, fmt.Errorf("manifest %s does not match its directory name", genPath)
	}
	return Generation{Path: genPath, Version: m.Version, ArchiveSHA512: m.ArchiveSHA512, Files: m.Files}, nil
}

// A tampered manifest must not be able to bless tampered payload.
func validateManifestFiles(files map[string]extractedFile) error {
	for _, required := range []string{runscBinary, shimBinary, checkpointGofer} {
		if _, ok := files[required]; !ok {
			return fmt.Errorf("missing required file %q", required)
		}
	}
	for name, rec := range files {
		if _, err := normalizeEntryName(name); err != nil {
			return err
		}
		if len(rec.SHA512) != sha512HexLen || !isHex(rec.SHA512) {
			return fmt.Errorf("file %q has a malformed digest", name)
		}
		if rec.Mode != "0755" && rec.Mode != "0644" {
			return fmt.Errorf("file %q has unexpected mode %q", name, rec.Mode)
		}
	}
	return nil
}

// Memoized per reconcile pass: verifying hashes the full ~265MB payload.
func (i *Installer) verifyGenerationCached(g Generation) error {
	if cached, ok := i.verifyCache[g.Path]; ok {
		return cached
	}
	err := i.verifyGeneration(g)
	if i.verifyCache != nil {
		i.verifyCache[g.Path] = err
	}
	return err
}

// Exact-set equality with the manifest.
func (i *Installer) verifyGeneration(g Generation) error {
	dir := g.hostDir(i.cfg)
	for name, want := range g.Files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		// Lstat: a symlink to matching content must not pass as payload, or
		// the generation is no longer self-contained.
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			return fmt.Errorf("generation file %q missing or not regular", name)
		}
		if got := fmt.Sprintf("%#o", fi.Mode().Perm()); got != want.Mode {
			return fmt.Errorf("generation file %q has mode %s, manifest says %s", name, got, want.Mode)
		}
		sum, err := fileSHA512(p)
		if err != nil {
			return err
		}
		if sum != want.SHA512 {
			return fmt.Errorf("generation file %q does not match its manifest digest", name)
		}
	}
	return i.checkNoExtraFiles(dir, g.Files)
}

func (i *Installer) checkNoExtraFiles(dir string, files map[string]extractedFile) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if rel == manifestFileName {
			if !d.Type().IsRegular() {
				return fmt.Errorf("generation file %q is not a regular file", rel)
			}
			return nil
		}
		if d.IsDir() {
			if rel == sidecarDirName {
				return nil
			}
			return fmt.Errorf("generation contains unexpected directory %q", rel)
		}
		if _, ok := files[rel]; !ok && !strings.HasPrefix(path.Base(rel), tempPrefix) {
			return fmt.Errorf("generation contains unexpected file %q", rel)
		}
		return nil
	})
}

// Restoration comes from a staged extraction of the identical archive digest,
// so it can never introduce cross-release skew into a path that running
// sandboxes still resolve. The directory itself is never renamed or removed:
// per-file atomic replacement, extras removed last, then a full re-verify.
func (i *Installer) restoreGeneration(g Generation, stagingDir string) error {
	dir := g.hostDir(i.cfg)
	for _, d := range []string{dir, filepath.Join(dir, sidecarDirName)} {
		// A symlinked directory would make MkdirAll and the writes below
		// follow it into content the generation does not own.
		if fi, err := os.Lstat(d); err == nil && !fi.IsDir() {
			if err := os.Remove(d); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(d, 0o755); err != nil { //nolint:gosec // layout must be world-readable for containerd and shims
			return err
		}
		if err := os.Chmod(d, 0o755); err != nil { //nolint:gosec // normalize drifted directory modes
			return err
		}
	}
	for name, want := range g.Files {
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if fi, err := os.Lstat(dest); err == nil && fi.Mode().IsRegular() {
			if got := fmt.Sprintf("%#o", fi.Mode().Perm()); got == want.Mode {
				if sum, err := fileSHA512(dest); err == nil && sum == want.SHA512 {
					continue
				}
			}
		}
		if err := replaceFromStaging(dest, filepath.Join(stagingDir, filepath.FromSlash(name)), want); err != nil {
			return fmt.Errorf("restoring generation file %q: %w", name, err)
		}
		i.log.Info("restored drifted generation file", "generation", g.Path, "file", name)
	}
	if err := i.removeGenerationExtras(dir, g.Files); err != nil {
		return err
	}
	if err := i.writeManifest(dir, g); err != nil {
		return err
	}
	return i.verifyGeneration(g)
}

// Same-directory rename: a shim exec'ing a sidecar sees old or new bytes,
// never truncated ones.
func replaceFromStaging(dest, src string, want extractedFile) error {
	in, err := os.Open(src) //nolint:gosec // installer-created staging path
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	tmp := filepath.Join(filepath.Dir(dest), tempPrefix+filepath.Base(dest))
	mode := os.FileMode(0o644)
	if want.Mode == "0755" {
		mode = 0o755
	}
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode) //nolint:gosec // installer-owned generation dir
	if err != nil {
		return err
	}
	_, writeErr := io.Copy(f, in)
	var syncErr error
	if writeErr == nil {
		// The directory fsync after the rename does not make the CONTENT
		// durable.
		syncErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		return errors.Join(writeErr, syncErr, closeErr)
	}
	if err := os.Chmod(tmp, mode); err != nil { // creation modes are umask-filtered
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return syncDir(filepath.Dir(dest))
}

func (i *Installer) removeGenerationExtras(dir string, files map[string]extractedFile) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." || rel == sidecarDirName {
				return nil
			}
			i.log.Info("removing unexpected directory from generation", "dir", rel)
			if err := os.RemoveAll(p); err != nil {
				return err
			}
			return fs.SkipDir
		}
		if rel == manifestFileName && d.Type().IsRegular() {
			return nil
		}
		if _, ok := files[rel]; !ok {
			i.log.Info("removing unexpected file from generation", "file", rel)
			return os.Remove(p)
		}
		return nil
	})
}
