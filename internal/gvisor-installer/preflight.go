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
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// knownDistroMarkers only names the distro in error messages. The real gate
// is preflight's live-process check, which also catches unlisted ones.
var knownDistroMarkers = []struct {
	distro string
	path   string
}{
	{"k3s", "/var/lib/rancher/k3s/agent/etc/containerd"},
	{"RKE2", "/var/lib/rancher/rke2/agent/etc/containerd"},
	{"MicroK8s", "/var/snap/microk8s/current/args"},
	{"k0s", "/etc/k0s/containerd.toml"},
}

// preflight refuses any layout other than a systemd-managed containerd
// loading /etc/containerd/config.toml, where editing that file would be
// silently ignored and the restart would hit the wrong unit. It observes the
// live process rather than enumerating distros, so unknown setups are caught
// too. A pass is cached: the layout cannot change under a running pod.
func (i *Installer) preflight(ctx context.Context) error {
	if i.preflightOK {
		return nil
	}
	out, err := i.host.Run(ctx, "systemctl", "show", "containerd", "--property=LoadState,ActiveState,MainPID")
	if err != nil {
		return fmt.Errorf("querying the containerd systemd unit: %w%s", err, i.distroHint(ctx))
	}
	props := parseProperties(out)
	if props["LoadState"] != "loaded" {
		return fmt.Errorf("no containerd systemd unit on this node (LoadState=%s), gVisor auto-install requires a standard systemd-managed containerd%s",
			props["LoadState"], i.distroHint(ctx))
	}
	if props["ActiveState"] != "active" {
		return fmt.Errorf("containerd systemd unit is not active (ActiveState=%s)", props["ActiveState"])
	}
	pid, err := strconv.Atoi(props["MainPID"])
	if err != nil || pid <= 0 {
		return fmt.Errorf("containerd systemd unit reports no main PID (MainPID=%q)", props["MainPID"])
	}
	// The host's procfs is directly readable because the pod runs with hostPID.
	cmdline, err := os.ReadFile(filepath.Join(i.procRoot, strconv.Itoa(pid), "cmdline")) //nolint:gosec // host procfs, PID from systemd
	if err != nil {
		return fmt.Errorf("reading the containerd process cmdline: %w", err)
	}
	if cfg := configFlagFromCmdline(cmdline); cfg != "" && cfg != containerdConfigPath {
		return fmt.Errorf("containerd runs with a non-standard config (--config %s), gVisor auto-install only manages %s%s",
			cfg, containerdConfigPath, i.distroHint(ctx))
	}
	// Refuse before mutating anything we could never verify afterwards.
	st, err := i.cri.Status(ctx, true)
	if err != nil {
		return fmt.Errorf("querying containerd CRI status: %w", err)
	}
	if len(st.Handlers) == 0 {
		return errNoRuntimeHandlers
	}
	i.preflightOK = true
	i.log.Info("preflight passed: standard systemd-managed containerd", "mainPID", pid)
	return nil
}

func parseProperties(out []byte) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			props[k] = v
		}
	}
	return props
}

// configFlagFromCmdline extracts the config path from a containerd process
// cmdline (NUL-separated argv). Empty means the default path is in use.
func configFlagFromCmdline(cmdline []byte) string {
	args := strings.Split(string(cmdline), "\x00")
	for n, a := range args {
		switch {
		case a == "-c" || a == "--config":
			if n+1 < len(args) {
				return args[n+1]
			}
		case strings.HasPrefix(a, "--config="):
			return strings.TrimPrefix(a, "--config=")
		case strings.HasPrefix(a, "-c="):
			return strings.TrimPrefix(a, "-c=")
		}
	}
	return ""
}

func (i *Installer) distroHint(ctx context.Context) string {
	for _, m := range knownDistroMarkers {
		// Any failure means "not present".
		if _, err := i.host.Run(ctx, "test", "-e", m.path); err == nil {
			return fmt.Sprintf(" (node appears to run %s, which manages containerd itself: install gVisor through the distribution's own mechanism and set gvisor.installer.enabled=false, or exclude this node with the %s=disabled label)",
				m.distro, LabelGVisorInstall)
		}
	}
	return ""
}
