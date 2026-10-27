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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestReconcileRepairsGenerationDirectoryModes(t *testing.T) {
	for _, scope := range []string{"install root", "releases", "generation", "sidecars"} {
		t.Run(scope, func(t *testing.T) {
			env := newTestEnv(t)
			if err := env.i.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			active := env.activeGen(t)
			gen, err := env.i.loadGeneration(active.GenerationPath)
			if err != nil {
				t.Fatal(err)
			}
			dir := gen.hostDir(env.i.cfg)
			switch scope {
			case "install root":
				dir = env.i.cfg.hostPath(env.i.cfg.InstallDir)
			case "releases":
				dir = filepath.Dir(dir)
			case "sidecars":
				dir = filepath.Join(dir, sidecarDirName)
			}
			if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // restrictive directory mode is the regression fixture
				t.Fatal(err)
			}
			if err := env.i.verifyGeneration(gen); err == nil {
				t.Fatal("generation verified with a directory inaccessible to unprivileged sentry")
			}
			restarts := env.host.restarts
			if err := env.i.Reconcile(t.Context()); err != nil {
				t.Fatalf("repair reconcile failed: %v", err)
			}
			fi, err := os.Stat(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := fi.Mode().Perm(); got != 0o755 {
				t.Errorf("directory mode = %04o, want 0755", got)
			}
			if !env.i.Ready() || env.node.label(LabelGVisorReady) != "true" {
				t.Error("node not ready after repairing directory permissions")
			}
			if env.host.restarts != restarts {
				t.Error("directory permission repair restarted containerd")
			}
		})
	}
}

func TestReconcileDirectoryModeDriftStaysUnreadyUntilRepair(t *testing.T) {
	for _, scope := range []string{"generation", "sidecars"} {
		t.Run(scope, func(t *testing.T) {
			env := newTestEnv(t)
			if err := env.i.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			active := env.activeGen(t)
			dir := env.i.cfg.hostPath(active.GenerationPath)
			if scope == "sidecars" {
				dir = filepath.Join(dir, sidecarDirName)
			}
			if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // restrictive directory mode is the regression fixture
				t.Fatal(err)
			}
			unavailable := httptest.NewServer(http.NotFoundHandler())
			defer unavailable.Close()
			originalURL := env.i.cfg.DownloadURLBase
			env.i.cfg.DownloadURLBase = unavailable.URL
			restarts := env.host.restarts
			if err := env.i.Reconcile(t.Context()); err == nil {
				t.Fatal("expected repair download to fail")
			}
			if env.i.Ready() || env.node.label(LabelGVisorReady) != "false" {
				t.Error("node remains ready with inaccessible generation directory")
			}
			env.i.cfg.DownloadURLBase = originalURL
			if err := env.i.Reconcile(t.Context()); err != nil {
				t.Fatalf("retry after restoring archive availability failed: %v", err)
			}
			if !env.i.Ready() || env.node.label(LabelGVisorReady) != "true" {
				t.Error("node did not recover readiness after directory repair")
			}
			if env.host.restarts != restarts {
				t.Error("directory permission recovery restarted containerd")
			}
		})
	}
}
