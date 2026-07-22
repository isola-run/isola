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

// Package gvisorinstaller installs gVisor on each node from a DaemonSet pod
// and labels the node once the runtime is verified healthy. It reconciles
// periodically, so node drift (deleted binaries, reverted config) self-heals.
package gvisorinstaller

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/isola-run/isola/internal/env"
)

// The shim config is isola-prefixed so a user-managed
// /etc/containerd/runsc.toml is never clobbered.
const (
	containerdConfigPath       = "/etc/containerd/config.toml"
	containerdConfigBackupPath = "/etc/containerd/config.toml.isola-orig"
	runscShimConfigPath        = "/etc/containerd/isola-runsc.toml"
	criSocketPath              = "/run/containerd/containerd.sock"
)

// Config is the desired state for one node, sourced from the pod environment.
type Config struct {
	NodeName string
	Version  string
	// DownloadURLBase is the release artifact base URL; artifacts are fetched
	// from <base>/<version>/<arch>/<binary>. https only, no opt-out.
	DownloadURLBase string
	Handler         string
	InstallDir      string

	// Fixed by the image and DaemonSet spec, fields only so tests can redirect.
	HostRoot       string
	RunscConfigSrc string
	HealthAddr     string

	ReconcileInterval time.Duration
	RetryInterval     time.Duration
	ReconcileTimeout  time.Duration
}

// RFC 1123 DNS labels: the RuntimeClass grammar, and anything looser could
// corrupt the TOML splice the handler is rendered into.
var handlerRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ConfigFromEnv defaults must stay in sync with the Helm chart's values.
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
		ReconcileTimeout:  env.GetOrDefaultDuration("RECONCILE_TIMEOUT", defaultReconcileTimeout),
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
	if err := validateDownloadURLBase(cfg.DownloadURLBase); err != nil {
		return cfg, err
	}
	if !handlerRe.MatchString(cfg.Handler) {
		return cfg, fmt.Errorf("GVISOR_HANDLER %q must be a lowercase DNS label (letters, digits, hyphens)", cfg.Handler)
	}
	if !path.IsAbs(cfg.InstallDir) {
		return cfg, fmt.Errorf("GVISOR_INSTALL_DIR must be an absolute path, got %q", cfg.InstallDir)
	}
	return cfg, nil
}

// validateDownloadURLBase requires TLS: the .sha512 is same-origin, so it
// catches corruption but never substitution.
func validateDownloadURLBase(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("GVISOR_DOWNLOAD_URL_BASE %q is not a valid URL: %w", raw, err)
	}
	if strings.ToLower(u.Scheme) != "https" || u.Host == "" {
		return fmt.Errorf("GVISOR_DOWNLOAD_URL_BASE %q must be an absolute https URL: the installer runs this binary as root on every node", raw)
	}
	return nil
}

func (c Config) hostPath(p string) string {
	return path.Join(c.HostRoot, p)
}

func (c Config) CRISocketPath() string { return c.hostPath(criSocketPath) }

func (c Config) runscPath() string { return path.Join(c.InstallDir, "runsc") }

func (c Config) shimPath() string { return path.Join(c.InstallDir, "containerd-shim-runsc-v1") }
