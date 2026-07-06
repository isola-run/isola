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
	"time"
)

// healthyUnitShow is the `systemctl show containerd` output of a standard
// node; MainPID matches the fake /proc entry written by newTestEnv.
const healthyUnitShow = "LoadState=loaded\nActiveState=active\nMainPID=1234\n"

// fakeHost scripts host-command behavior. The dump result is recomputed from
// the current on-disk config by default, mimicking `containerd config dump`.
type fakeHost struct {
	mu sync.Mutex
	// distroPaths marks paths that `test -e` reports as existing.
	distroPaths map[string]bool
	// unitShow is returned for `systemctl show containerd ...`.
	unitShow string
	showErr  error
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
		if len(args) > 0 && args[0] == "show" {
			return []byte(f.unitShow), f.showErr
		}
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

func (f *fakeNode) hasEvent(reason string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.events {
		if strings.Contains(e, reason) {
			return true
		}
	}
	return false
}

type testEnv struct {
	i    *Installer
	host *fakeHost
	cri  *fakeCRI
	node *fakeNode

	downloads atomic.Int64
}

// newTestEnv assembles an installer against a fake node: a temp host root
// seeded with a kind-style containerd config, a fake release bucket, a fake
// /proc for the preflight, and fake host/CRI/node collaborators. The default
// dump reflects the on-disk config: it reports a runsc handler only once the
// managed block has been written, and the fake CRI serves whatever the
// on-disk config declares after each restart.
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

	procRoot := filepath.Join(t.TempDir(), "proc")
	if err := os.MkdirAll(filepath.Join(procRoot, "1234"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, "1234", "cmdline"), []byte("/usr/local/bin/containerd\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	i.procRoot = procRoot

	env.host = &fakeHost{
		unitShow: healthyUnitShow,
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

	// Second reconcile: fully idempotent — no downloads, no restart.
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

	// Externally deleted labels are healed on the next reconcile.
	if err := env.node.SetNodeLabels(t.Context(), map[string]string{LabelGVisorReady: ""}); err != nil {
		t.Fatal(err)
	}
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Errorf("externally deleted ready label not healed: %q", got)
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

// A crash between writing config.toml and restarting containerd leaves the
// desired config on disk but a live daemon that never loaded it. The next
// reconcile must converge by restarting containerd.
func TestReconcileConvergesAfterCrashBeforeRestart(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Simulate the post-crash state on a fresh process: config on disk is
	// correct, but the daemon still serves only runc.
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)
	fresh := newInstallerLike(env.i)

	if err := fresh.Reconcile(t.Context()); err != nil {
		t.Fatalf("reconcile did not converge: %v", err)
	}
	if env.host.restarts != 2 {
		t.Errorf("restarts = %d, want 2 (initial install + convergence restart)", env.host.restarts)
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Errorf("ready label = %q", got)
	}
}

// If the convergence restart does not make containerd serve the handler,
// the installer must not keep restarting containerd every reconcile.
func TestReconcileWedgeRestartsOnlyOnce(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	// The daemon never picks up the handler, no matter how often we restart.
	env.host.onRestart = func() {
		env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)
	}
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)
	fresh := newInstallerLike(env.i)

	if err := fresh.Reconcile(t.Context()); err == nil {
		t.Fatal("expected error when handler is not served after restart")
	}
	restartsAfterFirst := env.host.restarts
	if restartsAfterFirst != 2 {
		t.Fatalf("restarts = %d, want 2", restartsAfterFirst)
	}
	if err := fresh.Reconcile(t.Context()); err == nil {
		t.Fatal("expected error on retry")
	}
	if env.host.restarts != restartsAfterFirst {
		t.Errorf("retry restarted containerd again (restarts = %d)", env.host.restarts)
	}
	if got := env.node.label(LabelGVisorReady); got == "true" {
		t.Error("node labeled ready while handler is not served")
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

// A foreign handler that is in the config file but not served by the live
// daemon (config edited without a containerd restart) must be reported as a
// failure, not silently accepted.
func TestReconcileForeignHandlerNotServed(t *testing.T) {
	env := newTestEnv(t)
	foreign := kindStyleConfig + `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
`
	if err := os.WriteFile(env.i.cfg.hostPath(containerdConfigPath), []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}
	// Live daemon does not serve runsc.
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "runsc") {
		t.Fatalf("expected not-served error, got: %v", err)
	}
	if env.host.restarts != 0 {
		t.Error("containerd restarted for a foreign install")
	}
	if got := env.node.label(LabelGVisorReady); got == "true" {
		t.Error("node labeled ready while foreign handler is not served")
	}
	if !env.node.hasEvent("GVisorInstallFailed") {
		t.Error("no GVisorInstallFailed event emitted")
	}
}

// A same-named handler with a non-gVisor runtime_type must fail AND must not
// label the node ready, even though the live daemon serves a handler by that
// name (this is the security boundary: pods would run without gVisor).
func TestReconcileForeignHandlerConflict(t *testing.T) {
	env := newTestEnv(t)
	conflict := kindStyleConfig + `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.kata.v2"
`
	if err := os.WriteFile(env.i.cfg.hostPath(containerdConfigPath), []byte(conflict), 0o600); err != nil {
		t.Fatal(err)
	}
	// Real containerd lists every configured handler by name, including the
	// conflicting one.
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc", "runsc"}}, nil)

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
	if got := env.node.label(LabelGVisorReady); got == "true" {
		t.Error("node labeled gVisor-ready while the handler is not gVisor")
	}
}

// Old containerd that does not report RuntimeHandlers cannot be verified;
// the installer must fail with an actionable error rather than trusting the
// on-disk config.
func TestReconcileRequiresRuntimeHandlers(t *testing.T) {
	env := newTestEnv(t)
	env.host.onRestart = func() {
		env.cri.set(criStatus{RuntimeReady: true, Handlers: nil}, nil)
	}

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "1.7.15") {
		t.Fatalf("expected containerd version error, got: %v", err)
	}
	if got := env.node.label(LabelGVisorReady); got == "true" {
		t.Error("node labeled ready without live handler verification")
	}
}

// Old containerd is refused in preflight, BEFORE anything on the node is
// mutated or downloaded.
func TestPreflightRequiresRuntimeHandlers(t *testing.T) {
	env := newTestEnv(t)
	env.cri.set(criStatus{RuntimeReady: true, Handlers: nil}, nil)

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "1.7.15") {
		t.Fatalf("expected containerd version error, got: %v", err)
	}
	if env.configOnDisk(t) != kindStyleConfig {
		t.Error("config modified on unverifiable containerd")
	}
	if env.downloads.Load() != 0 {
		t.Error("binaries downloaded on unverifiable containerd")
	}
	if env.host.restarts != 0 {
		t.Error("containerd restarted on unverifiable containerd")
	}
}

// A drop-in import can override the managed handler entry with a different
// runtime_type in the merged config; the node must flip to not-ready even
// though the managed block is intact and CRI serves the handler name.
func TestReconcileImportOverrideConflict(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Fatalf("precondition: ready label = %q", got)
	}

	// Merged view (file + imports) now backs the handler with runc.
	env.host.dumpFunc = func() ([]byte, error) {
		return []byte(kindStyleConfig + `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runc.v2"
`), nil
	}

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got: %v", err)
	}
	if got := env.node.label(LabelGVisorReady); got != "false" {
		t.Errorf("ready label = %q, want false after import override", got)
	}
}

// An import can also keep runtime_type intact but repoint the handler at a
// different shim binary or shim config; the node must flip to not-ready even
// though the type check passes and CRI serves the handler name.
func TestReconcileImportOverridesManagedEntry(t *testing.T) {
	overridden := map[string]string{
		"runtime_path": `  runtime_type = "io.containerd.runsc.v1"
  runtime_path = "/usr/local/bin/rogue-shim"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc.options]
  TypeUrl = "io.containerd.runsc.v1.options"
  ConfigPath = "/etc/containerd/isola-runsc.toml"
`,
		"options.ConfigPath": `  runtime_type = "io.containerd.runsc.v1"
  runtime_path = "/opt/isola/bin/containerd-shim-runsc-v1"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc.options]
  TypeUrl = "io.containerd.runsc.v1.options"
  ConfigPath = "/etc/containerd/rogue-runsc.toml"
`,
	}
	for field, entry := range overridden {
		t.Run(field, func(t *testing.T) {
			env := newTestEnv(t)
			if err := env.i.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := env.node.label(LabelGVisorReady); got != "true" {
				t.Fatalf("precondition: ready label = %q", got)
			}
			restartsBefore := env.host.restarts

			// Merged view (file + imports) backs the handler with our
			// runtime_type but a foreign shim path or shim config.
			env.host.dumpFunc = func() ([]byte, error) {
				return []byte(kindStyleConfig +
					`[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]` + "\n" + entry), nil
			}

			err := env.i.Reconcile(t.Context())
			if err == nil || !strings.Contains(err.Error(), field) {
				t.Fatalf("expected override error naming %q, got: %v", field, err)
			}
			if got := env.node.label(LabelGVisorReady); got != "false" {
				t.Errorf("ready label = %q, want false after import override", got)
			}
			if env.host.restarts != restartsBefore {
				t.Error("containerd restarted for an import override it cannot fix")
			}
		})
	}
}

// An import override must be caught even when it coincides with a managed
// block transition: the pre-write path check is skipped while the block is
// stale, so the post-rewrite recheck must flag the node before it is ever
// labeled ready (not one reconcile later).
func TestReconcileImportOverrideDuringTransition(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Fatalf("precondition: ready label = %q", got)
	}

	// A drop-in import repoints runtime_path; emulate containerd's merge by
	// serving the on-disk config with the managed block's entry replaced by
	// the import's values.
	cfgPath := env.i.cfg.hostPath(containerdConfigPath)
	env.host.dumpFunc = func() ([]byte, error) {
		raw, err := os.ReadFile(cfgPath) //nolint:gosec // test fixture path
		if err != nil {
			return nil, err
		}
		stripped, err := spliceManagedBlock(raw, "")
		if err != nil {
			return nil, err
		}
		return append(stripped, []byte(`[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
  runtime_path = "/usr/local/bin/rogue-shim"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc.options]
  TypeUrl = "io.containerd.runsc.v1.options"
  ConfigPath = "/etc/containerd/isola-runsc.toml"
`)...), nil
	}
	// Simultaneously, a transition makes the on-disk block stale, so the
	// pre-write path check cannot run this round.
	env.i.cfg.InstallDir = "/opt/isola/bin-v2"

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "runtime_path") {
		t.Fatalf("expected override error after transition, got: %v", err)
	}
	if got := env.node.label(LabelGVisorReady); got != "false" {
		t.Errorf("ready label = %q, want false when the rewritten block is overridden", got)
	}
}

// A managed node whose binaries were wiped (and cannot be re-downloaded)
// must not stay schedulable: handler registration alone does not make
// sandboxes startable.
func TestReconcileNotReadyWhenBinariesMissing(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	installDir := env.i.cfg.hostPath(env.i.cfg.InstallDir)
	for _, name := range []string{runscBinary, shimBinary} {
		if err := os.Remove(filepath.Join(installDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	// The desired version is not downloadable, so the wipe cannot self-heal.
	env.i.cfg.Version = "20270101.0"

	if err := env.i.Reconcile(t.Context()); err == nil {
		t.Fatal("expected download error")
	}
	if got := env.node.label(LabelGVisorReady); got != "false" {
		t.Errorf("ready label = %q, want false with binaries missing", got)
	}
}

// After a successful convergence restart the one-restart budget resets, so a
// later regression (same config content) converges again.
func TestReconcileConvergeGuardResetsAfterSuccess(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	fresh := newInstallerLike(env.i)

	// First regression: daemon lost the handler.
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)
	if err := fresh.Reconcile(t.Context()); err != nil {
		t.Fatalf("first convergence failed: %v", err)
	}
	// Second regression must also converge (guard was reset on success).
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)
	if err := fresh.Reconcile(t.Context()); err != nil {
		t.Fatalf("second convergence failed: %v", err)
	}
	if env.host.restarts != 3 {
		t.Errorf("restarts = %d, want 3 (install + two convergence restarts)", env.host.restarts)
	}
}

func TestPreflightUnsupportedDistro(t *testing.T) {
	env := newTestEnv(t)
	env.host.unitShow = "LoadState=not-found\nActiveState=inactive\nMainPID=0\n"
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

func TestPreflightNoContainerdUnit(t *testing.T) {
	env := newTestEnv(t)
	env.host.unitShow = "LoadState=not-found\nActiveState=inactive\nMainPID=0\n"

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "systemd") {
		t.Fatalf("expected missing-unit error, got: %v", err)
	}
}

func TestPreflightNonStandardConfigPath(t *testing.T) {
	env := newTestEnv(t)
	if err := os.WriteFile(filepath.Join(env.i.procRoot, "1234", "cmdline"),
		[]byte("/usr/bin/containerd\x00--config\x00/var/lib/other/config.toml\x00"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "/var/lib/other/config.toml") {
		t.Fatalf("expected non-standard config path error, got: %v", err)
	}
	if env.configOnDisk(t) != kindStyleConfig {
		t.Error("config modified despite non-standard config path")
	}
}

func TestPreflightRunsOncePerProcess(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Break the preflight inputs: a cached preflight must not re-probe.
	env.host.unitShow = "LoadState=not-found\n"
	env.host.showErr = errors.New("should not be called again")
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatalf("cached preflight re-probed the host: %v", err)
	}
}

func TestConfigFlagFromCmdline(t *testing.T) {
	tests := []struct {
		name    string
		cmdline string
		want    string
	}{
		{"no flag", "/usr/bin/containerd\x00", ""},
		{"separate flag", "/usr/bin/containerd\x00--config\x00/x/c.toml\x00", "/x/c.toml"},
		{"equals flag", "/usr/bin/containerd\x00--config=/x/c.toml\x00", "/x/c.toml"},
		{"short flag", "/usr/bin/containerd\x00-c\x00/x/c.toml\x00", "/x/c.toml"},
		{"trailing flag without value", "/usr/bin/containerd\x00--config\x00", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configFlagFromCmdline([]byte(tt.cmdline)); got != tt.want {
				t.Errorf("configFlagFromCmdline(%q) = %q, want %q", tt.cmdline, got, tt.want)
			}
		})
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

func TestBackoffInterval(t *testing.T) {
	const base, maxWait = time.Minute, 5 * time.Minute
	tests := []struct {
		failures int
		want     time.Duration
	}{
		{1, base}, {2, 2 * time.Minute}, {3, 4 * time.Minute},
		{4, maxWait}, {5, maxWait}, {100, maxWait},
	}
	for _, tt := range tests {
		if got := backoffInterval(base, tt.failures, maxWait); got != tt.want {
			t.Errorf("backoffInterval(failures=%d) = %v, want %v", tt.failures, got, tt.want)
		}
	}
	// A cap below the base still wins (never exceed ReconcileInterval).
	if got := backoffInterval(time.Minute, 1, 30*time.Second); got != 30*time.Second {
		t.Errorf("cap below base: got %v, want 30s", got)
	}
}

// newInstallerLike builds a fresh Installer sharing cfg and collaborators,
// simulating a restarted pod process (no in-memory state carried over).
func newInstallerLike(prev *Installer) *Installer {
	fresh := New(prev.cfg, prev.log, prev.host, prev.cri, prev.node)
	fresh.healthWait = prev.healthWait
	fresh.healthPoll = prev.healthPoll
	fresh.procRoot = prev.procRoot
	return fresh
}
