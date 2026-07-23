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
	"fmt"
	"os"
	"strconv"

	"github.com/BurntSushi/toml"
)

const runscShimConfigHeader = "# Managed by the isola gvisor-installer, do not edit (changes are overwritten).\n" +
	"# Source of truth: the gvisor.installer values of the isola Helm release.\n"

// renderRunscShimConfig pins binary_name because the shim otherwise looks for
// "runsc" on containerd's $PATH, which the install dir is deliberately not on.
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

// ensureRunscShimConfig needs no containerd restart: the shim reads
// ConfigPath at sandbox start, so changes apply to new sandboxes only.
func (i *Installer) ensureRunscShimConfig(dump []byte) error {
	base, err := os.ReadFile(i.cfg.RunscConfigSrc)
	if err != nil {
		return fmt.Errorf("reading runsc config source: %w", err)
	}
	desired, err := renderRunscShimConfig(base, i.cfg.runscPath(), systemdCgroupFromDump(dump))
	if err != nil {
		return err
	}

	dest := i.cfg.hostPath(runscShimConfigPath)
	current, err := os.ReadFile(dest) //nolint:gosec // fixed managed path
	if err == nil && bytes.Equal(current, desired) {
		return nil
	}
	i.log.Info("writing runsc shim config", "path", runscShimConfigPath)
	return writeFileAtomic(dest, desired)
}
