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

// Package gvisorinstaller implements the per-node gVisor auto-installer that
// runs as a DaemonSet pod: it downloads runsc and its containerd shim,
// registers the containerd runtime handler, and labels the node once the
// runtime is verified healthy. It reconciles periodically so node drift
// (deleted binaries, reverted config) is self-healed.
package gvisorinstaller

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/isola-run/isola/internal/env"
)

// Host-side paths the installer manages. They are fixed by design: the
// containerd config location is the vanilla-containerd default (k3s/RKE2-style
// distros that relocate it are detected and rejected, see distro.go), and the
// shim config lives next to it under an isola-prefixed name so a pre-existing
// user-managed /etc/containerd/runsc.toml is never touched.
const (
	containerdConfigPath = "/etc/containerd/config.toml"
	// Write-once pristine copy of config.toml taken before the first isola
	// edit. Never deleted; documented escape hatch for manual recovery.
	containerdConfigBackupPath = "/etc/containerd/config.toml.isola-orig"
	runscShimConfigPath        = "/etc/containerd/isola-runsc.toml"
	criSocketPath              = "/run/containerd/containerd.sock"
)

// Config holds the desired state for one node, sourced from the DaemonSet
// pod environment (rendered by the Helm chart).
type Config struct {
	// NodeName is the node this pod runs on (downward API).
	NodeName string
	// Version is the gVisor release to install, e.g. "20260608.0".
	Version string
	// DownloadURLBase is the release artifact base URL; artifacts are fetched
	// from <base>/<version>/<arch>/<binary>.
	DownloadURLBase string
	// Handler is the containerd runtime handler name (RuntimeClass.handler).
	Handler string
	// InstallDir is the host directory for runsc + containerd-shim-runsc-v1.
	InstallDir string
	// HostRoot is the container path under which host paths are mounted.
	HostRoot string
	// RunscConfigSrc is the mounted ConfigMap file holding the base runsc
	// shim configuration to install on the host.
	RunscConfigSrc string
	// HealthAddr is the listen address for the healthz/readyz server.
	HealthAddr string

	ReconcileInterval time.Duration
	RetryInterval     time.Duration
}

// ConfigFromEnv builds the Config from environment variables, applying
// defaults that match the Helm chart's values.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		NodeName:        os.Getenv("NODE_NAME"),
		Version:         os.Getenv("GVISOR_VERSION"),
		DownloadURLBase: env.GetOrDefault("GVISOR_DOWNLOAD_URL_BASE", "https://storage.googleapis.com/gvisor/releases/release"),
		Handler:         env.GetOrDefault("GVISOR_HANDLER", "runsc"),
		InstallDir:      env.GetOrDefault("GVISOR_INSTALL_DIR", "/opt/isola/bin"),
		HostRoot:        env.GetOrDefault("HOST_ROOT", "/host"),
		RunscConfigSrc:  env.GetOrDefault("RUNSC_CONFIG_SRC", "/etc/isola-gvisor/runsc.toml"),
		HealthAddr:      env.GetOrDefault("HEALTH_ADDR", ":8093"),

		ReconcileInterval: envDuration("RECONCILE_INTERVAL", 5*time.Minute),
		RetryInterval:     envDuration("RETRY_INTERVAL", time.Minute),
	}

	if cfg.NodeName == "" {
		return cfg, errors.New("NODE_NAME is required")
	}
	if cfg.Version == "" {
		return cfg, errors.New("GVISOR_VERSION is required")
	}
	// "latest" is a moving target the version check could never converge on:
	// the installed binary would always report a dated release and trigger an
	// endless reinstall loop.
	if cfg.Version == "latest" {
		return cfg, errors.New(`GVISOR_VERSION must be a dated release (e.g. "20260608.0"), not "latest"`)
	}
	if !path.IsAbs(cfg.InstallDir) {
		return cfg, fmt.Errorf("GVISOR_INSTALL_DIR must be an absolute path, got %q", cfg.InstallDir)
	}
	return cfg, nil
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// hostPath translates an absolute host path to its mount location inside the
// container (e.g. /etc/containerd/config.toml -> /host/etc/containerd/config.toml).
func (c Config) hostPath(p string) string {
	return path.Join(c.HostRoot, p)
}

// CRISocketPath is the containerd CRI socket as mounted inside the container.
func (c Config) CRISocketPath() string { return c.hostPath(criSocketPath) }

// runscHostPath is the runsc binary path as seen by the host (used in the
// shim config's binary_name and for state inspection).
func (c Config) runscPath() string { return path.Join(c.InstallDir, "runsc") }

// shimPath is the containerd-shim-runsc-v1 path as seen by the host (used as
// runtime_path in the containerd runtime entry).
func (c Config) shimPath() string { return path.Join(c.InstallDir, "containerd-shim-runsc-v1") }
