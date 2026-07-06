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
	"regexp"
	"time"

	"github.com/isola-run/isola/internal/env"
)

// Host-side paths the installer manages. They are fixed by design: the
// containerd config location is the vanilla-containerd default (the
// preflight rejects nodes whose containerd loads a different file), and the
// shim config lives next to it under an isola-prefixed name so a
// pre-existing user-managed /etc/containerd/runsc.toml is never touched.
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
	// Version is the gVisor release to install, e.g. "20260622.0".
	Version string
	// DownloadURLBase is the release artifact base URL; artifacts are fetched
	// from <base>/<version>/<arch>/<binary>.
	DownloadURLBase string
	// Handler is the containerd runtime handler name (RuntimeClass.handler).
	Handler string
	// InstallDir is the host directory for runsc + containerd-shim-runsc-v1.
	InstallDir string

	// The fields below are fixed by the container image and DaemonSet spec;
	// they exist as fields so tests can redirect them.
	HostRoot       string
	RunscConfigSrc string
	HealthAddr     string

	ReconcileInterval time.Duration
	RetryInterval     time.Duration
}

// handlerRe matches RFC 1123 DNS labels — the RuntimeClass handler grammar.
// Anything looser could also corrupt the TOML splice (the handler is
// rendered into the managed block and its markers).
var handlerRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ConfigFromEnv builds the Config from environment variables, applying
// defaults that match the Helm chart's values.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		NodeName:        os.Getenv("NODE_NAME"),
		Version:         os.Getenv("GVISOR_VERSION"),
		DownloadURLBase: env.GetOrDefault("GVISOR_DOWNLOAD_URL_BASE", "https://storage.googleapis.com/gvisor/releases/release"),
		Handler:         env.GetOrDefault("GVISOR_HANDLER", "runsc"),
		InstallDir:      env.GetOrDefault("GVISOR_INSTALL_DIR", "/opt/isola/bin"),

		HostRoot:       "/host",
		RunscConfigSrc: "/etc/isola-gvisor/runsc.toml",
		HealthAddr:     ":8093",

		ReconcileInterval: env.GetOrDefaultDuration("RECONCILE_INTERVAL", 5*time.Minute),
		RetryInterval:     env.GetOrDefaultDuration("RETRY_INTERVAL", time.Minute),
	}

	if cfg.NodeName == "" {
		return cfg, errors.New("NODE_NAME is required")
	}
	if cfg.Version == "" {
		return cfg, errors.New("GVISOR_VERSION is required")
	}
	// "latest" is a moving target the version check could never converge on:
	// the installed binaries would never match the recorded state and every
	// reconcile would reinstall.
	if cfg.Version == "latest" {
		return cfg, errors.New(`GVISOR_VERSION must be a dated release (e.g. "20260622.0"), not "latest"`)
	}
	if !handlerRe.MatchString(cfg.Handler) {
		return cfg, fmt.Errorf("GVISOR_HANDLER %q must be a lowercase DNS label (letters, digits, hyphens)", cfg.Handler)
	}
	if !path.IsAbs(cfg.InstallDir) {
		return cfg, fmt.Errorf("GVISOR_INSTALL_DIR must be an absolute path, got %q", cfg.InstallDir)
	}
	return cfg, nil
}

// hostPath translates an absolute host path to its mount location inside the
// container (e.g. /etc/containerd/config.toml -> /host/etc/containerd/config.toml).
func (c Config) hostPath(p string) string {
	return path.Join(c.HostRoot, p)
}

// CRISocketPath is the containerd CRI socket as mounted inside the container.
func (c Config) CRISocketPath() string { return c.hostPath(criSocketPath) }

// runscPath is the runsc binary path as seen by the host (used in the shim
// config's binary_name).
func (c Config) runscPath() string { return path.Join(c.InstallDir, "runsc") }

// shimPath is the containerd-shim-runsc-v1 path as seen by the host (used as
// runtime_path in the containerd runtime entry).
func (c Config) shimPath() string { return path.Join(c.InstallDir, "containerd-shim-runsc-v1") }
