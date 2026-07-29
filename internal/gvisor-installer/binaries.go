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
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	runscBinary = "runsc"
	shimBinary  = "containerd-shim-runsc-v1"

	archiveName = "gvisor.tar.bz2"

	// Shared by all staging and atomic writes so one sweep reclaims them.
	tempPrefix = ".isola-tmp."
)

func gvisorArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	default:
		return "", fmt.Errorf("unsupported architecture %q (gVisor releases cover x86_64 and aarch64)", runtime.GOARCH)
	}
}

var runscVersionRe = regexp.MustCompile(`runsc version (\S+)`)

// runscReportedVersion works because runsc is static and runs in-container.
func runscReportedVersion(ctx context.Context, binPath string) (string, error) {
	out, err := exec.CommandContext(ctx, binPath, "--version").CombinedOutput() //nolint:gosec // path is the managed install dir
	if err != nil {
		return "", fmt.Errorf("%s --version: %w (output: %s)", binPath, err, truncateOutput(out))
	}
	m := runscVersionRe.FindSubmatch(out)
	if m == nil {
		return "", fmt.Errorf("could not parse runsc version from output: %s", truncateOutput(out))
	}
	return string(m[1]), nil
}

// ensureGeneration makes the desired release exist as a verified generation
// and returns it. It never touches containerd state.
func (i *Installer) ensureGeneration(ctx context.Context) (Generation, bool, error) {
	version := i.cfg.Version
	releasesHost := i.cfg.hostPath(i.cfg.releasesDir())
	if err := os.MkdirAll(releasesHost, 0o755); err != nil { //nolint:gosec // layout must be world-readable for containerd and shims
		return Generation{}, false, fmt.Errorf("creating releases dir: %w", err)
	}
	sweepStaleStaging(releasesHost)

	if g, ok := i.intactGeneration(version); ok {
		return g, false, nil
	}

	arch, err := gvisorArch()
	if err != nil {
		return Generation{}, false, err
	}
	urlBase := fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(i.cfg.DownloadURLBase, "/"), version, arch)
	i.log.Info("downloading gVisor release archive", "version", version, "url", urlBase+"/"+archiveName)

	archivePath, digest, err := i.downloadArchive(ctx, urlBase, releasesHost)
	if err != nil {
		return Generation{}, false, err
	}
	defer func() { _ = os.Remove(archivePath) }()

	staging, err := os.MkdirTemp(releasesHost, tempPrefix+"stage-")
	if err != nil {
		return Generation{}, false, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	// MkdirTemp's 0700 would become the published generation's mode.
	if err := os.Chmod(staging, 0o755); err != nil { //nolint:gosec // layout must be world-readable for containerd and shims
		return Generation{}, false, err
	}

	files, err := i.extractArchive(ctx, archivePath, staging)
	if err != nil {
		return Generation{}, false, fmt.Errorf("extracting %s: %w", archiveName, err)
	}

	reported, err := runscReportedVersion(ctx, filepath.Join(staging, runscBinary))
	if err != nil {
		return Generation{}, false, fmt.Errorf("staged runsc failed verification: %w", err)
	}
	// Catches a mirror serving the wrong release with a self-consistent digest.
	if got := strings.TrimPrefix(reported, "release-"); got != version {
		return Generation{}, false, fmt.Errorf("staged runsc reports version %q, expected %s", reported, version)
	}

	g := Generation{
		Path:          path.Join(i.cfg.releasesDir(), generationDirName(version, digest)),
		Version:       version,
		ArchiveSHA512: digest,
		Files:         files,
	}
	target := g.hostDir(i.cfg)
	if fi, err := os.Lstat(target); err == nil {
		if !fi.IsDir() {
			// A non-directory squatting on the generation name is drift, not
			// a generation; removing it touches nothing a sandbox can hold.
			if err := os.Remove(target); err != nil {
				return Generation{}, false, err
			}
			return i.publishGeneration(g, staging, releasesHost)
		}
		i.log.Info("generation directory already exists, restoring it in place", "generation", g.Path)
		if err := i.restoreGeneration(g, staging); err != nil {
			return Generation{}, false, err
		}
		i.markVerified(g)
		return g, true, nil
	}
	return i.publishGeneration(g, staging, releasesHost)
}

// Manifest before rename: a directory under releases/ is complete by
// construction.
func (i *Installer) publishGeneration(g Generation, staging, releasesHost string) (Generation, bool, error) {
	if err := i.writeManifest(staging, g); err != nil {
		return Generation{}, false, err
	}
	if err := os.Rename(staging, g.hostDir(i.cfg)); err != nil {
		return Generation{}, false, fmt.Errorf("publishing generation: %w", err)
	}
	if err := syncDir(releasesHost); err != nil {
		return Generation{}, false, err
	}
	i.log.Info("gVisor generation installed", "generation", g.Path)
	i.markVerified(g)
	return g, true, nil
}

// A repair earlier in this reconcile supersedes the failure the cache
// recorded before it, or publishStatus would report the pre-repair verdict.
func (i *Installer) markVerified(g Generation) {
	if i.verifyCache != nil {
		i.verifyCache[g.Path] = nil
	}
}

// Prefers the active generation when several digests exist for one version
// (a mirror re-serving different bytes creates siblings, never overwrites).
func (i *Installer) intactGeneration(version string) (Generation, bool) {
	var candidates []string
	// Only generations under the CURRENT releases dir qualify: after an
	// installDir change the active generation still exists at its old path,
	// but the desired state is a generation under the new one.
	if active := i.readState().Active; active != nil && active.Version == version &&
		path.Dir(active.GenerationPath) == i.cfg.releasesDir() {
		candidates = append(candidates, active.GenerationPath)
	}
	pattern := filepath.Join(i.cfg.hostPath(i.cfg.releasesDir()), version+"-*")
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		candidates = append(candidates, path.Join(i.cfg.releasesDir(), filepath.Base(m)))
	}
	seen := map[string]bool{}
	for _, genPath := range candidates {
		if seen[genPath] {
			continue
		}
		seen[genPath] = true
		g, err := i.loadGeneration(genPath)
		if err != nil || g.Version != version {
			continue
		}
		if err := i.verifyGenerationCached(g); err != nil {
			i.log.Warn("generation failed verification, will reinstall from archive", "generation", genPath, "error", err)
			continue
		}
		return g, true
	}
	return Generation{}, false
}

// The .sha512 proves the transfer was not corrupted, never that it was
// authentic: anything that can serve the artifact can serve a matching
// digest, and redirects are constrained to https, not to the original host.
// Authenticity rests on trusting the configured origin.
func (i *Installer) downloadArchive(ctx context.Context, urlBase, dir string) (string, string, error) {
	url := urlBase + "/" + archiveName
	sumBody, err := httpGet(ctx, url+".sha512")
	if err != nil {
		return "", "", archiveUnavailableHint(err)
	}
	expected, err := parseSHA512File(sumBody)
	if err != nil {
		return "", "", fmt.Errorf("malformed sha512 file for %s: %w", url, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
		if resp.StatusCode == http.StatusNotFound {
			err = archiveUnavailableHint(err)
		}
		return "", "", err
	}

	f, err := os.CreateTemp(dir, tempPrefix+"archive-")
	if err != nil {
		return "", "", err
	}
	// A misbehaving mirror must not fill the host disk before the checksum
	// rejects it.
	const maxArchiveSize = 1 << 30
	h := sha512.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(contextReader{ctx, resp.Body}, maxArchiveSize+1))
	closeErr := f.Close()
	if copyErr == nil && n > maxArchiveSize {
		copyErr = fmt.Errorf("archive exceeds %d bytes", int64(maxArchiveSize))
	}
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil {
		if got := hex.EncodeToString(h.Sum(nil)); got != expected {
			copyErr = fmt.Errorf("sha512 mismatch for %s: got %s, want %s", url, got, expected)
		}
	}
	if copyErr != nil {
		_ = os.Remove(f.Name())
		return "", "", copyErr
	}
	return f.Name(), expected, nil
}

func archiveUnavailableHint(err error) error {
	if strings.Contains(err.Error(), "404") {
		return fmt.Errorf("%w. The archive is unavailable at the configured downloadURLBase for this version; if this is a mirror, ensure it mirrors %s and its .sha512", err, archiveName)
	}
	return err
}

// sha512sum format: `<hex>  <name>`. The name must be the archive's, so a
// mirror publishing a checksum for some other artifact fails loudly.
func parseSHA512File(body []byte) (string, error) {
	fields := strings.Fields(strings.TrimSpace(string(body)))
	if len(fields) != 2 {
		return "", fmt.Errorf("expected \"<sha512>  %s\", got %q", archiveName, truncateOutput(body))
	}
	sum := strings.ToLower(fields[0])
	if len(sum) != sha512HexLen || !isHex(sum) {
		return "", fmt.Errorf("%q is not a sha512 hex digest", fields[0])
	}
	if name := strings.TrimPrefix(fields[1], "*"); name != archiveName {
		return "", fmt.Errorf("checksum is for %q, expected %q", fields[1], archiveName)
	}
	return sum, nil
}

func fileSHA512(p string) (string, error) {
	f, err := os.Open(p) //nolint:gosec // fixed names under the managed install dir
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha512.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Bounds each request independently of the reconcile context.
var downloadClient = &http.Client{
	Timeout: 10 * time.Minute,
	// Startup validation only sees the configured base, so an https origin
	// could still redirect the artifact and its checksum to cleartext.
	// Supplying CheckRedirect also drops Go's default cap, hence the count.
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to non-https URL %s", req.URL.Redacted())
		}
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		return nil
	},
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// Files only: this also runs against /etc/containerd, which the installer
// does not own.
func removeStaleTemps(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, tempPrefix+"*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		if fi, err := os.Lstat(m); err == nil && !fi.IsDir() {
			_ = os.Remove(m)
		}
	}
}

// Recursive, so only ever pointed at the installer-owned releases dir.
func sweepStaleStaging(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, tempPrefix+"*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		_ = os.RemoveAll(m)
	}
}

func readJSONFile(p string, v any) error {
	data, err := os.ReadFile(p) //nolint:gosec // fixed name under the managed install dir
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSONFile(p string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFileAtomic(p, data)
}

// writeFileAtomic keeps readers from ever seeing a partial file. It preserves
// an existing file's mode and creates new ones 0644.
func writeFileAtomic(p string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(p); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), tempPrefix+path.Base(p)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	var syncErr error
	if writeErr == nil {
		syncErr = tmp.Sync()
	}
	closeErr := tmp.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		return errors.Join(writeErr, syncErr, closeErr)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	// Without the directory fsync a power loss can reorder the journal and
	// config renames the crash-recovery contract depends on.
	return syncDir(filepath.Dir(p))
}

func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // installer-owned directories
	if err != nil {
		return err
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
