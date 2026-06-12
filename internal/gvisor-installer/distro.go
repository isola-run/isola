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
)

// unsupportedDistroMarkers identifies Kubernetes distributions whose
// containerd configuration is NOT at /etc/containerd/config.toml or is
// regenerated from templates on every restart. Editing the standard path on
// such nodes would be silently ignored (or worse, fight the distro), so the
// installer refuses with a clear error instead of guessing.
var unsupportedDistroMarkers = []struct {
	distro string
	path   string
}{
	{"k3s", "/var/lib/rancher/k3s/agent/etc/containerd"},
	{"RKE2", "/var/lib/rancher/rke2/agent/etc/containerd"},
	{"MicroK8s", "/var/snap/microk8s/current/args"},
	{"k0s", "/etc/k0s/containerd.toml"},
}

// checkSupportedDistro fails when the node runs a distribution with a
// non-standard containerd config layout. Checks run through the host mount
// namespace because the installer only mounts narrow hostPaths.
func (i *Installer) checkSupportedDistro(ctx context.Context) error {
	for _, m := range unsupportedDistroMarkers {
		// `test -e` exits 1 when absent; treat any failure as "not present".
		if _, err := i.host.Run(ctx, "test", "-e", m.path); err == nil {
			return fmt.Errorf("node appears to run %s (%s exists), which manages containerd config outside %s; "+
				"gVisor auto-install does not support this distribution — install gVisor through the distribution's "+
				"own mechanism and set gvisor.autoInstall.enabled=false (or exclude this node with the %s=disabled label)",
				m.distro, m.path, containerdConfigPath, LabelGVisorInstall)
		}
	}
	return nil
}
