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
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// defaultReconcileTimeout bounds one iteration, since HostExec has no timeout
// of its own and a wedged host command would freeze the loop forever. It must
// clear the worst honest budget: two ~130MB downloads at up to 10m each plus
// two 90s health waits, or slow-but-healthy installs get killed.
const defaultReconcileTimeout = 30 * time.Minute

// stallFactor keeps the watchdog well past both ReconcileInterval and
// reconcileTimeout, so it cannot flap during a long install.
const stallFactor = 2

// Installer reconciles one node towards the desired gVisor installation.
type Installer struct {
	cfg  Config
	log  *slog.Logger
	host HostExec
	cri  CRIClient
	node NodeClient

	ready atomic.Bool
	// Emit node events on transitions only, not every reconcile.
	lastReadyLabel string

	// Cacheable: a node's containerd layout cannot change under a running pod.
	preflightOK bool
	// Host procfs, readable because the pod runs with hostPID.
	procRoot string

	// Bounds convergence restarts to one per regression, so a node that cannot
	// converge is not restarted on every retry.
	convergeRestartedFor string

	// Post-restart health check pacing (shortened in tests).
	healthWait time.Duration
	healthPoll time.Duration

	// Fields so tests can shorten them.
	reconcileTimeout time.Duration
	stallThreshold   time.Duration
	// Unix nanos, feeding the /healthz watchdog.
	lastReconcileDone atomic.Int64
}

func New(cfg Config, log *slog.Logger, host HostExec, cri CRIClient, node NodeClient) *Installer {
	reconcileTimeout := cfg.ReconcileTimeout
	// Configs built directly (tests, callers that do not go through
	// ConfigFromEnv) leave it zero, which would abort every iteration at once.
	if reconcileTimeout <= 0 {
		reconcileTimeout = defaultReconcileTimeout
	}
	i := &Installer{
		cfg: cfg, log: log, host: host, cri: cri, node: node,
		procRoot:         "/proc",
		healthWait:       90 * time.Second,
		healthPoll:       2 * time.Second,
		reconcileTimeout: reconcileTimeout,
	}
	i.stallThreshold = stallFactor * (i.reconcileTimeout + cfg.ReconcileInterval)
	// The pod has not stalled before it has had a chance to run.
	i.lastReconcileDone.Store(time.Now().UnixNano())
	return i
}

func (i *Installer) Ready() bool { return i.ready.Load() }

// Stalled reports whether the loop stopped completing iterations, and for how
// long (liveness probe). Only an unkillable host command reaches this, since
// the per-iteration timeout ends every other hang, so restarting the pod is
// the only recovery.
func (i *Installer) Stalled() (bool, time.Duration) {
	silent := time.Since(time.Unix(0, i.lastReconcileDone.Load()))
	return silent > i.stallThreshold, silent
}

// Run backs off exponentially because each retry can re-download binaries and
// restart containerd. It deliberately does not clean up on shutdown: a
// DaemonSet pod cannot tell "helm uninstall" from a rolling update, and
// removing the handler under running sandboxes breaks them.
func (i *Installer) Run(ctx context.Context) {
	failures := 0
	for {
		if err := i.reconcileOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			failures++
			i.log.Error("reconcile failed", "error", err)
		} else {
			failures = 0
		}
		interval := i.cfg.ReconcileInterval
		if failures > 0 {
			interval = backoffInterval(i.cfg.RetryInterval, failures, i.cfg.ReconcileInterval)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// reconcileOnce runs one iteration under its own deadline. ctx stays the
// parent so Run can still tell shutdown apart from a timeout.
func (i *Installer) reconcileOnce(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, i.reconcileTimeout)
	defer cancel()
	return i.Reconcile(ctx)
}

func backoffInterval(base time.Duration, failures int, maxWait time.Duration) time.Duration {
	d := base
	for n := 1; n < failures; n++ {
		d *= 2
		if d >= maxWait {
			return maxWait
		}
	}
	if d > maxWait {
		return maxWait
	}
	return d
}

type reconcileOutcome struct {
	// Someone else's runsc handler was verified. Usable but unmanaged.
	foreignOK bool
	// The handler name is backed by something else. Never ready.
	conflict bool
}

// Reconcile always publishes labels reflecting observed state, so a failed
// upgrade on a working node keeps it schedulable.
func (i *Installer) Reconcile(ctx context.Context) error {
	out, err := i.reconcile(ctx)
	i.publishStatus(ctx, out, err)
	// Reaching this line at all is the proof the loop is not wedged.
	i.lastReconcileDone.Store(time.Now().UnixNano())
	return err
}

func (i *Installer) reconcile(ctx context.Context) (out reconcileOutcome, err error) {
	if err := i.preflight(ctx); err != nil {
		return out, err
	}

	cfgPath := i.cfg.hostPath(containerdConfigPath)
	raw, err := os.ReadFile(cfgPath) //nolint:gosec // fixed managed path
	if err != nil {
		return out, fmt.Errorf("reading containerd config (a standard containerd installation with %s is required): %w", containerdConfigPath, err)
	}
	// All writes into the config dir are atomic-via-temp; sweep temps a crash
	// may have stranded (ensureBinaries does the same for the install dir).
	removeStaleTemps(filepath.Dir(cfgPath))

	// `config dump` is both the merged view (file + imports + defaults) and the
	// only validation containerd offers. It parses the on-disk config and says
	// nothing about the running daemon, hence the separate CRI check.
	dump, err := i.host.Run(ctx, "containerd", "config", "dump")
	if err != nil {
		return out, fmt.Errorf("containerd's current configuration failed to load, refusing to modify anything: %w", err)
	}

	current, managed, err := managedBlock(raw)
	if err != nil {
		return out, err
	}
	blockCurrent := false
	if managed {
		if desired, err := i.desiredManagedBlock(raw); err == nil {
			blockCurrent = current == desired
		}
	}
	// Checked unconditionally, because a drop-in import can override our entry
	// even on a managed node. Path pinning applies only while the on-disk block
	// is current: foreign entries own their paths, and a stale block
	// legitimately diverges mid-transition.
	rt, handlerInDump := runtimeFromDump(dump, i.cfg.Handler)
	if handlerInDump {
		if err := i.mergedEntryConflict(rt, managed && blockCurrent); err != nil {
			out.conflict = true
			return out, err
		}
	}
	if !managed && handlerInDump {
		// Someone else's runsc handler: require it to be served, change nothing.
		out.foreignOK = true
		i.log.Info("pre-existing runsc handler detected, leaving node unmanaged", "handler", i.cfg.Handler)
		return out, i.verifyServing(ctx)
	}

	if _, err := i.ensureBinaries(ctx); err != nil {
		return out, err
	}
	if err := i.ensureRunscShimConfig(dump); err != nil {
		return out, err
	}
	changed, err := i.ensureContainerdConfig(ctx, raw)
	if err != nil {
		return out, err
	}
	if changed {
		// Path pinning was skipped above while the block was stale, so re-check
		// now rather than leaving a conflicting drop-in ready until next cycle.
		dump, err := i.host.Run(ctx, "containerd", "config", "dump")
		if err != nil {
			return out, fmt.Errorf("containerd configuration failed to load after the managed block update: %w", err)
		}
		if rt, ok := runtimeFromDump(dump, i.cfg.Handler); ok {
			if err := i.mergedEntryConflict(rt, true); err != nil {
				out.conflict = true
				return out, err
			}
		}
	}
	return out, nil
}

// pinPaths demands the managed paths, which is right for nodes whose block we
// wrote but never for foreign entries, which own their paths.
func (i *Installer) mergedEntryConflict(rt map[string]any, pinPaths bool) error {
	if rtType, _ := rt["runtime_type"].(string); rtType != runscRuntimeType {
		return fmt.Errorf("containerd has a runtime handler %q with runtime_type %q (expected %s). "+
			"resolve the conflict by removing it or by configuring a different gvisor.installer.handler",
			i.cfg.Handler, rt["runtime_type"], runscRuntimeType)
	}
	if !pinPaths {
		return nil
	}
	if field, got := mergedEntryOverride(rt, i.cfg.shimPath()); field != "" {
		return fmt.Errorf("a containerd import overrides the managed runtime handler %q (%s is %q). "+
			"sandboxes would not run the isola-managed gVisor: remove the conflicting drop-in",
			i.cfg.Handler, field, got)
	}
	return nil
}

func (i *Installer) desiredManagedBlock(raw []byte) (string, error) {
	pluginID, err := criPluginID(configSchemaVersion(raw))
	if err != nil {
		return "", err
	}
	return renderManagedBlock(pluginID, i.cfg.Handler, i.cfg.shimPath(), runscShimConfigPath), nil
}

// changed reports whether the on-disk config was rewritten, which tells the
// caller to re-check the merged view.
func (i *Installer) ensureContainerdConfig(ctx context.Context, raw []byte) (changed bool, err error) {
	// Every success path below ends with the handler verified served, which
	// is the condition the convergence-restart budget is scoped to.
	defer func() {
		if err == nil {
			i.convergeRestartedFor = ""
		}
	}()

	desired, err := i.desiredManagedBlock(raw)
	if err != nil {
		return false, err
	}
	current, found, err := managedBlock(raw)
	if err != nil {
		return false, err
	}
	if !found || current != desired {
		err := i.applyConfigChange(ctx, raw, desired)
		return err == nil, err
	}

	servingErr := i.verifyServing(ctx)
	if servingErr == nil {
		return false, nil
	}
	// Config is right but the daemon disagrees: a previous run was interrupted
	// before its restart, or the daemon regressed. Restart once per regression,
	// never on every retry.
	if i.convergeRestartedFor == desired {
		return false, fmt.Errorf("runtime handler still not served after a convergence restart: %w", servingErr)
	}
	i.log.Warn("containerd config is up to date but the running daemon does not serve the handler; restarting containerd to converge", "error", servingErr)
	if _, err := i.host.Run(ctx, "systemctl", "restart", "containerd"); err != nil {
		// Nothing was restarted; the retry budget is not spent.
		return false, err
	}
	i.convergeRestartedFor = desired
	if err := i.awaitRuntimeReady(ctx); err != nil {
		return false, err
	}
	return false, i.verifyServing(ctx)
}

// applyConfigChange runs the safety chain: backup, atomic write, validate,
// restart, health check, rollback on failure. In-place rollback is possible
// because the installer pod survives containerd restarts.
func (i *Installer) applyConfigChange(ctx context.Context, raw []byte, desired string) error {
	// raw predates a download that can take minutes. Splicing a stale snapshot
	// would silently revert anything edited in that window, so bail out rather
	// than merge and let the next reconcile recompute.
	cfgPath := i.cfg.hostPath(containerdConfigPath)
	fresh, err := os.ReadFile(cfgPath) //nolint:gosec // fixed managed path
	if err != nil {
		return fmt.Errorf("re-reading containerd config before writing it: %w", err)
	}
	if !bytes.Equal(fresh, raw) {
		return fmt.Errorf("%s changed while this reconcile was running. Nothing was written and containerd was not restarted, retrying from the current file", containerdConfigPath)
	}

	next, err := spliceManagedBlock(raw, desired)
	if err != nil {
		return err
	}

	if err := writePristineBackup(i.cfg.hostPath(containerdConfigBackupPath), cfgPath, raw); err != nil {
		return fmt.Errorf("writing pristine config backup: %w", err)
	}

	i.log.Info("updating containerd config", "path", containerdConfigPath, "schemaVersion", configSchemaVersion(raw))
	if err := writeFileAtomic(cfgPath, next); err != nil {
		return fmt.Errorf("writing containerd config: %w", err)
	}

	if _, err := i.host.Run(ctx, "containerd", "config", "dump"); err != nil {
		if restoreErr := writeFileAtomic(cfgPath, raw); restoreErr != nil {
			return fmt.Errorf("new containerd config failed validation (%w) AND restoring the previous config failed: %w", err, restoreErr)
		}
		return fmt.Errorf("new containerd config failed validation, previous config restored, containerd not restarted: %w", err)
	}

	i.log.Warn("restarting containerd to register the gVisor runtime handler (running containers are unaffected)")
	if err := i.restartAndAwaitHealthy(ctx); err != nil {
		i.log.Error("containerd unhealthy after config change; rolling back", "error", err)
		if rbErr := i.rollback(ctx, cfgPath, raw); rbErr != nil {
			return fmt.Errorf("containerd unhealthy after config change (%w) AND rollback failed: %w", err, rbErr)
		}
		return fmt.Errorf("containerd unhealthy after config change, previous config rolled back and containerd recovered: %w", err)
	}

	if err := i.verifyServing(ctx); err != nil {
		return err
	}
	i.log.Info("gVisor runtime handler registered with containerd", "handler", i.cfg.Handler)
	i.node.Event(corev1.EventTypeNormal, "GVisorRuntimeConfigured",
		fmt.Sprintf("Registered containerd runtime handler %q (gVisor %s)", i.cfg.Handler, i.cfg.Version))
	return nil
}

// writePristineBackup keeps a write-once copy for manual recovery, at the
// source's mode: config.toml may be 0600 and hold inline registry credentials,
// which writeFileAtomic's 0644 default for new files would leak.
func writePristineBackup(backupPath, srcPath string, raw []byte) error {
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(srcPath); err == nil {
		mode = fi.Mode().Perm()
	}
	// Write-once. A zero-length backup is a crash remnant, not the pristine
	// config. An existing one is never re-permissioned: it can hold credentials
	// the current config no longer does, so tracking config.toml's mode could
	// only expose it.
	if fi, err := os.Stat(backupPath); err == nil && fi.Size() > 0 {
		return nil
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	// writeFileAtomic preserves an existing file's mode, so pre-create at the
	// target mode. The chmod defeats the umask.
	f, err := os.OpenFile(backupPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode) //nolint:gosec // mode is the source config's own, never widened
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(backupPath, mode); err != nil {
		return err
	}
	return writeFileAtomic(backupPath, raw)
}

// rollback detaches from ctx: an expired deadline or SIGTERM must not skip
// the restart that recovers the node onto the previous config.
func (i *Installer) rollback(ctx context.Context, cfgPath string, previous []byte) error {
	if err := writeFileAtomic(cfgPath, previous); err != nil {
		return fmt.Errorf("restoring previous config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), i.healthWait+time.Minute)
	defer cancel()
	return i.restartAndAwaitHealthy(ctx)
}

func (i *Installer) restartAndAwaitHealthy(ctx context.Context) error {
	if _, err := i.host.Run(ctx, "systemctl", "restart", "containerd"); err != nil {
		return err
	}
	return i.awaitRuntimeReady(ctx)
}

// awaitRuntimeReady polls the signal the kubelet uses, which tolerates ~30s
// of downtime, so a healthy restart is invisible to the cluster.
func (i *Installer) awaitRuntimeReady(ctx context.Context) error {
	deadline := time.Now().Add(i.healthWait)
	var lastErr error
	for time.Now().Before(deadline) {
		// Asking for handlers would marshal containerd 1.x's config every poll.
		st, err := i.cri.Status(ctx, false)
		if err == nil && st.RuntimeReady {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("RuntimeReady condition is false")
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(i.healthPoll):
		}
	}
	return fmt.Errorf("containerd not ready within %s: %w", i.healthWait, lastErr)
}

// verifyServing asks the live daemon, never the on-disk config: a config
// written but never loaded must not count as installed.
func (i *Installer) verifyServing(ctx context.Context) error {
	st, err := i.cri.Status(ctx, true)
	if err != nil {
		return err
	}
	if !st.RuntimeReady {
		return fmt.Errorf("containerd RuntimeReady condition is false")
	}
	if len(st.Handlers) == 0 {
		return errNoRuntimeHandlers
	}
	if !slices.Contains(st.Handlers, i.cfg.Handler) {
		return fmt.Errorf("runtime handler %q not served by containerd (handlers: %v)", i.cfg.Handler, st.Handlers)
	}
	return nil
}

// publishStatus translates the reconcile outcome into node labels, events and
// pod readiness. Labels reflect observed state, not reconcile success, so a
// failed upgrade download does not clear an already-working node's label.
// Ready requires identity (managed block matches, or a type-checked foreign
// handler, with no conflict), liveness (the daemon serves the handler), and
// for managed nodes intact binaries.
func (i *Installer) publishStatus(ctx context.Context, out reconcileOutcome, reconcileErr error) {
	// The reconcile ctx may already be cancelled on shutdown.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	identity := false
	if !out.conflict {
		if out.foreignOK {
			identity = true
		} else if raw, err := os.ReadFile(i.cfg.hostPath(containerdConfigPath)); err == nil { //nolint:gosec // fixed managed path
			// Mere marker presence is not enough: a stale block could
			// register a different handler than the currently configured one.
			if desired, err := i.desiredManagedBlock(raw); err == nil {
				current, found, _ := managedBlock(raw)
				identity = found && current == desired
			}
		}
	}

	readyLabel := "false"
	versionLabel := ""
	if identity && i.verifyServing(ctx) == nil {
		if out.foreignOK {
			readyLabel = "true"
			versionLabel = VersionUnmanaged
		} else if v := i.installedVersion(); v != "" {
			// Registration alone is not enough for managed nodes: containerd
			// registers handlers without stat'ing the shim binary, so wiped
			// binaries would otherwise keep the node schedulable while every
			// sandbox start fails.
			readyLabel = "true"
			versionLabel = v
		}
	}

	// Unconditionally, so externally deleted labels are healed; a no-op
	// strategic-merge patch does not bump the node's resourceVersion.
	patchErr := i.node.SetNodeLabels(ctx, map[string]string{
		LabelGVisorReady:   readyLabel,
		LabelGVisorVersion: versionLabel,
	})
	if patchErr != nil {
		i.log.Error("updating node labels failed", "error", patchErr)
	}

	if reconcileErr != nil {
		i.node.Event(corev1.EventTypeWarning, "GVisorInstallFailed", truncateOutput([]byte(reconcileErr.Error())))
	}
	if readyLabel != i.lastReadyLabel {
		if readyLabel == "true" {
			i.node.Event(corev1.EventTypeNormal, "GVisorReady",
				fmt.Sprintf("gVisor runtime handler %q is available on this node (version: %s)", i.cfg.Handler, versionLabel))
		} else if i.lastReadyLabel == "true" {
			i.node.Event(corev1.EventTypeWarning, "GVisorUnready",
				fmt.Sprintf("gVisor runtime handler %q is no longer verified healthy on this node", i.cfg.Handler))
		}
		i.lastReadyLabel = readyLabel
	}

	i.ready.Store(reconcileErr == nil && readyLabel == "true" && patchErr == nil)
}
