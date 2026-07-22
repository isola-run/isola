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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

// MainPID matches the fake /proc entry newTestEnv writes.
const healthyUnitShow = "LoadState=loaded\nActiveState=active\nMainPID=1234\n"

// fakeHost scripts host-command behavior.
type fakeHost struct {
	mu sync.Mutex
	// distroPaths marks paths that `test -e` reports as existing.
	distroPaths map[string]bool
	// unitShow is returned for `systemctl show containerd ...`.
	unitShow string
	showErr  error
	// dumpFunc returns the dump output.
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

// hostFunc makes every host command behave the same way.
type hostFunc func(context.Context, string, ...string) ([]byte, error)

func (f hostFunc) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}

type fakeCRI struct {
	mu  sync.Mutex
	st  criStatus
	err error
}

func (f *fakeCRI) Status(_ context.Context, withHandlers bool) (criStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !withHandlers {
		return criStatus{RuntimeReady: f.st.RuntimeReady}, f.err
	}
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
// /proc for the preflight, and fake host/CRI/node collaborators.
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
			// Approximates `containerd config dump` from the on-disk file.
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
	backup, err := os.Stat(env.i.cfg.hostPath(containerdConfigBackupPath))
	if err != nil {
		t.Errorf("pristine backup missing: %v", err)
	} else if backup.Mode().Perm() != 0o600 {
		// newTestEnv writes config.toml 0600; the copy must not widen it.
		t.Errorf("pristine backup mode = %v, want 0600", backup.Mode().Perm())
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

// A crash between the config write and the restart leaves a daemon that never
// loaded it. The next reconcile must converge.
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

// Configured but not served (edited without a restart) must fail, not be
// silently accepted.
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

// The security boundary: a same-named non-gVisor handler must never label the
// node ready, or pods run unsandboxed.
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

// A runtime exposing no handler list cannot be verified, so fail rather than
// trust the on-disk config.
func TestReconcileRequiresRuntimeHandlers(t *testing.T) {
	env := newTestEnv(t)
	env.host.onRestart = func() {
		env.cri.set(criStatus{RuntimeReady: true, Handlers: nil}, nil)
	}

	err := env.i.Reconcile(t.Context())
	if !errors.Is(err, errNoRuntimeHandlers) {
		t.Fatalf("expected unverifiable-runtime error, got: %v", err)
	}
	if got := env.node.label(LabelGVisorReady); got == "true" {
		t.Error("node labeled ready without live handler verification")
	}
}

// Such a runtime is refused in preflight, BEFORE anything on the node is
// mutated or downloaded.
func TestPreflightRequiresRuntimeHandlers(t *testing.T) {
	env := newTestEnv(t)
	env.cri.set(criStatus{RuntimeReady: true, Handlers: nil}, nil)

	err := env.i.Reconcile(t.Context())
	if !errors.Is(err, errNoRuntimeHandlers) {
		t.Fatalf("expected unverifiable-runtime error, got: %v", err)
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

// containerd1xPluginConfig is written out literally rather than marshalled,
// so the test pins containerd 1.x's actual wire shape.
func containerd1xPluginConfig(handlers ...string) string {
	entries := make([]string, 0, len(handlers))
	for _, h := range handlers {
		entries = append(entries, fmt.Sprintf(
			`%q:{"runtimeType":"io.containerd.runsc.v1","runtimePath":"/opt/isola/bin/containerd-shim-runsc-v1"}`, h))
	}
	return `{"containerd":{"snapshotter":"overlayfs","defaultRuntimeName":"runc","runtimes":{` +
		strings.Join(entries, ",") + `}},"rootDir":"/var/lib/containerd/io.containerd.grpc.v1.cri"}`
}

func readyConditions() *runtimeapi.RuntimeStatus {
	return &runtimeapi.RuntimeStatus{Conditions: []*runtimeapi.RuntimeCondition{
		{Type: runtimeapi.RuntimeReady, Status: true},
		{Type: runtimeapi.NetworkReady, Status: true},
	}}
}

// The handler list must be read from RuntimeHandlers on containerd 2.x and
// from the verbose plugin config on containerd 1.x, without ever guessing:
// anything else yields no handlers, which callers turn into a hard failure.
func TestStatusFromRPCHandlerSources(t *testing.T) {
	tests := []struct {
		name         string
		withHandlers bool
		plain        *runtimeapi.StatusResponse
		verbose      *runtimeapi.StatusResponse
		verboseErr   error
		want         []string
		wantVerbose  int
		wantErr      string
	}{
		{
			name:         "containerd 2.x reports RuntimeHandlers",
			withHandlers: true,
			plain: &runtimeapi.StatusResponse{Status: readyConditions(), RuntimeHandlers: []*runtimeapi.RuntimeHandler{
				{Name: "runc"}, {Name: "runsc"},
			}},
			want:        []string{"runc", "runsc"},
			wantVerbose: 0,
		},
		{
			name:         "containerd 1.x reports the handler in its verbose plugin config",
			withHandlers: true,
			plain:        &runtimeapi.StatusResponse{Status: readyConditions()},
			verbose: &runtimeapi.StatusResponse{Status: readyConditions(), Info: map[string]string{
				"config": containerd1xPluginConfig("runc", "runsc"),
				"golang": `"go1.21.13"`,
			}},
			want:        []string{"runc", "runsc"},
			wantVerbose: 1,
		},
		{
			name:         "containerd 1.x without the handler configured",
			withHandlers: true,
			plain:        &runtimeapi.StatusResponse{Status: readyConditions()},
			verbose: &runtimeapi.StatusResponse{Status: readyConditions(), Info: map[string]string{
				"config": containerd1xPluginConfig("runc"),
			}},
			want:        []string{"runc"},
			wantVerbose: 1,
		},
		{
			name:         "verbose config absent",
			withHandlers: true,
			plain:        &runtimeapi.StatusResponse{Status: readyConditions()},
			verbose:      &runtimeapi.StatusResponse{Status: readyConditions(), Info: map[string]string{"golang": `"go1.21.13"`}},
			want:         nil,
			wantVerbose:  1,
		},
		{
			name:         "verbose config malformed",
			withHandlers: true,
			plain:        &runtimeapi.StatusResponse{Status: readyConditions()},
			verbose: &runtimeapi.StatusResponse{Status: readyConditions(), Info: map[string]string{
				"config": `{"containerd":{"runtimes":`,
			}},
			wantVerbose: 1,
			wantErr:     "parsing the CRI plugin config",
		},
		{
			name:         "verbose config with an empty runtimes table",
			withHandlers: true,
			plain:        &runtimeapi.StatusResponse{Status: readyConditions()},
			verbose: &runtimeapi.StatusResponse{Status: readyConditions(), Info: map[string]string{
				"config": containerd1xPluginConfig(),
			}},
			want:        nil,
			wantVerbose: 1,
		},
		{
			// `containerd config dump` nests the same table under
			// plugins."io.containerd.grpc.v1.cri"; that shape is NOT what the
			// CRI plugin reports, and must not be silently accepted.
			name:         "dump-shaped config is not accepted",
			withHandlers: true,
			plain:        &runtimeapi.StatusResponse{Status: readyConditions()},
			verbose: &runtimeapi.StatusResponse{Status: readyConditions(), Info: map[string]string{
				"config": `{"plugins":{"io.containerd.grpc.v1.cri":{"containerd":{"runtimes":{"runsc":{}}}}}}`,
			}},
			want:        nil,
			wantVerbose: 1,
		},
		{
			name:         "verbose status call fails",
			withHandlers: true,
			plain:        &runtimeapi.StatusResponse{Status: readyConditions()},
			verboseErr:   errors.New("connection refused"),
			wantVerbose:  1,
			wantErr:      "connection refused",
		},
		{
			// Pollers that only wait for RuntimeReady must never make
			// containerd marshal its whole plugin config.
			name:         "handlers not requested",
			withHandlers: false,
			plain:        &runtimeapi.StatusResponse{Status: readyConditions()},
			want:         nil,
			wantVerbose:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verboseCalls := 0
			call := func(_ context.Context, verbose bool) (*runtimeapi.StatusResponse, error) {
				if !verbose {
					return tt.plain, nil
				}
				verboseCalls++
				if tt.verboseErr != nil {
					return nil, tt.verboseErr
				}
				return tt.verbose, nil
			}

			st, err := statusFromRPC(t.Context(), call, tt.withHandlers)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if !st.RuntimeReady {
				t.Error("RuntimeReady not propagated")
			}
			if !slices.Equal(st.Handlers, tt.want) {
				t.Errorf("handlers = %v, want %v", st.Handlers, tt.want)
			}
			if verboseCalls != tt.wantVerbose {
				t.Errorf("verbose status calls = %d, want %d", verboseCalls, tt.wantVerbose)
			}
		})
	}
}

// containerd1xCRI is a CRIClient backed by containerd 1.x-shaped Status
// responses: RuntimeHandlers is never set, so the served handlers are only
// discoverable through the verbose plugin config. Unlike fakeCRI it runs the
// real response translation, exercising the fallback end to end.
type containerd1xCRI struct {
	mu       sync.Mutex
	handlers []string
	verbose  int
}

func (c *containerd1xCRI) set(handlers ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers = handlers
}

func (c *containerd1xCRI) verboseCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.verbose
}

func (c *containerd1xCRI) Status(ctx context.Context, withHandlers bool) (criStatus, error) {
	return statusFromRPC(ctx, func(_ context.Context, verbose bool) (*runtimeapi.StatusResponse, error) {
		resp := &runtimeapi.StatusResponse{Status: readyConditions()}
		if verbose {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.verbose++
			resp.Info = map[string]string{"config": containerd1xPluginConfig(c.handlers...)}
		}
		return resp, nil
	}, withHandlers)
}

// containerd 1.x never populates StatusResponse.RuntimeHandlers, so the
// installer must fall back to the verbose plugin config — and a full install
// must succeed and label the node ready on such a node.
func TestReconcileOnContainerd1x(t *testing.T) {
	env := newTestEnv(t)
	cri := &containerd1xCRI{handlers: []string{"runc"}}
	env.i.cri = cri
	cfgPath := env.i.cfg.hostPath(containerdConfigPath)
	env.host.onRestart = func() {
		raw, _ := os.ReadFile(cfgPath) //nolint:gosec // test fixture path
		handlers := []string{"runc"}
		if _, found := runtimeFromDump(raw, env.i.cfg.Handler); found {
			handlers = append(handlers, env.i.cfg.Handler)
		}
		cri.set(handlers...)
	}

	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatalf("install failed on containerd 1.x: %v", err)
	}
	if cri.verboseCalls() == 0 {
		t.Error("verbose CRI status never requested; the containerd 1.x fallback did not run")
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Errorf("ready label = %q, want true on containerd 1.x", got)
	}
	if got := env.node.label(LabelGVisorVersion); got != "20260101.0" {
		t.Errorf("version label = %q", got)
	}
}

// The 1.x fallback must stay fail-closed: a daemon whose plugin config does
// not list the handler is not serving it, however healthy it looks.
func TestReconcileOnContainerd1xHandlerAbsent(t *testing.T) {
	env := newTestEnv(t)
	// The daemon reports only runc, no matter how often it is restarted.
	env.i.cri = &containerd1xCRI{handlers: []string{"runc"}}

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "not served by containerd") {
		t.Fatalf("expected not-served error, got: %v", err)
	}
	if errors.Is(err, errNoRuntimeHandlers) {
		t.Error("a handler list was available; the error must be 'not served', not 'unverifiable'")
	}
	if got := env.node.label(LabelGVisorReady); got == "true" {
		t.Error("node labeled ready while the handler is absent from the plugin config")
	}
}

// A containerd 1.x daemon whose plugin config lists no runtimes at all (or
// reports none) is unverifiable, and must be refused in preflight before
// anything on the node is touched.
func TestPreflightRefusesContainerd1xWithoutHandlers(t *testing.T) {
	env := newTestEnv(t)
	env.i.cri = &containerd1xCRI{}

	err := env.i.Reconcile(t.Context())
	if !errors.Is(err, errNoRuntimeHandlers) {
		t.Fatalf("expected unverifiable-runtime error, got: %v", err)
	}
	if env.configOnDisk(t) != kindStyleConfig {
		t.Error("config modified on an unverifiable containerd")
	}
	if env.downloads.Load() != 0 {
		t.Error("binaries downloaded on an unverifiable containerd")
	}
	if env.host.restarts != 0 {
		t.Error("containerd restarted on an unverifiable containerd")
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

// The config snapshot a reconcile starts from can be minutes old by the time
// the managed block is spliced into it (binary downloads sit in between). An
// edit made in that window must abort the write, not be silently reverted.
func TestReconcileAbortsWhenConfigChangedUnderIt(t *testing.T) {
	env := newTestEnv(t)
	cfgPath := env.i.cfg.hostPath(containerdConfigPath)
	const adminEdit = "\n# added by the node's config management\n"

	// `containerd config dump` runs after the snapshot is taken and before
	// ensureBinaries, so editing the file from here lands in exactly the
	// window the staleness check protects.
	edited := false
	env.host.dumpFunc = func() ([]byte, error) {
		raw, err := os.ReadFile(cfgPath) //nolint:gosec // test fixture path
		if err != nil {
			return nil, err
		}
		if !edited {
			edited = true
			if err := os.WriteFile(cfgPath, append(raw, adminEdit...), 0o600); err != nil {
				return nil, err
			}
		}
		return raw, nil
	}

	err := env.i.Reconcile(t.Context())
	if err == nil || !strings.Contains(err.Error(), "changed while this reconcile was running") {
		t.Fatalf("expected stale-snapshot abort, got: %v", err)
	}
	if got := env.configOnDisk(t); got != kindStyleConfig+adminEdit {
		t.Errorf("concurrent edit was clobbered:\n%s", got)
	}
	if env.host.restarts != 0 {
		t.Error("containerd restarted on the abort path")
	}
	if _, err := os.Stat(env.i.cfg.hostPath(containerdConfigBackupPath)); !os.IsNotExist(err) {
		t.Error("aborted reconcile still wrote to the config dir")
	}
	if got := env.node.label(LabelGVisorReady); got == "true" {
		t.Error("node labeled ready without a managed block on disk")
	}

	// The next reconcile recomputes from the current bytes and converges,
	// keeping the foreign edit.
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatalf("follow-up reconcile: %v", err)
	}
	cfg := env.configOnDisk(t)
	if !strings.Contains(cfg, adminEdit) || !strings.Contains(cfg, beginMarker) {
		t.Errorf("follow-up reconcile did not converge on the edited config:\n%s", cfg)
	}
	if got := env.node.label(LabelGVisorReady); got != "true" {
		t.Errorf("ready label = %q after convergence", got)
	}
}

// The pristine backup is a verbatim copy of a file that may be 0600 and may
// carry inline registry credentials, so it must inherit the source's mode
// rather than writeFileAtomic's 0644 default for new files.
func TestWritePristineBackupPreservesMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o640} {
		t.Run(mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "config.toml")
			backup := filepath.Join(dir, "config.toml.isola-orig")
			if err := os.WriteFile(src, []byte("pristine"), mode); err != nil {
				t.Fatal(err)
			}
			// Independent of the process umask.
			if err := os.Chmod(src, mode); err != nil {
				t.Fatal(err)
			}

			if err := writePristineBackup(backup, src, []byte("pristine")); err != nil {
				t.Fatal(err)
			}
			fi, err := os.Stat(backup)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != mode {
				t.Errorf("backup mode = %v, want %v", fi.Mode().Perm(), mode)
			}

			// Write-once: a later reconcile must not overwrite the original.
			if err := writePristineBackup(backup, src, []byte("no longer pristine")); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(backup) //nolint:gosec // test fixture path
			if err != nil || string(data) != "pristine" {
				t.Errorf("backup content = %q (err %v)", data, err)
			}
		})
	}
}

func TestWritePristineBackupFallsBackToOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "config.toml.isola-orig")
	// Source mode unknowable: the copy must not be created world-readable.
	if err := writePristineBackup(backup, filepath.Join(dir, "gone.toml"), []byte("secret")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(backup)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("backup mode = %v, want 0600", fi.Mode().Perm())
	}
}

// A host command that never returns must not wedge the loop: each iteration
// is bounded, so status keeps being republished.
func TestRunBoundsEachReconcileIteration(t *testing.T) {
	env := newTestEnv(t)
	var calls atomic.Int64
	env.i.host = hostFunc(func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		calls.Add(1)
		<-ctx.Done() // a wedged systemctl: only the deadline ends this
		return nil, ctx.Err()
	})
	env.i.reconcileTimeout = 30 * time.Millisecond
	env.i.cfg.ReconcileInterval = 10 * time.Millisecond
	env.i.cfg.RetryInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		env.i.Run(ctx)
	}()

	deadline := time.After(5 * time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("reconcile loop wedged after %d iterations", calls.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got := env.node.label(LabelGVisorReady); got == "true" {
		t.Error("node labeled ready while every host command hangs")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

// Liveness must catch a loop that stopped completing iterations (an
// unkillable host command outlives the per-iteration deadline), and must
// clear again once an iteration completes.
func TestHealthzWatchdogFailsOnStalledLoop(t *testing.T) {
	env := newTestEnv(t)
	env.i.stallThreshold = time.Hour
	srv := httptest.NewServer(healthHandler(env.i))
	t.Cleanup(srv.Close)

	get := func(path string) int {
		t.Helper()
		resp, err := http.Get(srv.URL + path) //nolint:noctx // short-lived test probe
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode
	}

	if got := get("/healthz"); got != http.StatusOK {
		t.Errorf("fresh installer: /healthz = %d, want 200", got)
	}
	env.i.lastReconcileDone.Store(time.Now().Add(-2 * time.Hour).UnixNano())
	if got := get("/healthz"); got != http.StatusServiceUnavailable {
		t.Errorf("stalled loop: /healthz = %d, want 503", got)
	}
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := get("/healthz"); got != http.StatusOK {
		t.Errorf("after a completed reconcile: /healthz = %d, want 200", got)
	}
}

// A failing reconcile still completes, so the watchdog must not fire on it:
// only a wedged loop may restart the pod.
func TestHealthzWatchdogIgnoresFailingReconciles(t *testing.T) {
	env := newTestEnv(t)
	env.i.stallThreshold = time.Hour
	env.host.dumpFunc = func() ([]byte, error) { return nil, errors.New("invalid TOML") }
	env.i.lastReconcileDone.Store(time.Now().Add(-2 * time.Hour).UnixNano())

	if err := env.i.Reconcile(t.Context()); err == nil {
		t.Fatal("expected reconcile error")
	}
	if stalled, silent := env.i.Stalled(); stalled {
		t.Errorf("watchdog fired after a completed (failing) reconcile: silent for %v", silent)
	}
	if env.i.Ready() {
		t.Error("readiness must still reflect the failure")
	}
}

// The default watchdog threshold must clear a full slow install plus a normal
// wait, or long-running installs would restart themselves in a loop.
func TestStallThresholdClearsSlowInstalls(t *testing.T) {
	i := New(Config{ReconcileInterval: 5 * time.Minute}, slog.New(slog.DiscardHandler), nil, nil, nil)
	if floor := i.reconcileTimeout + i.cfg.ReconcileInterval; i.stallThreshold <= floor {
		t.Errorf("stallThreshold = %v, must exceed %v (bounded install + one interval)", i.stallThreshold, floor)
	}
	// The bound on one iteration must in turn clear the internal budgets it
	// contains: two artifact downloads plus a restart/rollback pair.
	if floor := 2*downloadClient.Timeout + 2*(90*time.Second); i.reconcileTimeout <= floor {
		t.Errorf("reconcileTimeout = %v, must exceed %v (download + health-wait budgets)", i.reconcileTimeout, floor)
	}
}

func TestNewHonoursConfiguredReconcileTimeout(t *testing.T) {
	i := New(Config{ReconcileInterval: time.Minute, ReconcileTimeout: 3 * time.Minute}, slog.New(slog.DiscardHandler), nil, nil, nil)
	if i.reconcileTimeout != 3*time.Minute {
		t.Errorf("reconcileTimeout = %v, want the configured 3m", i.reconcileTimeout)
	}
	if want := stallFactor * (3*time.Minute + time.Minute); i.stallThreshold != want {
		t.Errorf("stallThreshold = %v, want %v (derived from the configured timeout)", i.stallThreshold, want)
	}
	// A zero value must not collapse the bound to "abort immediately".
	if zero := New(Config{}, slog.New(slog.DiscardHandler), nil, nil, nil); zero.reconcileTimeout != defaultReconcileTimeout {
		t.Errorf("unset ReconcileTimeout = %v, want the default %v", zero.reconcileTimeout, defaultReconcileTimeout)
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
