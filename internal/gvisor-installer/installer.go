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
	"errors"
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
// clear the worst honest budget: the checksum and archive requests at up to
// 10m each, local extraction and hashing of a few hundred MB, plus two 90s
// health waits, or slow-but-healthy installs get killed.
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
	preflightPID int
	// Host procfs, readable because the pod runs with hostPID.
	procRoot string

	// Bounds convergence restarts to one per regression, so a node that cannot
	// converge is not restarted on every retry.
	convergeRestartedFor string
	// Rate-limit rather than latch: a node whose containerd is unhealthy for
	// unrelated reasons must not be restarted every interval, but must still
	// recover on its own once the cause clears.
	recoveryGate    restartGate
	activationGate  restartGate
	restartCooldown time.Duration

	// Post-restart health check pacing (shortened in tests).
	healthWait time.Duration
	healthPoll time.Duration

	// Verifying a generation hashes its full ~265MB payload. The early drift
	// check and publishStatus both need the verdict, so one reconcile pass
	// memoizes it rather than doubling the installer's steady-state IO.
	verifyCache map[string]error

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
		restartCooldown:  30 * time.Minute,
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

// Run backs off exponentially because each retry can re-download the archive
// and restart containerd. It deliberately does not clean up on shutdown: a
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

type restartGate struct {
	sig       string
	notBefore time.Time
}

func (g *restartGate) blocked(now time.Time, sig string) bool {
	return g.sig == sig && now.Before(g.notBefore)
}

func (g *restartGate) block(now time.Time, sig string, cooldown time.Duration) {
	g.sig, g.notBefore = sig, now.Add(cooldown)
}

func (g *restartGate) clear() { *g = restartGate{} }

type reconcileOutcome struct {
	// Someone else's runsc handler was verified. Usable but unmanaged.
	foreignOK bool
	// The handler name is backed by something else. Never ready.
	conflict bool
}

// Reconcile always publishes labels reflecting observed state, so a failed
// upgrade on a working node keeps it schedulable.
func (i *Installer) Reconcile(ctx context.Context) error {
	i.verifyCache = map[string]error{}
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

	// While pending exists, neither the on-disk block nor the served handler
	// can be attributed to a generation, so settle it before classifying.
	st := i.readState()
	if st.Pending != nil {
		if err := i.recoverPending(ctx, &st); err != nil {
			return out, fmt.Errorf("recovering interrupted activation: %w", err)
		}
	}

	cfgPath := i.cfg.hostPath(containerdConfigPath)
	raw, err := os.ReadFile(cfgPath) //nolint:gosec // fixed managed path
	if err != nil {
		return out, fmt.Errorf("reading containerd config (a standard containerd installation with %s is required): %w", containerdConfigPath, err)
	}
	// All writes into the config dir are atomic-via-temp; sweep temps a crash
	// may have stranded (ensureGeneration does the same for the releases dir).
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
	activeCurrent := managed && st.Active != nil && current == st.Active.ManagedBlock
	// A drop-in import can override our entry even on a managed node.
	rt, handlerInDump := runtimeFromDump(dump, i.cfg.Handler)
	if handlerInDump {
		if err := i.mergedEntryConflict(rt, i.pinnedTarget(st, activeCurrent)); err != nil {
			out.conflict = true
			return out, err
		}
	}
	if !managed && handlerInDump {
		// Someone else's runsc handler: require it to be served, change nothing.
		out.foreignOK = true
		i.log.Info("pre-existing runsc handler detected, leaving node unmanaged", "handler", i.cfg.Handler)
		return out, i.verifyServing(ctx, i.cfg.Handler, nil)
	}

	i.maintainActive(ctx, st)

	gen, _, err := i.ensureGeneration(ctx)
	if err != nil {
		return out, err
	}
	configPath, err := i.ensureRunscConfig(gen, dump)
	if err != nil {
		return out, err
	}
	target := runtimeTarget{ShimPath: gen.shimPath(), ConfigPath: configPath}
	changed, err := i.ensureContainerdConfig(ctx, &st, raw, gen, target)
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
			if err := i.mergedEntryConflict(rt, &target); err != nil {
				out.conflict = true
				return out, err
			}
		}
	}
	return out, nil
}

// nil when nothing is safely pinnable: foreign entries and stale blocks own
// their paths.
func (i *Installer) pinnedTarget(st installerState, activeCurrent bool) *runtimeTarget {
	if !activeCurrent {
		return nil
	}
	g, err := i.loadGeneration(st.Active.GenerationPath)
	if err != nil {
		return nil
	}
	return &runtimeTarget{ShimPath: g.shimPath(), ConfigPath: st.Active.ConfigPath}
}

// A corrupted active generation flips the node unready before the repair
// download, not after: its sandboxes may be failing right now. A failed
// upgrade with an intact active generation keeps the node ready.
func (i *Installer) maintainActive(ctx context.Context, st installerState) {
	if st.Active == nil || st.Active.Handler != i.cfg.Handler {
		return
	}
	err := i.verifiedActiveGeneration(st)
	if err == nil && !i.runscConfigIntact(st.Active.ConfigPath) {
		err = fmt.Errorf("active shim config %s is missing or altered", st.Active.ConfigPath)
	}
	// Either failing means new sandboxes cannot work RIGHT NOW; the node must
	// not stay schedulable for the duration of whatever repair or upgrade
	// download follows.
	if err != nil {
		i.log.Warn("active installation unhealthy, marking node unready before repairing", "generation", st.Active.GenerationPath, "error", err)
		_ = i.setNodeLabels(ctx, "false", "")
	}
}

func (i *Installer) verifiedActiveGeneration(st installerState) error {
	if st.Active == nil {
		return fmt.Errorf("no active generation")
	}
	g, err := i.loadGeneration(st.Active.GenerationPath)
	if err != nil {
		return err
	}
	if g.Version != st.Active.Version || g.ArchiveSHA512 != st.Active.ArchiveSHA512 {
		return fmt.Errorf("generation %s does not match the active record", st.Active.GenerationPath)
	}
	return i.verifyGenerationCached(g)
}

func (i *Installer) mergedEntryConflict(rt map[string]any, pin *runtimeTarget) error {
	if rtType, _ := rt["runtime_type"].(string); rtType != runscRuntimeType {
		return fmt.Errorf("containerd has a runtime handler %q with runtime_type %q (expected %s). "+
			"resolve the conflict by removing it or by configuring a different gvisor.installer.handler",
			i.cfg.Handler, rt["runtime_type"], runscRuntimeType)
	}
	if pin == nil {
		return nil
	}
	if field, got := mergedEntryOverride(rt, *pin); field != "" {
		return fmt.Errorf("a containerd import overrides the managed runtime handler %q (%s is %q). "+
			"sandboxes would not run the isola-managed gVisor: remove the conflicting drop-in",
			i.cfg.Handler, field, got)
	}
	return nil
}

func (i *Installer) desiredManagedBlock(raw []byte, want runtimeTarget) (string, error) {
	pluginID, err := criPluginID(configSchemaVersion(raw))
	if err != nil {
		return "", err
	}
	return renderManagedBlock(pluginID, i.cfg.Handler, want.ShimPath, want.ConfigPath), nil
}

// changed reports whether the on-disk config was rewritten, which tells the
// caller to re-check the merged view.
func (i *Installer) ensureContainerdConfig(ctx context.Context, st *installerState, raw []byte, gen Generation, want runtimeTarget) (changed bool, err error) {
	// Every success path below ends with the handler verified served, which
	// is the condition the convergence-restart budget is scoped to.
	defer func() {
		if err == nil {
			i.convergeRestartedFor = ""
		}
	}()

	desired, err := i.desiredManagedBlock(raw, want)
	if err != nil {
		return false, err
	}
	current, found, err := managedBlock(raw)
	if err != nil {
		return false, err
	}
	settled := found && current == desired &&
		st.Active != nil && st.Active.ManagedBlock == desired
	if !settled {
		err := i.activate(ctx, st, raw, desired, gen, want)
		return err == nil, err
	}

	servingErr := i.verifyServing(ctx, i.cfg.Handler, &want)
	if servingErr == nil {
		return false, nil
	}
	// Config and journal agree but the daemon disagrees. Restart once per
	// regression, never on every retry.
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
	return false, i.verifyServing(ctx, i.cfg.Handler, &want)
}

// activate switches containerd to the generation under a durable journal:
// pending is written before the block is touched and cleared only once a
// restart has settled the daemon on a known block, so a crash at any point
// leaves enough to finish or roll back coherently.
func (i *Installer) activate(ctx context.Context, st *installerState, raw []byte, desired string, gen Generation, want runtimeTarget) error {
	if i.activationGate.blocked(time.Now(), desired) {
		return fmt.Errorf("this managed block failed to activate recently and containerd was rolled back, waiting before restarting it again")
	}
	pending := &pendingActivation{
		Target: generationRecord{
			GenerationPath: gen.Path,
			ConfigPath:     want.ConfigPath,
			Version:        gen.Version,
			ArchiveSHA512:  gen.ArchiveSHA512,
			Handler:        i.cfg.Handler,
			ManagedBlock:   desired,
		},
	}
	// The rollback predecessor is the RECOGNIZED block, never whatever
	// happens to be on disk: with an active record, a drifted or deleted
	// block must roll back to the active one. Only markerless legacy
	// migration records the current block, to keep the legacy install
	// restorable.
	if st.Active != nil {
		pending.PreviousManagedBlock = st.Active.ManagedBlock
		pending.PreviousHandler = st.Active.Handler
	} else if current, found, err := managedBlock(raw); err != nil {
		return err
	} else if found {
		pending.PreviousManagedBlock = current
	}
	st.Pending = pending
	if err := i.writeState(*st); err != nil {
		return err
	}

	if err := i.applyConfigChange(ctx, st, raw, desired, want); err != nil {
		return err
	}

	st.Active = &pending.Target
	st.Pending = nil
	// On failure the stale pending record makes the next reconcile redo the
	// commit via recovery.
	return i.writeState(*st)
}

// applyConfigChange runs the safety chain: backup, atomic write, validate,
// restart, health check, rollback on failure. In-place rollback is possible
// because the installer pod survives containerd restarts.
func (i *Installer) applyConfigChange(ctx context.Context, st *installerState, raw []byte, desired string, want runtimeTarget) error {
	// raw predates a download that can take minutes. Splicing a stale snapshot
	// would silently revert anything edited in that window, so bail out rather
	// than merge and let the next reconcile recompute.
	cfgPath := i.cfg.hostPath(containerdConfigPath)
	fresh, err := os.ReadFile(cfgPath) //nolint:gosec // fixed managed path
	if err != nil {
		return i.abandonPending(st, fmt.Errorf("re-reading containerd config before writing it: %w", err))
	}
	if !bytes.Equal(fresh, raw) {
		return i.abandonPending(st, fmt.Errorf("%s changed while this reconcile was running. Nothing was written and containerd was not restarted, retrying from the current file", containerdConfigPath))
	}

	next, err := spliceManagedBlock(raw, desired)
	if err != nil {
		return i.abandonPending(st, err)
	}

	if err := writePristineBackup(i.cfg.hostPath(containerdConfigBackupPath), cfgPath, raw); err != nil {
		return i.abandonPending(st, fmt.Errorf("writing pristine config backup: %w", err))
	}

	i.log.Info("updating containerd config", "path", containerdConfigPath, "schemaVersion", configSchemaVersion(raw))
	if err := writeFileAtomic(cfgPath, next); err != nil {
		return i.abandonPending(st, fmt.Errorf("writing containerd config: %w", err))
	}

	if _, err := i.host.Run(ctx, "containerd", "config", "dump"); err != nil {
		if restoreErr := writeFileAtomic(cfgPath, raw); restoreErr != nil {
			return fmt.Errorf("new containerd config failed validation (%w) AND restoring the previous config failed: %w", err, restoreErr)
		}
		return i.abandonPending(st, fmt.Errorf("new containerd config failed validation, previous config restored, containerd not restarted: %w", err))
	}

	i.log.Warn("restarting containerd to register the gVisor runtime handler (running containers are unaffected)")
	if err := i.restartAndAwaitHealthy(ctx); err != nil {
		i.log.Error("containerd unhealthy after config change; rolling back", "error", err)
		// Rollback splices the recognized predecessor into the surrounding
		// config rather than restoring raw verbatim: raw may carry a drifted
		// block that must never be blessed with a restart.
		previous, rbErr := previousConfig(raw, st.Pending)
		if rbErr == nil {
			rbErr = i.rollback(ctx, cfgPath, previous)
		}
		if rbErr != nil {
			return fmt.Errorf("containerd unhealthy after config change (%w) AND rollback failed: %w", err, rbErr)
		}
		// The rollback restart settled the daemon on the previous config, so
		// the transaction is closed.
		i.activationGate.block(time.Now(), desired, i.restartCooldown)
		return i.abandonPending(st, fmt.Errorf("containerd unhealthy after config change, previous config rolled back and containerd recovered: %w", err))
	}

	// containerd came back healthy but does not serve the new runtime, so the
	// block is bad. Leaving it live would keep re-activating and restarting
	// every interval, so restore the predecessor and spend the budget.
	if err := i.verifyServing(ctx, i.cfg.Handler, &want); err != nil {
		previous, rbErr := previousConfig(raw, st.Pending)
		if rbErr == nil {
			rbErr = i.rollback(ctx, cfgPath, previous)
		}
		if rbErr != nil {
			return fmt.Errorf("the new config does not serve runtime handler %q (%w) AND rollback failed: %w", i.cfg.Handler, err, rbErr)
		}
		i.activationGate.block(time.Now(), desired, i.restartCooldown)
		return i.abandonPending(st, fmt.Errorf("the new config does not serve runtime handler %q, previous config rolled back: %w", i.cfg.Handler, err))
	}
	i.activationGate.clear()
	i.log.Info("gVisor runtime handler registered with containerd", "handler", i.cfg.Handler)
	i.node.Event(corev1.EventTypeNormal, "GVisorRuntimeConfigured",
		fmt.Sprintf("Registered containerd runtime handler %q (gVisor %s)", i.cfg.Handler, i.cfg.Version))
	return nil
}

func previousConfig(raw []byte, p *pendingActivation) ([]byte, error) {
	if p == nil || p.PreviousManagedBlock == "" {
		return removeManagedBlock(raw)
	}
	return spliceManagedBlock(raw, p.PreviousManagedBlock)
}

// abandonPending closes a transaction that verifiably never reached the
// daemon, where leaving the journal pending would only force a needless
// recovery restart.
func (i *Installer) abandonPending(st *installerState, cause error) error {
	st.Pending = nil
	if err := i.writeState(*st); err != nil {
		i.log.Warn("clearing pending activation failed; next reconcile recovers it", "error", err)
	}
	return cause
}

// The on-disk block decides the direction: the target block finishes forward
// when the target still checks out, anything else rolls back. Every path
// restarts containerd, since pending means loaded and on-disk config cannot
// be assumed to agree.
func (i *Installer) recoverPending(ctx context.Context, st *installerState) error {
	p := st.Pending
	i.log.Warn("recovering interrupted activation", "targetGeneration", p.Target.GenerationPath)

	raw, err := os.ReadFile(i.cfg.hostPath(containerdConfigPath)) //nolint:gosec // fixed managed path
	if err != nil {
		return err
	}
	block, found, err := managedBlock(raw)
	if err != nil {
		return err
	}

	if found && block == p.Target.ManagedBlock {
		err := i.commitTarget(ctx, st)
		if err == nil {
			return nil
		}
		// Once the daemon settled on the target, rollback is no longer an
		// improvement: retry the commit forward on the next reconcile.
		if errors.Is(err, errCommitSettled) {
			return err
		}
		i.log.Warn("interrupted activation cannot be committed, rolling back", "error", err)
	} else if !found && p.PreviousManagedBlock != "" || found && block != p.PreviousManagedBlock {
		// An unrecognized block must never be blessed with a restart just
		// because it happens to be on disk.
		i.log.Warn("on-disk managed block matches neither the pending target nor its predecessor, restoring the predecessor")
	}
	return i.rollbackPending(ctx, st, raw)
}

// The target re-verifies from disk: the crash may have been the extraction's
// fault.
func (i *Installer) commitTarget(ctx context.Context, st *installerState) error {
	p := st.Pending
	g, err := i.loadGeneration(p.Target.GenerationPath)
	if err != nil {
		return err
	}
	if g.Version != p.Target.Version || g.ArchiveSHA512 != p.Target.ArchiveSHA512 {
		return fmt.Errorf("generation %s does not match the pending record", p.Target.GenerationPath)
	}
	if err := i.verifyGeneration(g); err != nil {
		return err
	}
	dump, err := i.host.Run(ctx, "containerd", "config", "dump")
	if err != nil {
		return fmt.Errorf("target config failed validation: %w", err)
	}
	// Re-rendering restores the recorded config only while it still hashes to
	// the same name. A different hash means the values changed under the
	// crash, leaving this activation unreconstructible.
	if !i.runscConfigIntact(p.Target.ConfigPath) {
		configPath, err := i.ensureRunscConfig(g, dump)
		if err != nil {
			return err
		}
		if configPath != p.Target.ConfigPath {
			return fmt.Errorf("shim config %s recorded by the pending activation can no longer be reproduced", p.Target.ConfigPath)
		}
	}
	want := runtimeTarget{ShimPath: g.shimPath(), ConfigPath: p.Target.ConfigPath}
	if rt, ok := runtimeFromDump(dump, p.Target.Handler); ok {
		if err := i.mergedEntryConflict(rt, &want); err != nil {
			return err
		}
	}
	// A crash after the activation restart leaves the daemon already on the
	// target. Only the pinned paths can tell, as both generations register
	// the same handler name.
	if err := i.verifyServing(ctx, p.Target.Handler, &want); err != nil {
		if err := i.recoveryRestart(ctx, p, "commit"); err != nil {
			return err
		}
		if err := i.verifyServing(ctx, p.Target.Handler, &want); err != nil {
			return err
		}
	}
	// st stays untouched until the journal write lands: on failure the caller
	// still needs the pending record. Past this point the daemon serves the
	// target, so failures are settled rather than rollback-worthy.
	if err := i.writeState(installerState{Active: &p.Target}); err != nil {
		return fmt.Errorf("%w: %w", errCommitSettled, err)
	}
	st.Active = &p.Target
	st.Pending = nil
	return nil
}

// errCommitSettled marks commit failures that happened after the daemon
// settled on the target.
var errCommitSettled = errors.New("target is serving but the activation is not fully committed")

func recoverySig(p *pendingActivation, direction string) string {
	return p.Target.GenerationPath + "\x00" + p.Target.ConfigPath + "\x00" + p.Target.ManagedBlock + "\x00" + direction
}

// Rate-limited so an unrelated containerd outage cannot turn into two
// restarts per interval forever.
func (i *Installer) recoveryRestart(ctx context.Context, p *pendingActivation, direction string) error {
	sig := recoverySig(p, direction)
	if i.recoveryGate.blocked(time.Now(), sig) {
		return fmt.Errorf("containerd was restarted recently to %s this interrupted activation and still does not serve the expected runtime", direction)
	}
	i.recoveryGate.block(time.Now(), sig, i.restartCooldown)
	return i.restartAndAwaitHealthy(ctx)
}

// Pending is cleared only after the restart succeeded: a crash beforehand
// leaves the journal for the next process, which still knows a restart is
// mandatory.
func (i *Installer) rollbackPending(ctx context.Context, st *installerState, raw []byte) error {
	p := st.Pending
	var next []byte
	var err error
	if p.PreviousManagedBlock == "" {
		next, err = removeManagedBlock(raw)
	} else {
		next, err = spliceManagedBlock(raw, p.PreviousManagedBlock)
	}
	if err != nil {
		return err
	}
	cfgPath := i.cfg.hostPath(containerdConfigPath)
	if !bytes.Equal(next, raw) {
		if err := writeFileAtomic(cfgPath, next); err != nil {
			return fmt.Errorf("restoring previous managed block: %w", err)
		}
	}
	// A spent budget here means the restart already happened, so the journal
	// write below must still be retryable without another one.
	if !i.recoveryGate.blocked(time.Now(), recoverySig(p, "roll back")) {
		if err := i.recoveryRestart(ctx, p, "roll back"); err != nil {
			return fmt.Errorf("restarting containerd onto the previous config: %w", err)
		}
	}
	if p.PreviousHandler != "" {
		// Settled but unhealthy is still settled; the normal flow takes over.
		if err := i.verifyServing(ctx, p.PreviousHandler, nil); err != nil {
			i.log.Warn("previous handler not served after rollback", "handler", p.PreviousHandler, "error", err)
		}
	}
	st.Pending = nil
	if err := i.writeState(*st); err != nil {
		return err
	}
	i.log.Info("interrupted activation rolled back", "targetGeneration", p.Target.GenerationPath)
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

// Asks the live daemon, never the on-disk config: a config written but never
// loaded must not count as installed. want additionally pins the loaded
// paths, the only way to tell two generations apart.
func (i *Installer) verifyServing(ctx context.Context, handler string, want *runtimeTarget) error {
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
	if !slices.Contains(st.Handlers, handler) {
		return fmt.Errorf("runtime handler %q not served by containerd (handlers: %v)", handler, st.Handlers)
	}
	if want != nil {
		loaded, ok := st.Runtimes[handler]
		if !ok {
			return fmt.Errorf("containerd did not report a loaded config for runtime handler %q", handler)
		}
		if loaded.Path != want.ShimPath {
			return fmt.Errorf("containerd serves handler %q from shim %q, expected %q", handler, loaded.Path, want.ShimPath)
		}
		if loaded.ConfigPath != want.ConfigPath {
			return fmt.Errorf("containerd serves handler %q with ConfigPath %q, expected %q", handler, loaded.ConfigPath, want.ConfigPath)
		}
		if loaded.Type != runscRuntimeType {
			return fmt.Errorf("containerd serves handler %q with runtime_type %q, expected %s", handler, loaded.Type, runscRuntimeType)
		}
	}
	return nil
}

// Labels reflect observed state, not reconcile success, so a failed upgrade
// download does not clear an already-working node's label.
func (i *Installer) publishStatus(ctx context.Context, out reconcileOutcome, reconcileErr error) {
	// The reconcile ctx may already be cancelled on shutdown.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	identity := false
	versionLabel := ""
	var want *runtimeTarget
	if !out.conflict {
		if out.foreignOK {
			identity = true
			versionLabel = VersionUnmanaged
		} else if record, ok := i.verifiedActiveState(); ok {
			identity = true
			versionLabel = record.Version
			want = &runtimeTarget{
				ShimPath:   (Generation{Path: record.GenerationPath}).shimPath(),
				ConfigPath: record.ConfigPath,
			}
		}
	}

	readyLabel := "false"
	if identity && i.verifyServing(ctx, i.cfg.Handler, want) == nil {
		readyLabel = "true"
	} else {
		versionLabel = ""
	}

	patchErr := i.setNodeLabels(ctx, readyLabel, versionLabel)

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

// CRI registration alone cannot be identity: containerd lists handlers
// without stat'ing the shim, and a broken ConfigPath fails every new sandbox
// while the handler stays listed. Hence block, payload and shim config are
// all checked against the settled journal.
func (i *Installer) verifiedActiveState() (generationRecord, bool) {
	st := i.readState()
	if st.Pending != nil || st.Active == nil || st.Active.Handler != i.cfg.Handler {
		return generationRecord{}, false
	}
	raw, err := os.ReadFile(i.cfg.hostPath(containerdConfigPath)) //nolint:gosec // fixed managed path
	if err != nil {
		return generationRecord{}, false
	}
	current, found, err := managedBlock(raw)
	if err != nil || !found || current != st.Active.ManagedBlock {
		return generationRecord{}, false
	}
	if err := i.verifiedActiveGeneration(st); err != nil {
		return generationRecord{}, false
	}
	if !i.runscConfigIntact(st.Active.ConfigPath) {
		return generationRecord{}, false
	}
	return *st.Active, true
}

// Unconditional, so externally deleted labels are healed; a no-op
// strategic-merge patch does not bump the node's resourceVersion.
func (i *Installer) setNodeLabels(ctx context.Context, ready, version string) error {
	err := i.node.SetNodeLabels(ctx, map[string]string{
		LabelGVisorReady:   ready,
		LabelGVisorVersion: version,
	})
	if err != nil {
		i.log.Error("updating node labels failed", "error", err)
	}
	return err
}
