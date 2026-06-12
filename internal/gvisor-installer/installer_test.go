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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeHost scripts host-command behavior. The dump result is recomputed from
// the current on-disk config by default, mimicking `containerd config dump`.
type fakeHost struct {
	mu sync.Mutex
	// distroPaths marks paths that `test -e` reports as existing.
	distroPaths map[string]bool
	// dumpFunc returns the dump output; defaults provided by newTestEnv.
	dumpFunc func() ([]byte, error)
	// restartErr fails `systemctl restart containerd` when set.
	restartErr error
	restarts   int
	onRestart  func()
}

func (f *fakeHost) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch name {
	case "test":
		if len(args) == 2 && f.distroPaths[args[1]] {
			return nil, nil
		}
		return nil, errors.New("exit status 1")
	case "containerd":
		return f.dumpFunc()
	case "systemctl":
		f.restarts++
		if f.onRestart != nil {
			f.onRestart()
		}
		return nil, f.restartErr
	default:
		return nil, fmt.Errorf("unexpected host command %q", name)
	}
}

type fakeCRI struct {
	mu  sync.Mutex
	st  criStatus
	err error
}

func (f *fakeCRI) Status(context.Context) (criStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.st, f.err
}

func (f *fakeCRI) set(st criStatus, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.st, f.err = st, err
}

type fakeNode struct {
	mu     sync.Mutex
	labels map[string]string
	events []string
}

func (f *fakeNode) SetNodeLabels(_ context.Context, labels map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.labels == nil {
		f.labels = map[string]string{}
	}
	for k, v := range labels {
		if v == "" {
			delete(f.labels, k)
		} else {
			f.labels[k] = v
		}
	}
	return nil
}

func (f *fakeNode) Event(eventType, reason, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, eventType+"/"+reason+": "+message)
}

func (f *fakeNode) label(k string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.labels[k]
}

type testEnv struct {
	i    *Installer
	host *fakeHost
	cri  *fakeCRI
	node *fakeNode

	downloads atomic.Int64
}

// newTestEnv assembles an installer against a fake node: a temp host root
// seeded with a kind-style containerd config, a fake release bucket, and fake
// host/CRI/node collaborators. The default dump reflects the on-disk config:
// it reports a runsc handler only once the managed block has been written.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	const version = "20260101.0"
	env := &testEnv{}
	srv := gvisorReleaseServer(t, releaseFiles(t, version), &env.downloads)

	i := testInstaller(t, srv.URL)
	cfgPath := i.cfg.hostPath(containerdConfigPath)
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(kindStyleConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	runscSrc := filepath.Join(t.TempDir(), "runsc.toml")
	if err := os.WriteFile(runscSrc, []byte("[runsc_config]\n  allow-rootfs-tar-annotation = \"true\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	i.cfg.RunscConfigSrc = runscSrc

	env.host = &fakeHost{
		dumpFunc: func() ([]byte, error) {
			// Approximate `containerd config dump`: the on-disk file (which
			// is what our managed block lands in) parsed and re-served.
			return os.ReadFile(cfgPath) //nolint:gosec // test fixture path
		},
	}
	env.cri = &fakeCRI{}
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)
	env.host.onRestart = func() {
		// After a restart the runtime serves whatever the config declares.
		raw, _ := os.ReadFile(cfgPath) //nolint:gosec // test fixture path
		handlers := []string{"runc"}
		if _, found := runtimeFromDump(raw, i.cfg.Handler); found {
			handlers = append(handlers, i.cfg.Handler)
		}
		env.cri.set(criStatus{RuntimeReady: true, Handlers: handlers}, nil)
	}
	env.node = &fakeNode{}

	i.host = env.host
	i.cri = env.cri
	i.node = env.node
	env.i = i
	return env
}

func (e *testEnv) configOnDisk(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(e.i.cfg.hostPath(containerdConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestReconcileFreshInstall(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}

	cfg := env.configOnDisk(t)
	if !strings.HasPrefix(cfg, kindStyleConfig) {
		t.Error("original config content modified")
	}
	if !strings.Contains(cfg, beginMarker) {
		t.Error("managed block missing")
	}
	if env.host.restarts != 1 {
		t.Errorf("restarts = %d, want 1", env.host.restarts)
	}
	if _, err := os.Stat(env.i.cfg.hostPath(containerdConfigBackupPath)); err != nil {
		t.Errorf("pristine backup missing: %v", err)
	}
	shim, err := os.ReadFile(env.i.cfg.hostPath(runscShimConfigPath))
	if err != nil {
		t.Fatalf("shim config missing: %v", err)
	}
	for _, want := range []string{"binary_name", "allow-rootfs-tar-annotation", `systemd-cgroup = "true"`} {
		if !strings.Contains(string(shim), want) {
			t.Errorf("shim config missing %q:\n%s", want, shim)
		}
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Errorf("ready label = %q", got)
	}
	if got := env.node.label(LabelGVisorVersion); got != "20260101.0" {
		t.Errorf("version label = %q", got)
	}
	if !env.i.Ready() {
		t.Error("installer not ready after successful reconcile")
	}

	// Second reconcile: fully idempotent — no downloads, no restart, no churn.
	before := env.downloads.Load()
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if env.host.restarts != 1 {
		t.Errorf("idempotent reconcile restarted containerd (restarts = %d)", env.host.restarts)
	}
	if env.downloads.Load() != before {
		t.Error("idempotent reconcile re-downloaded binaries")
	}
}

func TestReconcileHealsRemovedManagedBlock(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Simulate drift: someone reverts the config.
	cfgPath := env.i.cfg.hostPath(containerdConfigPath)
	if err := os.WriteFile(cfgPath, []byte(kindStyleConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)

	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env.configOnDisk(t), beginMarker) {
		t.Error("managed block not restored")
	}
	if env.host.restarts != 2 {
		t.Errorf("restarts = %d, want 2", env.host.restarts)
	}
}

func TestReconcileForeignInstall(t *testing.T) {
	env := newTestEnv(t)
	// The node already has a runsc handler not managed by isola.
	foreign := kindStyleConfig + `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
`
	cfgPath := env.i.cfg.hostPath(containerdConfigPath)
	if err := os.WriteFile(cfgPath, []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc", "runsc"}}, nil)

	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if env.configOnDisk(t) != foreign {
		t.Error("foreign config was modified")
	}
	if env.host.restarts != 0 {
		t.Error("containerd restarted for a foreign install")
	}
	if env.downloads.Load() != 0 {
		t.Error("binaries downloaded for a foreign install")
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Errorf("ready label = %q", got)
	}
	if got := env.node.label(LabelGVisorVersion); got != VersionUnmanaged {
		t.Errorf("version label = %q, want %q", got, VersionUnmanaged)
	}
}

func TestReconcileForeignHandlerConflict(t *testing.T) {
	env := newTestEnv(t)
	conflict := kindStyleConfig + `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.kata.v2"
`
	if err := os.WriteFile(env.i.cfg.hostPath(containerdConfigPath), []byte(conflict), 0o600); err != nil {
		t.Fatal(err)
	}
	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got: %v", err)
	}
	if env.configOnDisk(t) != conflict {
		t.Error("conflicting config was modified")
	}
	if env.i.Ready() {
		t.Error("installer ready despite conflict")
	}
}

func TestReconcileUnsupportedDistro(t *testing.T) {
	env := newTestEnv(t)
	env.host.distroPaths = map[string]bool{"/var/lib/rancher/k3s/agent/etc/containerd": true}

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "k3s") {
		t.Fatalf("expected k3s error, got: %v", err)
	}
	if env.configOnDisk(t) != kindStyleConfig {
		t.Error("config modified on unsupported distro")
	}
	if env.downloads.Load() != 0 {
		t.Error("binaries downloaded on unsupported distro")
	}
}

func TestReconcileUnsupportedConfigVersion(t *testing.T) {
	env := newTestEnv(t)
	if err := os.WriteFile(env.i.cfg.hostPath(containerdConfigPath), []byte("[plugins]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "unsupported schema version") {
		t.Fatalf("expected schema version error, got: %v", err)
	}
	if env.configOnDisk(t) != "[plugins]\n" {
		t.Error("v1 config was modified")
	}
}

func TestReconcileRollsBackWhenContainerdUnhealthy(t *testing.T) {
	env := newTestEnv(t)
	// First restart leaves the runtime broken; the rollback restart heals it.
	env.host.onRestart = func() {
		if env.host.restarts == 1 {
			env.cri.set(criStatus{}, errors.New("connection refused"))
		} else {
			env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)
		}
	}

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected rollback error, got: %v", err)
	}
	if env.configOnDisk(t) != kindStyleConfig {
		t.Errorf("config not rolled back:\n%s", env.configOnDisk(t))
	}
	if env.host.restarts != 2 {
		t.Errorf("restarts = %d, want 2 (change + rollback)", env.host.restarts)
	}
	if env.i.Ready() {
		t.Error("installer ready despite failed install")
	}
	if got := env.node.label(LabelGVisorReady); got != "false" {
		t.Errorf("ready label = %q, want false", got)
	}
}

func TestReconcileRestoresConfigWhenValidationFails(t *testing.T) {
	env := newTestEnv(t)
	cfgPath := env.i.cfg.hostPath(containerdConfigPath)
	dumps := 0
	env.host.dumpFunc = func() ([]byte, error) {
		dumps++
		if dumps == 1 {
			return os.ReadFile(cfgPath) //nolint:gosec // test fixture path // pre-check of the current config succeeds
		}
		return nil, errors.New("config parse error") // post-write validation fails
	}

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "failed validation") {
		t.Fatalf("expected validation error, got: %v", err)
	}
	if env.configOnDisk(t) != kindStyleConfig {
		t.Error("config not restored after validation failure")
	}
	if env.host.restarts != 0 {
		t.Error("containerd restarted despite validation failure")
	}
}

func TestReconcileBrokenPreexistingConfigUntouched(t *testing.T) {
	env := newTestEnv(t)
	env.host.dumpFunc = func() ([]byte, error) { return nil, errors.New("invalid TOML") }

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "refusing to modify") {
		t.Fatalf("expected refusal, got: %v", err)
	}
	if env.configOnDisk(t) != kindStyleConfig {
		t.Error("broken-config node was modified")
	}
	if env.host.restarts != 0 {
		t.Error("containerd restarted on a node with broken config")
	}
}

func TestReconcileKeepsReadyLabelWhenUpgradeDownloadFails(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Fatalf("precondition: ready label = %q", got)
	}

	// Bump to a version the bucket does not have: download fails, but the
	// node still has a fully working runtime — it must stay schedulable.
	env.i.cfg.Version = "20270101.0"
	err := env.i.Reconcile(t.Context())
	if err == nil {
		t.Fatal("expected download error")
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Errorf("ready label lost on failed upgrade: %q", got)
	}
	if env.i.Ready() {
		t.Error("readiness should reflect the failed reconcile")
	}
	if got := env.node.label(LabelGVisorVersion); got != "20260101.0" {
		t.Errorf("version label = %q, want the still-installed version", got)
	}
}
