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
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fakeRunscScript(version string) []byte {
	return []byte(fmt.Sprintf("#!/bin/sh\necho 'runsc version release-%s'\necho 'spec: 1.2.0'\n", version))
}

// fixtureArchive returns the committed gvisor.tar.bz2 for a version known to
// hack/gen-gvisor-test-fixtures.
func fixtureArchive(t *testing.T, version string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "release-"+version+".tar.bz2")) //nolint:gosec // fixed fixture name per test version
	if err != nil {
		t.Fatalf("missing fixture (regenerate with go run ./hack/gen-gvisor-test-fixtures): %v", err)
	}
	return data
}

func fixtureGenDirName(t *testing.T, version string) string {
	t.Helper()
	sum := sha512.Sum512(fixtureArchive(t, version))
	return generationDirName(version, hex.EncodeToString(sum[:]))
}

// gvisorReleaseServer serves a fake release bucket and counts artifact
// downloads (checksum requests are not counted).
func gvisorReleaseServer(t *testing.T, files map[string][]byte, downloads *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for name, content := range files {
		sum := sha512.Sum512(content)
		sumLine := hex.EncodeToString(sum[:]) + "  " + archiveName + "\n"
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, _ *http.Request) {
			downloads.Add(1)
			_, _ = w.Write(content)
		})
		mux.HandleFunc("/"+name+".sha512", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(sumLine))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func testInstaller(t *testing.T, urlBase string) *Installer {
	const version = "20260101.0"
	t.Helper()
	cfg := Config{
		NodeName:          "test-node",
		Version:           version,
		DownloadURLBase:   urlBase,
		Handler:           "runsc",
		InstallDir:        "/opt/isola/bin",
		HostRoot:          t.TempDir(),
		ReconcileInterval: time.Hour,
		RetryInterval:     time.Hour,
	}
	i := New(cfg, slog.New(slog.DiscardHandler), nil, nil, nil)
	i.healthWait = 200 * time.Millisecond
	i.healthPoll = 10 * time.Millisecond
	return i
}

// releaseFiles keys artifacts as "<version>/<arch>/gvisor.tar.bz2", as the
// bucket lays them out.
func releaseFiles(t *testing.T, versions ...string) map[string][]byte {
	t.Helper()
	arch, err := gvisorArch()
	if err != nil {
		t.Skipf("unsupported test arch: %v", err)
	}
	files := map[string][]byte{}
	for _, v := range versions {
		files[v+"/"+arch+"/"+archiveName] = fixtureArchive(t, v)
	}
	return files
}

func TestEnsureGenerationInstallUpgradeAndHeal(t *testing.T) {
	const v1, v2 = "20260101.0", "20260202.0"
	var downloads atomic.Int64
	srv := gvisorReleaseServer(t, releaseFiles(t, v1, v2), &downloads)

	i := testInstaller(t, srv.URL)
	ctx := t.Context()

	gen, changed, err := i.ensureGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || downloads.Load() != 1 {
		t.Fatalf("fresh install: changed=%v downloads=%d", changed, downloads.Load())
	}
	if want := fixtureGenDirName(t, v1); filepath.Base(gen.Path) != want {
		t.Fatalf("generation dir = %s, want %s", filepath.Base(gen.Path), want)
	}
	genDir := gen.hostDir(i.cfg)
	for _, name := range []string{runscBinary, shimBinary, checkpointGofer, "gvisor-bin/runsc-metric-server"} {
		fi, err := os.Stat(filepath.Join(genDir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("%s not installed: %v", name, err)
		}
		if fi.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %v, want 0755", name, fi.Mode())
		}
	}
	if err := i.verifyGeneration(gen); err != nil {
		t.Fatalf("fresh generation does not verify: %v", err)
	}

	gen, changed, err = i.ensureGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changed || downloads.Load() != 1 {
		t.Fatalf("idempotent reconcile: changed=%v downloads=%d", changed, downloads.Load())
	}

	i.cfg.Version = v2
	gen2, changed, err := i.ensureGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || downloads.Load() != 2 {
		t.Fatalf("upgrade: changed=%v downloads=%d", changed, downloads.Load())
	}
	if gen2.Version != v2 {
		t.Errorf("upgrade version = %q", gen2.Version)
	}
	if _, err := os.Stat(genDir); err != nil {
		t.Error("old generation removed by upgrade; running sandboxes still need it")
	}

	// Heal: corrupt a sidecar, plant extra files (top-level and inside
	// gvisor-bin), an unexpected directory, and swap a payload file for a
	// symlink to identical content.
	gen2Dir := gen2.hostDir(i.cfg)
	if err := os.WriteFile(filepath.Join(gen2Dir, filepath.FromSlash(checkpointGofer)), []byte("drifted"), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gen2Dir, "rogue"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gen2Dir, "gvisor-bin", "rogue-sidecar"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gen2Dir, "gvisor-bin", "obsolete"), 0o755); err != nil { //nolint:gosec // drift fixture
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gen2Dir, "gvisor-bin", "obsolete", "junk"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(gen2Dir, shimBinary)
	shimCopy := filepath.Join(t.TempDir(), "shim-copy")
	if data, err := os.ReadFile(shimPath); err != nil { //nolint:gosec // test fixture path
		t.Fatal(err)
	} else if err := os.WriteFile(shimCopy, data, 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatal(err)
	}
	if err := os.Remove(shimPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shimCopy, shimPath); err != nil {
		t.Fatal(err)
	}
	gen2b, changed, err := i.ensureGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || downloads.Load() != 3 {
		t.Fatalf("heal: changed=%v downloads=%d", changed, downloads.Load())
	}
	if gen2b.Path != gen2.Path {
		t.Errorf("heal produced a different generation dir: %s vs %s", gen2b.Path, gen2.Path)
	}
	if err := i.verifyGeneration(gen2b); err != nil {
		t.Errorf("healed generation does not verify: %v", err)
	}
	for _, extra := range []string{"rogue", "gvisor-bin/rogue-sidecar", "gvisor-bin/obsolete"} {
		if _, err := os.Lstat(filepath.Join(gen2Dir, filepath.FromSlash(extra))); !os.IsNotExist(err) {
			t.Errorf("extra %s survived restoration", extra)
		}
	}
	if fi, err := os.Lstat(shimPath); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("symlinked payload not restored to a regular file: %v %v", fi, err)
	}
}

func TestEnsureGenerationChecksumMismatch(t *testing.T) {
	arch, err := gvisorArch()
	if err != nil {
		t.Skipf("unsupported test arch: %v", err)
	}
	const v = "20260101.0"
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%s/%s/", v, arch), func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha512") {
			_, _ = w.Write([]byte(strings.Repeat("ab", sha512.Size) + "  " + archiveName + "\n"))
			return
		}
		_, _ = w.Write([]byte("content that does not match the checksum"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	i := testInstaller(t, srv.URL)
	if _, _, err := i.ensureGeneration(t.Context()); err == nil || !strings.Contains(err.Error(), "sha512 mismatch") {
		t.Fatalf("expected sha512 mismatch error, got: %v", err)
	}
	assertNoPublishedGenerations(t, i)
}

func assertNoPublishedGenerations(t *testing.T, i *Installer) {
	t.Helper()
	entries, err := os.ReadDir(i.cfg.hostPath(i.cfg.releasesDir()))
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), tempPrefix) {
			t.Errorf("unverified content published as generation %q", e.Name())
		}
	}
}

func TestEnsureGenerationDownloadFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	i := testInstaller(t, srv.URL)
	// The checksum is fetched first, so its 404 must already carry the
	// mirror hint.
	if _, _, err := i.ensureGeneration(t.Context()); err == nil || !strings.Contains(err.Error(), "unavailable at the configured downloadURLBase") {
		t.Fatalf("expected the archive-unavailable hint, got: %v", err)
	}
}

func TestEnsureGenerationWrongReportedVersion(t *testing.T) {
	arch, err := gvisorArch()
	if err != nil {
		t.Skipf("unsupported test arch: %v", err)
	}
	// A mirror serving a (checksum-consistent) archive of the WRONG release.
	var downloads atomic.Int64
	files := map[string][]byte{
		"20260202.0/" + arch + "/" + archiveName: fixtureArchive(t, "20260101.0"),
	}
	srv := gvisorReleaseServer(t, files, &downloads)

	i := testInstaller(t, srv.URL)
	i.cfg.Version = "20260202.0"
	if _, _, err := i.ensureGeneration(t.Context()); err == nil || !strings.Contains(err.Error(), "reports version") {
		t.Fatalf("expected version mismatch error, got: %v", err)
	}
	assertNoPublishedGenerations(t, i)
}

func TestEnsureGenerationCorruptArchive(t *testing.T) {
	const v = "20260101.0"
	arch, err := gvisorArch()
	if err != nil {
		t.Skipf("unsupported test arch: %v", err)
	}
	var downloads atomic.Int64
	for name, content := range map[string][]byte{
		"not bzip2":     []byte("these are not the bytes you are looking for"),
		"truncated tar": fixtureArchive(t, v)[:100],
	} {
		t.Run(name, func(t *testing.T) {
			srv := gvisorReleaseServer(t, map[string][]byte{v + "/" + arch + "/" + archiveName: content}, &downloads)
			i := testInstaller(t, srv.URL)
			if _, _, err := i.ensureGeneration(t.Context()); err == nil || !strings.Contains(err.Error(), "extracting") {
				t.Fatalf("expected extraction error, got: %v", err)
			}
			assertNoPublishedGenerations(t, i)
		})
	}
}

// A crash can leave a complete generation directory with no state record; the
// next install of the same version must reuse or repair it in place, never
// delete it (a previously activated generation may still serve sandboxes).
func TestEnsureGenerationReusesExistingDirectory(t *testing.T) {
	const v = "20260101.0"
	var downloads atomic.Int64
	srv := gvisorReleaseServer(t, releaseFiles(t, v), &downloads)
	i := testInstaller(t, srv.URL)

	gen, _, err := i.ensureGeneration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// No state file exists (activation never ran); a fresh installer must
	// still find the directory intact without another download.
	fresh := New(i.cfg, i.log, nil, nil, nil)
	gen2, changed, err := fresh.ensureGeneration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if changed || downloads.Load() != 1 || gen2.Path != gen.Path {
		t.Fatalf("existing generation not reused: changed=%v downloads=%d", changed, downloads.Load())
	}

	// Now corrupt it: repair must land in the SAME directory.
	if err := os.Truncate(filepath.Join(gen.hostDir(i.cfg), runscBinary), 1); err != nil {
		t.Fatal(err)
	}
	gen3, changed, err := fresh.ensureGeneration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || gen3.Path != gen.Path {
		t.Fatalf("corrupted generation not repaired in place: changed=%v path=%s", changed, gen3.Path)
	}

	// A symlinked sidecar directory must be replaced by a real one, without
	// writing through the link.
	genDir := gen.hostDir(i.cfg)
	realCopy := filepath.Join(t.TempDir(), "gvisor-bin-copy")
	if err := os.CopyFS(realCopy, os.DirFS(filepath.Join(genDir, sidecarDirName))); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(genDir, sidecarDirName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realCopy, filepath.Join(genDir, sidecarDirName)); err != nil {
		t.Fatal(err)
	}
	gen4, changed, err := fresh.ensureGeneration(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !changed || gen4.Path != gen.Path {
		t.Fatalf("symlinked sidecar dir not repaired: changed=%v", changed)
	}
	if fi, err := os.Lstat(filepath.Join(genDir, sidecarDirName)); err != nil || !fi.IsDir() {
		t.Errorf("sidecar dir is not a real directory after repair: %v %v", fi, err)
	}
	if entries, err := os.ReadDir(realCopy); err != nil || len(entries) == 0 {
		t.Errorf("repair wrote through the symlink target: %v %v", entries, err)
	}
}

func TestParseSHA512File(t *testing.T) {
	digest := strings.Repeat("ab", sha512.Size)
	tests := []struct {
		name    string
		body    string
		want    string
		wantErr string
	}{
		{"canonical", digest + "  gvisor.tar.bz2\n", digest, ""},
		{"binary-mode marker", digest + " *gvisor.tar.bz2\n", digest, ""},
		{"uppercase hex accepted", strings.ToUpper(digest) + "  gvisor.tar.bz2", digest, ""},
		{"missing filename", digest, "", "expected"},
		{"wrong filename", digest + "  runsc\n", "", `expected "gvisor.tar.bz2"`},
		{"short digest", "abcd  gvisor.tar.bz2\n", "", "not a sha512"},
		{"non-hex digest", strings.Repeat("zz", sha512.Size) + "  gvisor.tar.bz2\n", "", "not a sha512"},
		{"empty", "", "", "expected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSHA512File([]byte(tt.body))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got (%q, %v), want %q", got, err, tt.want)
			}
		})
	}
}

func TestWriteFileAtomicPreservesMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(p, []byte("new")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
	data, err := os.ReadFile(p) //nolint:gosec // test fixture path
	if err != nil || string(data) != "new" {
		t.Errorf("content = %q err=%v", data, err)
	}
}

// Pins the contract between writeFileAtomic's temp naming and
// removeStaleTemps's glob: a temp stranded by a crash mid-write must be
// reclaimed by the sweep.
func TestRemoveStaleTempsCoversAtomicWriteTemps(t *testing.T) {
	dir := t.TempDir()
	tmp, err := os.CreateTemp(dir, tempPrefix+"state.json-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, tempPrefix+"stage-x")
	if err := os.MkdirAll(filepath.Join(stray, "gvisor-bin"), 0o755); err != nil { //nolint:gosec // test fixture tree
		t.Fatal(err)
	}
	removeStaleTemps(dir)
	if _, err := os.Stat(tmp.Name()); !os.IsNotExist(err) {
		t.Errorf("stranded atomic-write temp %s not swept", filepath.Base(tmp.Name()))
	}
	if _, err := os.Stat(stray); err != nil {
		t.Error("removeStaleTemps must leave directories alone (it also runs on /etc/containerd)")
	}
	sweepStaleStaging(dir)
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Error("stranded staging dir not swept")
	}
}

func TestRunscReportedVersionParse(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "runsc")
	if err := os.WriteFile(bin, fakeRunscScript("20260101.0"), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatal(err)
	}
	got, err := runscReportedVersion(t.Context(), bin)
	if err != nil {
		t.Fatal(err)
	}
	if got != "release-20260101.0" {
		t.Errorf("reported = %q", got)
	}
}

func TestDownloadClientRejectsSchemeDowngrade(t *testing.T) {
	for _, tt := range []struct {
		name    string
		target  string
		wantErr bool
	}{
		{"https hop allowed", "https://mirror.internal/gvisor.tar.bz2", false},
		{"http hop refused", "http://mirror.internal/gvisor.tar.bz2", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = downloadClient.CheckRedirect(req, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckRedirect(%s) error = %v, wantErr %v", tt.target, err, tt.wantErr)
			}
		})
	}

	t.Run("redirect chain is bounded", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://mirror.internal/gvisor.tar.bz2", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := downloadClient.CheckRedirect(req, make([]*http.Request, 10)); err == nil {
			t.Fatal("expected the redirect chain to be capped")
		}
	})
}
