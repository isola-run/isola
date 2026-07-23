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
	"strings"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	valid := func(t *testing.T) {
		t.Setenv("NODE_NAME", "n1")
		t.Setenv("GVISOR_VERSION", "20260101.0")
	}

	t.Run("defaults", func(t *testing.T) {
		valid(t)
		cfg, err := ConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Handler != "runsc" || cfg.InstallDir != "/opt/isola/bin" || cfg.HostRoot != "/host" {
			t.Errorf("unexpected defaults: %+v", cfg)
		}
		if cfg.ReconcileInterval != 5*time.Minute || cfg.RetryInterval != time.Minute {
			t.Errorf("unexpected default intervals: %+v", cfg)
		}
		if cfg.ReconcileTimeout != defaultReconcileTimeout {
			t.Errorf("ReconcileTimeout = %v, want %v", cfg.ReconcileTimeout, defaultReconcileTimeout)
		}
		if !strings.HasPrefix(cfg.DownloadURLBase, "https://") {
			t.Errorf("default download origin must be https: %+v", cfg)
		}
	})

	t.Run("interval override", func(t *testing.T) {
		valid(t)
		t.Setenv("RECONCILE_INTERVAL", "30s")
		t.Setenv("RECONCILE_TIMEOUT", "45m")
		cfg, err := ConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ReconcileInterval != 30*time.Second {
			t.Errorf("ReconcileInterval = %v", cfg.ReconcileInterval)
		}
		if cfg.ReconcileTimeout != 45*time.Minute {
			t.Errorf("ReconcileTimeout = %v", cfg.ReconcileTimeout)
		}
	})

	for _, tt := range []struct {
		name    string
		mutate  func(t *testing.T)
		wantErr string
	}{
		{"missing node name", func(t *testing.T) { t.Setenv("NODE_NAME", "") }, "NODE_NAME"},
		{"missing version", func(t *testing.T) { t.Setenv("GVISOR_VERSION", "") }, "GVISOR_VERSION"},
		{"latest rejected", func(t *testing.T) { t.Setenv("GVISOR_VERSION", "latest") }, "dated release"},
		{"handler with uppercase", func(t *testing.T) { t.Setenv("GVISOR_HANDLER", "Runsc") }, "GVISOR_HANDLER"},
		{"handler with marker injection", func(t *testing.T) { t.Setenv("GVISOR_HANDLER", "x# END isola") }, "GVISOR_HANDLER"},
		{"handler with leading hyphen", func(t *testing.T) { t.Setenv("GVISOR_HANDLER", "-runsc") }, "GVISOR_HANDLER"},
		{"relative install dir", func(t *testing.T) { t.Setenv("GVISOR_INSTALL_DIR", "opt/bin") }, "absolute"},
		{"plain http origin", func(t *testing.T) {
			t.Setenv("GVISOR_DOWNLOAD_URL_BASE", "http://mirror.internal/gvisor")
		}, "absolute https URL"},
		{"non-http scheme", func(t *testing.T) {
			t.Setenv("GVISOR_DOWNLOAD_URL_BASE", "file:///var/mirror/gvisor")
		}, "absolute https URL"},
		{"scheme-relative origin", func(t *testing.T) {
			t.Setenv("GVISOR_DOWNLOAD_URL_BASE", "//mirror.internal/gvisor")
		}, "absolute https URL"},
		{"host-less origin", func(t *testing.T) {
			t.Setenv("GVISOR_DOWNLOAD_URL_BASE", "https:///gvisor")
		}, "absolute https URL"},
		{"unparsable origin", func(t *testing.T) {
			t.Setenv("GVISOR_DOWNLOAD_URL_BASE", "https://mirror.internal/%zz")
		}, "not a valid URL"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			valid(t)
			tt.mutate(t)
			_, err := ConfigFromEnv()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}

	for _, tt := range []struct {
		name    string
		urlBase string
	}{
		{"https origin", "https://mirror.internal/gvisor"},
		{"uppercase https scheme", "HTTPS://mirror.internal/gvisor"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			valid(t)
			t.Setenv("GVISOR_DOWNLOAD_URL_BASE", tt.urlBase)
			cfg, err := ConfigFromEnv()
			if err != nil {
				t.Fatalf("expected %q to be accepted, got: %v", tt.urlBase, err)
			}
			if cfg.DownloadURLBase != tt.urlBase {
				t.Errorf("DownloadURLBase = %q, want %q", cfg.DownloadURLBase, tt.urlBase)
			}
		})
	}
}
