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
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
)

const runscShimConfigHeader = "# Managed by the isola gvisor-installer, do not edit (changes are overwritten).\n" +
	"# Source of truth: the gvisor.installer values of the isola Helm release.\n"

// binary_name pinned to the generation's runsc is what keeps a sandbox on
// its generation for life: the shim persists it at sandbox create, so
// retained old generations keep old sandboxes release-consistent.
func renderRunscShimConfig(baseConfig []byte, runscPath string, systemdCgroup bool) ([]byte, error) {
	var doc map[string]any
	if err := toml.Unmarshal(baseConfig, &doc); err != nil {
		return nil, fmt.Errorf("parsing base runsc config: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	doc["binary_name"] = runscPath

	flags, ok := doc["runsc_config"].(map[string]any)
	if !ok {
		flags = map[string]any{}
		doc["runsc_config"] = flags
	}
	if _, set := flags["systemd-cgroup"]; !set {
		// runsc_config values are strings: the shim turns them into CLI flags.
		flags["systemd-cgroup"] = strconv.FormatBool(systemdCgroup)
	}

	var buf bytes.Buffer
	buf.WriteString(runscShimConfigHeader)
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return nil, fmt.Errorf("encoding runsc config: %w", err)
	}
	return buf.Bytes(), nil
}

func (i *Installer) desiredRunscShimConfig(g Generation, dump []byte) ([]byte, error) {
	base, err := os.ReadFile(i.cfg.RunscConfigSrc)
	if err != nil {
		return nil, fmt.Errorf("reading runsc config source: %w", err)
	}
	return renderRunscShimConfig(base, g.runscPath(), systemdCgroupFromDump(dump))
}

// Content-addressed, so the only write that can ever land on an existing path
// restores that path's own bytes. That matters because the shim re-reads this
// exact path for every later container in the pod, and a changed config there
// would change runsc flags under a live sentry.
func (i *Installer) ensureRunscConfig(g Generation, dump []byte) (string, error) {
	desired, err := i.desiredRunscShimConfig(g, dump)
	if err != nil {
		return "", err
	}
	sum := sha512.Sum512(desired)
	name := hex.EncodeToString(sum[:])[:32] + ".toml"
	configPath := path.Join(i.cfg.configsDir(), name)
	dest := i.cfg.hostPath(configPath)

	if regularFileEqual(dest, desired) {
		return configPath, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil { //nolint:gosec // layout must be readable by containerd and shims
		return "", err
	}
	// writeFileAtomic preserves the destination mode, which through a symlink
	// would be the target's.
	if fi, err := os.Lstat(dest); err == nil && !fi.Mode().IsRegular() {
		if err := os.Remove(dest); err != nil {
			return "", err
		}
	}
	i.log.Info("writing runsc shim config", "path", configPath)
	if err := writeFileAtomic(dest, desired); err != nil {
		return "", err
	}
	return configPath, nil
}

func regularFileEqual(p string, want []byte) bool {
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() {
		return false
	}
	current, err := os.ReadFile(p) //nolint:gosec // fixed managed path
	return err == nil && bytes.Equal(current, want)
}

func (i *Installer) runscConfigIntact(configPath string) bool {
	dest := i.cfg.hostPath(configPath)
	data, err := os.ReadFile(dest) //nolint:gosec // path recorded by this installer
	if err != nil {
		return false
	}
	if fi, err := os.Lstat(dest); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	sum := sha512.Sum512(data)
	return path.Base(configPath) == hex.EncodeToString(sum[:])[:32]+".toml"
}
