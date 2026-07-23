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

// gvisorReleaseServer serves a fake release bucket and counts artifact downloads.
func gvisorReleaseServer(t *testing.T, files map[string][]byte, downloads *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for name, content := range files {
		sum := sha512.Sum512(content)
		sumLine := hex.EncodeToString(sum[:]) + "  " + name + "\n"
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

// releaseFiles keys artifacts as "<version>/<arch>/<binary>", as the bucket lays them out.
func releaseFiles(t *testing.T, version string) map[string][]byte {
	t.Helper()
	arch, err := gvisorArch()
	if err != nil {
		t.Skipf("unsupported test arch: %v", err)
	}
	return map[string][]byte{
		version + "/" + arch + "/" + runscBinary: fakeRunscScript(version),
		version + "/" + arch + "/" + shimBinary:  []byte("#!/bin/sh\nexit 0\n"),
	}
}

func TestEnsureBinariesInstallUpgradeAndHeal(t *testing.T) {
	const v1, v2 = "20260101.0", "20260202.0"
	var downloads atomic.Int64
	files := releaseFiles(t, v1)
	for k, v := range releaseFiles(t, v2) {
		files[k] = v
	}
	srv := gvisorReleaseServer(t, files, &downloads)

	i := testInstaller(t, srv.URL)
	ctx := t.Context()

	changed, err := i.ensureBinaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || downloads.Load() != 2 {
		t.Fatalf("fresh install: changed=%v downloads=%d", changed, downloads.Load())
	}
	installDir := i.cfg.hostPath(i.cfg.InstallDir)
	for _, name := range []string{runscBinary, shimBinary} {
		fi, err := os.Stat(filepath.Join(installDir, name))
		if err != nil {
			t.Fatalf("%s not installed: %v", name, err)
		}
		if fi.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s not executable: %v", name, fi.Mode())
		}
	}
	if got := i.installedVersion(); got != v1 {
		t.Errorf("installedVersion = %q, want %q", got, v1)
	}

	changed, err = i.ensureBinaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if changed || downloads.Load() != 2 {
		t.Fatalf("idempotent reconcile: changed=%v downloads=%d", changed, downloads.Load())
	}

	i.cfg.Version = v2
	changed, err = i.ensureBinaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || downloads.Load() != 4 {
		t.Fatalf("upgrade: changed=%v downloads=%d", changed, downloads.Load())
	}
	if got := i.installedVersion(); got != v2 {
		t.Errorf("installedVersion after upgrade = %q, want %q", got, v2)
	}

	if err := os.WriteFile(filepath.Join(installDir, runscBinary), fakeRunscScript("99990101.0"), 0o755); err != nil { //nolint:gosec // test binary must be executable
		t.Fatal(err)
	}
	changed, err = i.ensureBinaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("drifted binary not reinstalled")
	}
	if got := i.installedVersion(); got != v2 {
		t.Errorf("installedVersion after heal = %q, want %q", got, v2)
	}
}

func TestEnsureBinariesChecksumMismatch(t *testing.T) {
	arch, err := gvisorArch()
	if err != nil {
		t.Skipf("unsupported test arch: %v", err)
	}
	const v = "20260101.0"
	mux := http.NewServeMux()
	mux.HandleFunc(fmt.Sprintf("/%s/%s/", v, arch), func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha512") {
			_, _ = w.Write([]byte(strings.Repeat("ab", sha512.Size) + "  file\n"))
			return
		}
		_, _ = w.Write([]byte("content that does not match the checksum"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	i := testInstaller(t, srv.URL)
	if _, err := i.ensureBinaries(t.Context()); err == nil || !strings.Contains(err.Error(), "sha512 mismatch") {
		t.Fatalf("expected sha512 mismatch error, got: %v", err)
	}
	installDir := i.cfg.hostPath(i.cfg.InstallDir)
	entries, err := os.ReadDir(installDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == runscBinary || e.Name() == shimBinary {
			t.Errorf("unverified binary %s was installed", e.Name())
		}
	}
}

func TestEnsureBinariesDownloadFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	i := testInstaller(t, srv.URL)
	if _, err := i.ensureBinaries(t.Context()); err == nil {
		t.Fatal("expected error when artifacts are missing")
	}
}

func TestEnsureBinariesWrongReportedVersion(t *testing.T) {
	const v = "20260101.0"
	var downloads atomic.Int64
	arch, err := gvisorArch()
	if err != nil {
		t.Skipf("unsupported test arch: %v", err)
	}
	// A mirror serving a (checksum-consistent) artifact of the WRONG version.
	files := map[string][]byte{
		v + "/" + arch + "/" + runscBinary: fakeRunscScript("19990101.0"),
		v + "/" + arch + "/" + shimBinary:  []byte("#!/bin/sh\nexit 0\n"),
	}
	srv := gvisorReleaseServer(t, files, &downloads)

	i := testInstaller(t, srv.URL)
	if _, err := i.ensureBinaries(t.Context()); err == nil || !strings.Contains(err.Error(), "reports version") {
		t.Fatalf("expected version mismatch error, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(i.cfg.hostPath(i.cfg.InstallDir), runscBinary)); !os.IsNotExist(err) {
		t.Error("wrong-version runsc was promoted")
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
	removeStaleTemps(dir)
	if _, err := os.Stat(tmp.Name()); !os.IsNotExist(err) {
		t.Errorf("stranded atomic-write temp %s not swept", filepath.Base(tmp.Name()))
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
		{"https hop allowed", "https://mirror.internal/runsc", false},
		{"http hop refused", "http://mirror.internal/runsc", true},
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
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://mirror.internal/runsc", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := downloadClient.CheckRedirect(req, make([]*http.Request, 10)); err == nil {
			t.Fatal("expected the redirect chain to be capped")
		}
	})
}
