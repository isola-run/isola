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

	// Post-restart health check pacing (shortened in tests).
	healthWait time.Duration
	healthPoll time.Duration
}

// New assembles an Installer from its collaborators (separated for testing).
func New(cfg Config, log *slog.Logger, host HostExec, cri CRIClient, node NodeClient) *Installer {
	return &Installer{
		cfg: cfg, log: log, host: host, cri: cri, node: node,
		healthWait: 90 * time.Second,
		healthPoll: 2 * time.Second,
	}
}

// Ready reports whether the last reconcile succeeded (readiness probe).
func (i *Installer) Ready() bool { return i.ready.Load() }

// Run reconciles in a loop until the context is cancelled. There is
// deliberately no cleanup on shutdown: removing a runtime handler out from
// under running sandboxes breaks them, and a DaemonSet pod cannot
// distinguish "helm uninstall" from a rolling update or eviction.
func (i *Installer) Run(ctx context.Context) {
	for {
		interval := i.cfg.ReconcileInterval
		if err := i.Reconcile(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			i.log.Error("reconcile failed", "error", err)
			interval = i.cfg.RetryInterval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

// Reconcile drives the node to the desired state:
//
//  1. refuse unsupported distros (k3s/RKE2/... manage containerd config
//     elsewhere) rather than edit a file nobody reads;
//  2. if a non-isola runsc handler already exists, adopt nothing and touch
//     nothing — verify it and report the node ready (foreign mode);
//  3. ensure binaries (download/verify/atomically replace; no containerd
//     restart needed — running sandboxes keep their inode);
//  4. ensure the runsc shim config (read by the shim per sandbox start; no
//     restart needed);
//  5. ensure the managed block in containerd's config.toml — the only step
//     that restarts containerd, guarded by validate/health-check/rollback.
//
// Whatever happens, node labels are updated at the end to reflect the
// *observed* state, so a failed upgrade on a previously working node keeps
// the node schedulable for sandboxes.
func (i *Installer) Reconcile(ctx context.Context) (err error) {
	foreign := false
	defer func() {
		i.publishStatus(ctx, foreign, err)
	}()

	if err := i.checkSupportedDistro(ctx); err != nil {
		return err
	}

	raw, err := os.ReadFile(i.cfg.hostPath(containerdConfigPath)) //nolint:gosec // fixed managed path
	if err != nil {
		return fmt.Errorf("reading containerd config (a standard containerd installation with %s is required): %w", containerdConfigPath, err)
	}

	// `containerd config dump` is both the merged-config view (file +
	// imports + defaults, post-migration) and the only validation containerd
	// offers: it exits non-zero if the current config does not load.
	dump, err := i.host.Run(ctx, "containerd", "config", "dump")
	if err != nil {
		return fmt.Errorf("containerd's current configuration failed to load; refusing to modify anything: %w", err)
	}

	_, managed, err := managedBlock(raw)
	if err != nil {
		return err
	}
	if !managed {
		if rt, found := runtimeFromDump(dump, i.cfg.Handler); found {
			// A handler we did not write already exists. Respect it: verify
			// it is actually a runsc runtime and leave the node alone.
			foreign = true
			if rtType, _ := rt["runtime_type"].(string); rtType != runscRuntimeType {
				return fmt.Errorf("containerd already has a runtime handler %q with runtime_type %q (expected %s); "+
					"resolve the conflict by removing it or by configuring a different gvisor.autoInstall.handler",
					i.cfg.Handler, rt["runtime_type"], runscRuntimeType)
			}
			i.log.Info("pre-existing runsc handler detected; leaving node unmanaged", "handler", i.cfg.Handler)
			return nil
		}
	}

	if _, err := i.ensureBinaries(ctx); err != nil {
		return err
	}
	if err := i.ensureRunscShimConfig(dump); err != nil {
		return err
	}
	return i.ensureContainerdConfig(ctx, raw)
}

// ensureContainerdConfig reconciles the managed block in config.toml. When a
// change is required it follows the full safety chain: pristine backup →
// atomic write → validate → restart → CRI health check → rollback on any
// failure. The installer pod itself survives containerd restarts (shims keep
// containers running), which is what makes in-place rollback possible.
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
		return i.verifyHandlerRegistered(ctx)
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

	if err := i.verifyHandlerRegistered(ctx); err != nil {
		return err
	}
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

// verifyHandlerRegistered confirms the running containerd actually serves
// the handler. Prefers the CRI RuntimeHandlers field; falls back to the
// merged config dump for containerd versions that don't report handlers.
func (i *Installer) verifyHandlerRegistered(ctx context.Context) error {
	st, err := i.cri.Status(ctx)
	if err != nil {
		return err
	}
	if !st.RuntimeReady {
		return fmt.Errorf("containerd RuntimeReady condition is false")
	}
	if st.Handlers != nil {
		for _, h := range st.Handlers {
			if h == i.cfg.Handler {
				return nil
			}
		}
		return fmt.Errorf("runtime handler %q not reported by containerd (handlers: %v)", i.cfg.Handler, st.Handlers)
	}
	dump, err := i.host.Run(ctx, "containerd", "config", "dump")
	if err != nil {
		return err
	}
	if _, found := runtimeFromDump(dump, i.cfg.Handler); !found {
		return fmt.Errorf("runtime handler %q not present in containerd's merged config", i.cfg.Handler)
	}
	return nil
}

// publishStatus translates the reconcile outcome into node labels, events and
// pod readiness. Labels reflect observed truth rather than reconcile success:
// a node whose handler still works keeps its ready label even if e.g. an
// upgrade download just failed (the failure is surfaced via events,
// readiness and retries instead).
func (i *Installer) publishStatus(ctx context.Context, foreign bool, reconcileErr error) {
	// The reconcile context may already be cancelled (shutdown); still try to
	// publish with a short independent deadline.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	readyLabel := "false"
	versionLabel := ""
	if err := i.verifyHandlerRegistered(ctx); err == nil {
		readyLabel = "true"
		if foreign {
			versionLabel = VersionUnmanaged
		} else {
			versionLabel = i.installedVersion(ctx)
		}
	}

	if err := i.node.SetNodeLabels(ctx, map[string]string{
		LabelGVisorReady:   readyLabel,
		LabelGVisorVersion: versionLabel,
	}); err != nil {
		i.log.Error("updating node labels failed", "error", err)
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

	i.ready.Store(reconcileErr == nil && readyLabel == "true")
}
