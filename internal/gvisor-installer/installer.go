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
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Installer reconciles one node towards the desired gVisor installation.
type Installer struct {
	cfg  Config
	log  *slog.Logger
	host HostExec
	cri  CRIClient
	node NodeClient

	ready atomic.Bool
	// lastReadyLabel tracks the last applied ready-label value so node events
	// are only emitted on transitions, not every reconcile.
	lastReadyLabel string

	// preflightOK caches a passed preflight; a node's containerd layout
	// cannot change under a running pod.
	preflightOK bool
	// procRoot is the procfs used to inspect the host's containerd process
	// (readable directly because the pod runs with hostPID). Tests override it.
	procRoot string

	// convergeRestartedFor remembers the managed-block content a convergence
	// restart was already issued for, so a node that cannot converge is not
	// subjected to a containerd restart on every retry.
	convergeRestartedFor string

	// Post-restart health check pacing (shortened in tests).
	healthWait time.Duration
	healthPoll time.Duration
}

// New assembles an Installer from its collaborators (separated for testing).
func New(cfg Config, log *slog.Logger, host HostExec, cri CRIClient, node NodeClient) *Installer {
	return &Installer{
		cfg: cfg, log: log, host: host, cri: cri, node: node,
		procRoot:   "/proc",
		healthWait: 90 * time.Second,
		healthPoll: 2 * time.Second,
	}
}

// Ready reports whether the last reconcile succeeded (readiness probe).
func (i *Installer) Ready() bool { return i.ready.Load() }

// Run reconciles in a loop until the context is cancelled, backing off
// exponentially on persistent failures: each retry can re-download binaries
// and, worst case, restart containerd, so a flat interval would hammer both.
// There is deliberately no cleanup on shutdown: removing a runtime handler
// out from under running sandboxes breaks them, and a DaemonSet pod cannot
// distinguish "helm uninstall" from a rolling update or eviction.
func (i *Installer) Run(ctx context.Context) {
	failures := 0
	for {
		if err := i.Reconcile(ctx); err != nil {
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

// backoffInterval doubles base per consecutive failure, capped at maxWait.
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

// Reconcile drives the node to the desired state:
//
//  1. preflight: refuse nodes that do not run containerd as the standard
//     systemd unit loading /etc/containerd/config.toml (k3s/RKE2/... manage
//     containerd elsewhere; editing a file nobody reads helps no one);
//  2. if a non-isola runsc handler already exists, adopt nothing and touch
//     nothing — verify the live runtime serves it and report the node ready
//     (foreign mode);
//  3. ensure binaries (download/verify/atomically replace; no containerd
//     restart needed — running sandboxes keep their inode);
//  4. ensure the runsc shim config (read by the shim per sandbox start; no
//     restart needed);
//  5. ensure the managed block in containerd's config.toml — the only step
//     that restarts containerd, guarded by validate/health-check/rollback —
//     and verify the live runtime serves the handler.
//
// Whatever happens, node labels are updated at the end to reflect the
// *observed* state, so a failed upgrade on a previously working node keeps
// the node schedulable for sandboxes.
func (i *Installer) Reconcile(ctx context.Context) (err error) {
	foreignOK, conflict := false, false
	defer func() {
		i.publishStatus(ctx, foreignOK, conflict, err)
	}()

	if err := i.preflight(ctx); err != nil {
		return err
	}

	raw, err := os.ReadFile(i.cfg.hostPath(containerdConfigPath)) //nolint:gosec // fixed managed path
	if err != nil {
		return fmt.Errorf("reading containerd config (a standard containerd installation with %s is required): %w", containerdConfigPath, err)
	}

	// `containerd config dump` is both the merged-config view (file +
	// imports + defaults, post-migration) and the only validation containerd
	// offers: it exits non-zero if the current config does not load. It
	// parses the on-disk config — it says nothing about the running daemon,
	// which is why serving is verified separately over CRI.
	dump, err := i.host.Run(ctx, "containerd", "config", "dump")
	if err != nil {
		return fmt.Errorf("containerd's current configuration failed to load; refusing to modify anything: %w", err)
	}

	_, managed, err := managedBlock(raw)
	if err != nil {
		return err
	}
	// The type check runs on the MERGED view unconditionally: even on a
	// managed node, a drop-in import can override our handler entry with a
	// different runtime_type (imports overwrite scalar fields), and such a
	// node must never be considered gVisor-ready.
	rt, handlerInDump := runtimeFromDump(dump, i.cfg.Handler)
	if handlerInDump {
		if rtType, _ := rt["runtime_type"].(string); rtType != runscRuntimeType {
			conflict = true
			return fmt.Errorf("containerd has a runtime handler %q with runtime_type %q (expected %s); "+
				"resolve the conflict by removing it or by configuring a different gvisor.installer.handler",
				i.cfg.Handler, rt["runtime_type"], runscRuntimeType)
		}
	}
	if !managed && handlerInDump {
		// A handler we did not write already exists and is runsc-typed.
		// Respect it: require the live daemon to serve it, but leave the
		// node alone.
		foreignOK = true
		i.log.Info("pre-existing runsc handler detected; leaving node unmanaged", "handler", i.cfg.Handler)
		return i.verifyServing(ctx)
	}

	if _, err := i.ensureBinaries(ctx); err != nil {
		return err
	}
	if err := i.ensureRunscShimConfig(dump); err != nil {
		return err
	}
	return i.ensureContainerdConfig(ctx, raw)
}

// ensureContainerdConfig reconciles the managed block in config.toml and
// verifies the live daemon serves the handler. When a change is required it
// follows the full safety chain: pristine backup → atomic write → validate →
// restart → CRI health check → rollback on any failure. The installer pod
// itself survives containerd restarts (shims keep containers running), which
// is what makes in-place rollback possible.
func (i *Installer) ensureContainerdConfig(ctx context.Context, raw []byte) error {
	pluginID, err := criPluginID(configSchemaVersion(raw))
	if err != nil {
		return err
	}
	desired := renderManagedBlock(pluginID, i.cfg.Handler, i.cfg.shimPath(), runscShimConfigPath)
	current, found, err := managedBlock(raw)
	if err != nil {
		return err
	}
	if found && current == desired {
		servingErr := i.verifyServing(ctx)
		if servingErr == nil {
			i.convergeRestartedFor = ""
			return nil
		}
		// The on-disk config is correct but the live daemon does not serve
		// the handler — a previous run was interrupted between writing the
		// config and restarting containerd, or the daemon regressed. Restart
		// once per regression to converge; never repeatedly, so a node that
		// cannot converge does not get containerd restarted on every retry.
		if i.convergeRestartedFor == desired {
			return fmt.Errorf("runtime handler still not served after a convergence restart: %w", servingErr)
		}
		i.log.Warn("containerd config is up to date but the running daemon does not serve the handler; restarting containerd to converge", "error", servingErr)
		if _, err := i.host.Run(ctx, "systemctl", "restart", "containerd"); err != nil {
			// Nothing was restarted; the retry budget is not spent.
			return err
		}
		i.convergeRestartedFor = desired
		if err := i.awaitRuntimeReady(ctx); err != nil {
			return err
		}
		if err := i.verifyServing(ctx); err != nil {
			return err
		}
		i.convergeRestartedFor = ""
		return nil
	}

	next, err := spliceManagedBlock(raw, desired)
	if err != nil {
		return err
	}

	// Keep a write-once copy of the pre-isola config for manual recovery.
	cfgPath := i.cfg.hostPath(containerdConfigPath)
	backupPath := i.cfg.hostPath(containerdConfigBackupPath)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		if err := writeFileAtomic(backupPath, raw); err != nil {
			return fmt.Errorf("writing pristine config backup: %w", err)
		}
	}

	i.log.Info("updating containerd config", "path", containerdConfigPath, "schemaPlugin", pluginID)
	if err := writeFileAtomic(cfgPath, next); err != nil {
		return fmt.Errorf("writing containerd config: %w", err)
	}

	if _, err := i.host.Run(ctx, "containerd", "config", "dump"); err != nil {
		if restoreErr := writeFileAtomic(cfgPath, raw); restoreErr != nil {
			return fmt.Errorf("new containerd config failed validation (%w) AND restoring the previous config failed: %w", err, restoreErr)
		}
		return fmt.Errorf("new containerd config failed validation; previous config restored, containerd not restarted: %w", err)
	}

	i.log.Warn("restarting containerd to register the gVisor runtime handler (running containers are unaffected)")
	if err := i.restartAndAwaitHealthy(ctx); err != nil {
		i.log.Error("containerd unhealthy after config change; rolling back", "error", err)
		if rbErr := i.rollback(ctx, cfgPath, raw); rbErr != nil {
			return fmt.Errorf("containerd unhealthy after config change (%w) AND rollback failed: %w", err, rbErr)
		}
		return fmt.Errorf("containerd unhealthy after config change; previous config rolled back and containerd recovered: %w", err)
	}

	if err := i.verifyServing(ctx); err != nil {
		return err
	}
	i.convergeRestartedFor = ""
	i.log.Info("gVisor runtime handler registered with containerd", "handler", i.cfg.Handler)
	i.node.Event(corev1.EventTypeNormal, "GVisorRuntimeConfigured",
		fmt.Sprintf("Registered containerd runtime handler %q (gVisor %s)", i.cfg.Handler, i.cfg.Version))
	return nil
}

func (i *Installer) rollback(ctx context.Context, cfgPath string, previous []byte) error {
	if err := writeFileAtomic(cfgPath, previous); err != nil {
		return fmt.Errorf("restoring previous config: %w", err)
	}
	return i.restartAndAwaitHealthy(ctx)
}

func (i *Installer) restartAndAwaitHealthy(ctx context.Context) error {
	if _, err := i.host.Run(ctx, "systemctl", "restart", "containerd"); err != nil {
		return err
	}
	return i.awaitRuntimeReady(ctx)
}

// awaitRuntimeReady polls the CRI RuntimeReady condition — the same signal
// the kubelet uses (it tolerates up to 30s of runtime downtime before
// flagging the node, so a healthy restart is invisible to the cluster).
func (i *Installer) awaitRuntimeReady(ctx context.Context) error {
	deadline := time.Now().Add(i.healthWait)
	var lastErr error
	for time.Now().Before(deadline) {
		st, err := i.cri.Status(ctx)
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

// verifyServing confirms the running containerd serves the handler, over the
// same CRI surface the kubelet uses. The on-disk config is deliberately not
// consulted: it proves nothing about the running daemon (a config written
// but never loaded must not count as installed). RuntimeHandlers requires
// containerd >= 1.7.15; older versions cannot be verified and fail closed.
func (i *Installer) verifyServing(ctx context.Context) error {
	st, err := i.cri.Status(ctx)
	if err != nil {
		return err
	}
	if !st.RuntimeReady {
		return fmt.Errorf("containerd RuntimeReady condition is false")
	}
	if st.Handlers == nil {
		return fmt.Errorf("containerd does not report runtime handlers over CRI; containerd >= 1.7.15 is required")
	}
	if !slices.Contains(st.Handlers, i.cfg.Handler) {
		return fmt.Errorf("runtime handler %q not served by containerd (handlers: %v)", i.cfg.Handler, st.Handlers)
	}
	return nil
}

// publishStatus translates the reconcile outcome into node labels, events
// and pod readiness. Labels reflect observed truth rather than reconcile
// success: a node whose install still works keeps its ready label even if
// e.g. an upgrade download just failed (the failure is surfaced via events,
// readiness and retries instead). Ready requires identity — the on-disk
// managed block matches the current desired render, or a type-checked
// foreign handler, and no type conflict was detected — liveness (the running
// daemon serves the handler), and, for managed nodes, intact binaries. A
// same-named non-gVisor handler must never mark the node ready.
func (i *Installer) publishStatus(ctx context.Context, foreignOK, conflict bool, reconcileErr error) {
	// The reconcile context may already be cancelled (shutdown); still try to
	// publish with a short independent deadline.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	identity := false
	if !conflict {
		if foreignOK {
			identity = true
		} else if raw, err := os.ReadFile(i.cfg.hostPath(containerdConfigPath)); err == nil { //nolint:gosec // fixed managed path
			// Mere marker presence is not enough: a stale block could
			// register a different handler than the currently configured one.
			if pluginID, err := criPluginID(configSchemaVersion(raw)); err == nil {
				desired := renderManagedBlock(pluginID, i.cfg.Handler, i.cfg.shimPath(), runscShimConfigPath)
				current, found, _ := managedBlock(raw)
				identity = found && current == desired
			}
		}
	}

	readyLabel := "false"
	versionLabel := ""
	if identity && i.verifyServing(ctx) == nil {
		if foreignOK {
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
