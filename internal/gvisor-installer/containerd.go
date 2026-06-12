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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Marker comments delimiting the isola-managed section of config.toml.
// Everything between them is owned by the installer and replaced wholesale on
// reconcile; everything outside them is never modified. This avoids
// round-tripping the user's config through a TOML library (which would
// reorder tables and drop comments).
const (
	beginMarker = "# BEGIN isola-managed gVisor runtime (managed by isola gvisor-installer; do not edit)"
	endMarker   = "# END isola-managed gVisor runtime"
)

const (
	// CRI plugin IDs per containerd config schema version. Config version 2
	// (containerd 1.x, still accepted by 2.x via auto-migration) vs version
	// 3+ (containerd 2.x, where the CRI plugin was split).
	criPluginIDV2 = "io.containerd.grpc.v1.cri"
	criPluginIDV3 = "io.containerd.cri.v1.runtime"

	runscRuntimeType = "io.containerd.runsc.v1"
)

var versionLineRe = regexp.MustCompile(`^\s*version\s*=\s*([0-9]+)`)

// configSchemaVersion returns the root-level `version = N` of a containerd
// config file. Only lines before the first table header count: a `version`
// key inside a table belongs to that table, not the root. Returns 0 if absent
// (containerd then treats the file as a legacy version 1 config).
func configSchemaVersion(data []byte) int {
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break
		}
		if m := versionLineRe.FindStringSubmatch(line); m != nil {
			v, err := strconv.Atoi(m[1])
			if err != nil {
				return 0
			}
			return v
		}
	}
	return 0
}

// criPluginID maps a config schema version to the CRI runtime plugin ID used
// in the runtimes table path.
func criPluginID(schemaVersion int) (string, error) {
	switch {
	case schemaVersion == 2:
		return criPluginIDV2, nil
	case schemaVersion >= 3:
		return criPluginIDV3, nil
	default:
		return "", fmt.Errorf("containerd config declares unsupported schema version %d: "+
			"only version 2 (containerd 1.x+) and version 3+ (containerd 2.x) are supported; "+
			"a missing version field means a legacy v1 config", schemaVersion)
	}
}

// renderManagedBlock renders the runtime handler registration that goes
// between the markers. runtime_path pins the shim binary location so it does
// not need to be on containerd's $PATH, and ConfigPath points the shim at the
// isola-managed runsc configuration.
func renderManagedBlock(pluginID, handler, shimPath, shimConfigPath string) string {
	return fmt.Sprintf(`[plugins.%q.containerd.runtimes.%q]
  runtime_type = %q
  runtime_path = %q
  # Allow gVisor annotations (e.g. dev.gvisor.flag.*, mount hints) to reach runsc.
  pod_annotations = ["dev.gvisor.*"]
[plugins.%q.containerd.runtimes.%q.options]
  TypeUrl = "io.containerd.runsc.v1.options"
  ConfigPath = %q`,
		pluginID, handler, runscRuntimeType, shimPath,
		pluginID, handler, shimConfigPath)
}

// managedBlock extracts the current content between the markers. Returns
// found=false when no markers are present. A begin marker without an end
// marker is reported as an error rather than guessed at.
func managedBlock(data []byte) (block string, found bool, err error) {
	s := string(data)
	bi := strings.Index(s, beginMarker)
	if bi < 0 {
		return "", false, nil
	}
	rest := s[bi+len(beginMarker):]
	ei := strings.Index(rest, endMarker)
	if ei < 0 {
		return "", false, fmt.Errorf("%s: begin marker present but end marker missing; refusing to touch the file", containerdConfigPath)
	}
	return strings.Trim(rest[:ei], "\n"), true, nil
}

// spliceManagedBlock returns data with the managed block inserted (appended)
// or replaced in place. The rest of the file is preserved byte-for-byte.
func spliceManagedBlock(data []byte, block string) ([]byte, error) {
	s := string(data)
	section := beginMarker + "\n" + block + "\n" + endMarker

	bi := strings.Index(s, beginMarker)
	if bi < 0 {
		if !strings.HasSuffix(s, "\n") && s != "" {
			s += "\n"
		}
		return []byte(s + "\n" + section + "\n"), nil
	}
	rest := s[bi:]
	ei := strings.Index(rest, endMarker)
	if ei < 0 {
		return nil, fmt.Errorf("%s: begin marker present but end marker missing; refusing to touch the file", containerdConfigPath)
	}
	return []byte(s[:bi] + section + s[bi+ei+len(endMarker):]), nil
}

// tomlLookup walks a decoded TOML document along the given keys.
func tomlLookup(doc map[string]any, keys ...string) (any, bool) {
	var cur any = doc
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// runtimeFromDump looks up a runtime handler entry in `containerd config
// dump` output. Both plugin IDs are checked: containerd 2.x migrates the
// dumped config to the newest schema regardless of the on-disk version.
func runtimeFromDump(dump []byte, handler string) (map[string]any, bool) {
	var doc map[string]any
	if err := toml.Unmarshal(dump, &doc); err != nil {
		return nil, false
	}
	for _, pluginID := range []string{criPluginIDV2, criPluginIDV3} {
		if v, ok := tomlLookup(doc, "plugins", pluginID, "containerd", "runtimes", handler); ok {
			if m, ok := v.(map[string]any); ok {
				return m, true
			}
		}
	}
	return nil, false
}

// systemdCgroupFromDump reports whether the node's default runc runtime uses
// the systemd cgroup driver, which gVisor must match (runsc's systemd-cgroup
// flag). Defaults to false (runc's own default) when undetectable.
func systemdCgroupFromDump(dump []byte) bool {
	var doc map[string]any
	if err := toml.Unmarshal(dump, &doc); err != nil {
		return false
	}
	for _, pluginID := range []string{criPluginIDV2, criPluginIDV3} {
		if v, ok := tomlLookup(doc, "plugins", pluginID, "containerd", "runtimes", "runc", "options", "SystemdCgroup"); ok {
			if b, ok := v.(bool); ok {
				return b
			}
		}
	}
	return false
}
