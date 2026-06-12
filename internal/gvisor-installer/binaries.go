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

	// stateFileName records what was installed so reconciles can short-circuit
	// without re-downloading. Lives in the install dir so it survives pod
	// restarts and node reboots, and disappears together with the binaries.
	stateFileName = ".isola-gvisor-state.json"
)

// installState ties the configured release version to the version string the
// installed runsc binary actually reports. The indirection matters because
// the two differ in shape (config "20260608.0" vs binary "release-20260608.0"),
// and comparing the reported string against a recorded value also detects
// out-of-band binary swaps.
type installState struct {
	DesiredVersion  string `json:"desiredVersion"`
	ReportedVersion string `json:"reportedVersion"`
}

// gvisorArch maps the pod's GOARCH (which equals the node arch) to the gVisor
// release bucket's directory naming.
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

// runscReportedVersion executes the installed runsc binary (a static binary,
// so it runs fine inside the installer container) and returns its version
// token, e.g. "release-20260608.0".
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

// ensureBinaries makes sure the desired gVisor release is installed in the
// install dir, downloading and atomically replacing binaries when needed.
// Returns whether anything was (re)installed.
func (i *Installer) ensureBinaries(ctx context.Context) (bool, error) {
	installDir := i.cfg.hostPath(i.cfg.InstallDir)
	if err := os.MkdirAll(installDir, 0o755); err != nil { //nolint:gosec // binaries must be world-readable for containerd/shims
		return false, fmt.Errorf("creating install dir: %w", err)
	}
	removeStaleTemps(installDir)

	if i.binariesUpToDate(ctx, installDir) {
		return false, nil
	}

	arch, err := gvisorArch()
	if err != nil {
		return false, err
	}
	urlBase := fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(i.cfg.DownloadURLBase, "/"), i.cfg.Version, arch)
	i.log.Info("installing gVisor binaries", "version", i.cfg.Version, "url", urlBase, "dir", i.cfg.InstallDir)

	// Stage both binaries before promoting either, so a failed download never
	// leaves a half-upgraded pair in place.
	staged := make(map[string]string, 2)
	defer func() {
		for _, tmp := range staged {
			_ = os.Remove(tmp)
		}
	}()
	for _, name := range []string{runscBinary, shimBinary} {
		tmp := filepath.Join(installDir, ".isola-tmp."+name)
		if err := downloadVerified(ctx, urlBase+"/"+name, tmp); err != nil {
			return false, fmt.Errorf("downloading %s: %w", name, err)
		}
		staged[name] = tmp
	}

	// Sanity-check the staged runsc actually runs and reports a version
	// before it can ever be referenced by containerd.
	reported, err := runscReportedVersion(ctx, staged[runscBinary])
	if err != nil {
		return false, fmt.Errorf("staged runsc failed verification: %w", err)
	}

	// rename(2) is atomic and leaves running shims/sandboxes on the old inode;
	// only newly started sandboxes pick up the new binaries.
	for _, name := range []string{runscBinary, shimBinary} {
		if err := os.Rename(staged[name], filepath.Join(installDir, name)); err != nil {
			return false, fmt.Errorf("installing %s: %w", name, err)
		}
		delete(staged, name)
	}

	if err := writeJSONFile(filepath.Join(installDir, stateFileName), installState{
		DesiredVersion:  i.cfg.Version,
		ReportedVersion: reported,
	}); err != nil {
		return false, fmt.Errorf("recording install state: %w", err)
	}
	i.log.Info("gVisor binaries installed", "version", i.cfg.Version, "reportedVersion", reported)
	return true, nil
}

// binariesUpToDate reports whether the installed binaries already match the
// desired version: the state file must record the desired version AND the
// on-disk runsc must still report the version recorded at install time.
func (i *Installer) binariesUpToDate(ctx context.Context, installDir string) bool {
	var st installState
	if err := readJSONFile(filepath.Join(installDir, stateFileName), &st); err != nil {
		return false
	}
	if st.DesiredVersion != i.cfg.Version {
		return false
	}
	reported, err := runscReportedVersion(ctx, filepath.Join(installDir, runscBinary))
	if err != nil || reported != st.ReportedVersion {
		return false
	}
	fi, err := os.Stat(filepath.Join(installDir, shimBinary))
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}

// installedVersion returns the version label value for the current install:
// the recorded desired version when state and binary agree, otherwise the
// raw reported version of whatever runsc is on disk.
func (i *Installer) installedVersion(ctx context.Context) string {
	installDir := i.cfg.hostPath(i.cfg.InstallDir)
	if i.binariesUpToDate(ctx, installDir) {
		return i.cfg.Version
	}
	if reported, err := runscReportedVersion(ctx, filepath.Join(installDir, runscBinary)); err == nil {
		return strings.TrimPrefix(reported, "release-")
	}
	return ""
}

// downloadVerified fetches url into dest and verifies it against the .sha512
// checksum published next to the artifact. The checksum only proves
// integrity in transit (it comes from the same origin); authenticity rests
// on the HTTPS origin, same as gVisor's documented install flow.
func downloadVerified(ctx context.Context, url, dest string) error {
	sumBody, err := httpGet(ctx, url+".sha512")
	if err != nil {
		return err
	}
	expected, _, _ := strings.Cut(strings.TrimSpace(string(sumBody)), " ")
	if len(expected) != sha512.Size*2 {
		return fmt.Errorf("malformed sha512 file for %s: %q", url, truncateOutput(sumBody))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %s", url, resp.Status)
	}

	f, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755) //nolint:gosec // binaries must be world-executable
	if err != nil {
		return err
	}
	h := sha512.New()
	_, copyErr := io.Copy(io.MultiWriter(f, h), resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("downloading %s: %w", url, copyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != expected {
		_ = os.Remove(dest)
		return fmt.Errorf("sha512 mismatch for %s: got %s, want %s", url, got, expected)
	}
	return nil
}

// downloadClient bounds each artifact download independently of the reconcile
// context. runsc is ~130MB, so allow slow links a generous window.
var downloadClient = &http.Client{Timeout: 10 * time.Minute}

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

// removeStaleTemps cleans up staging files left by a previously interrupted
// install attempt.
func removeStaleTemps(dir string) {
	matches, err := filepath.Glob(filepath.Join(dir, ".isola-tmp.*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		_ = os.Remove(m)
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

// writeFileAtomic writes via a temp file + rename(2) in the same directory so
// readers (containerd, the shim) never observe a partial file. An existing
// file's mode is preserved; new files are created 0644.
func writeFileAtomic(p string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(p); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), "."+path.Base(p)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		return errors.Join(writeErr, closeErr)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
