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
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRecoveryProceedsWhenContainerdUnavailable(t *testing.T) {
	for _, state := range []string{"stopped", "CRI unavailable"} {
		t.Run(state, func(t *testing.T) {
			a := setupUpgradeArtifacts(t)
			a.writeCrashState(t, a.activeV2.ManagedBlock, installerState{Active: &a.activeV1, Pending: a.pendingV2()})
			a.env.cri.set(criStatus{}, errors.New("CRI unavailable after interrupted restart"))
			if state == "stopped" {
				a.env.host.unitShow = "LoadState=loaded\nActiveState=failed\nMainPID=0\n" +
					"ExecStart={ path=/usr/local/bin/containerd ; argv[]=/usr/local/bin/containerd ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }\n"
			}
			restart := a.env.host.onRestart
			a.env.host.onRestart = func() {
				a.env.host.unitShow = healthyUnitShow
				restart()
			}
			fresh := newInstallerLike(a.env.i)
			if err := fresh.Reconcile(t.Context()); err != nil {
				t.Fatalf("could not recover interrupted activation: %v", err)
			}
			if st := a.env.state(t); st.Pending != nil || st.Active.Version != a.activeV2.Version {
				t.Fatalf("target not committed: %+v", st)
			}
		})
	}
}

func TestRecoveryDoesNotForgetFailedRollbackRestart(t *testing.T) {
	a := setupUpgradeArtifacts(t)
	a.writeCrashState(t, a.prevV1, installerState{Active: &a.activeV1, Pending: a.pendingV2()})
	// The target is still loaded while rollback has only restored the file.
	a.env.cri.set(criStatusFromConfig([]byte(a.activeV2.ManagedBlock), "runsc"), nil)
	a.env.i.cfg.Version = a.activeV1.Version
	a.env.host.onRestart = nil
	a.env.host.restartErr = errors.New("restart job failed")
	fresh := newInstallerLike(a.env.i)
	for range 2 {
		if err := fresh.Reconcile(t.Context()); err == nil {
			t.Fatal("failed rollback unexpectedly succeeded")
		}
		if st := a.env.state(t); st.Pending == nil {
			t.Fatal("failed restart cleared the recovery journal")
		}
	}
	before := a.env.host.restarts
	a.env.host.restartErr = nil
	a.env.host.onRestart = func() {
		a.env.cri.set(criStatusFromConfig([]byte(a.prevV1), "runsc"), nil)
	}
	fresh.recoveryGate.notBefore = time.Now().Add(-time.Second)
	if err := fresh.Reconcile(t.Context()); err != nil {
		t.Fatalf("rollback did not retry after cooldown: %v", err)
	}
	if a.env.host.restarts != before+1 || a.env.state(t).Pending != nil {
		t.Fatal("rollback did not settle after the successful retry")
	}
}

func TestRecoveryRefusesUnsupportedStoppedUnit(t *testing.T) {
	a := setupUpgradeArtifacts(t)
	a.writeCrashState(t, a.activeV2.ManagedBlock, installerState{Active: &a.activeV1, Pending: a.pendingV2()})
	a.env.host.unitShow = "LoadState=loaded\nActiveState=failed\nMainPID=0\n" +
		"ExecStart={ path=/usr/bin/containerd ; argv[]=/usr/bin/containerd --config /etc/custom.toml ; ignore_errors=no }\n"
	before := a.env.host.restarts
	if err := newInstallerLike(a.env.i).Reconcile(t.Context()); err == nil || !strings.Contains(err.Error(), "ExecStart") {
		t.Fatalf("unsupported unit accepted: %v", err)
	}
	if a.env.host.restarts != before || a.env.state(t).Pending == nil {
		t.Fatal("unsupported unit was mutated")
	}
}

func TestValidateRecoveryExecStart(t *testing.T) {
	for _, argv := range []string{"/usr/bin/containerd", "/usr/bin/containerd --config=/etc/containerd/config.toml", "/usr/bin/containerd -c /etc/containerd/config.toml"} {
		if err := validateRecoveryExecStart("{ path=/usr/bin/containerd ; argv[]=" + argv + " ; ignore_errors=no }"); err != nil {
			t.Errorf("direct standard invocation rejected: %v", err)
		}
	}
	for _, value := range []string{
		"",
		"{ path=/bin/sh ; argv[]=/bin/sh -c containerd ; ignore_errors=no }",
		"{ path=/usr/bin/containerd ; argv[]=/usr/bin/containerd --config /etc/custom.toml ; ignore_errors=no }",
		"{ path=/usr/bin/containerd ; argv[]=/usr/bin/containerd $OPTIONS ; ignore_errors=no }",
	} {
		if err := validateRecoveryExecStart(value); err == nil {
			t.Errorf("ambiguous invocation accepted: %q", value)
		}
	}
}

func TestReconcileRejectsForeignHandlerLoadedAsRunc(t *testing.T) {
	env := newTestEnv(t)
	foreign := kindStyleConfig + `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.runsc]
  runtime_type = "io.containerd.runsc.v1"
`
	if err := writeFileAtomic(env.i.cfg.hostPath(containerdConfigPath), []byte(foreign)); err != nil {
		t.Fatal(err)
	}
	// A changed config has not yet been loaded by the live daemon.
	env.cri.set(criStatus{
		RuntimeReady: true,
		Handlers:     []string{"runc", "runsc"},
		Runtimes:     map[string]criRuntime{"runsc": {Type: "io.containerd.runc.v2"}},
	}, nil)
	if err := env.i.Reconcile(t.Context()); err == nil {
		t.Fatal("live runc handler was accepted as gVisor")
	}
	if env.node.label(LabelGVisorReady) != "false" || env.host.restarts != 0 {
		t.Fatal("foreign handler was marked ready or modified")
	}
}

func TestRecoveryDoesNotRestartWithInvalidSurroundingConfig(t *testing.T) {
	a := setupUpgradeArtifacts(t)
	a.writeCrashState(t, a.activeV2.ManagedBlock, installerState{Active: &a.activeV1, Pending: a.pendingV2()})
	a.env.i.cfg.Version = a.activeV1.Version
	cfgPath := a.env.i.cfg.hostPath(containerdConfigPath)
	valid := a.env.configOnDisk(t)
	invalid := "invalid TOML\n" + valid
	if err := writeFileAtomic(cfgPath, []byte(invalid)); err != nil {
		t.Fatal(err)
	}
	readDump := a.env.host.dumpFunc
	a.env.host.dumpFunc = func() ([]byte, error) {
		data, err := readDump()
		if strings.HasPrefix(string(data), "invalid TOML\n") {
			return nil, errors.New("invalid surrounding TOML")
		}
		return data, err
	}
	before := a.env.host.restarts
	fresh := newInstallerLike(a.env.i)
	if err := fresh.Reconcile(t.Context()); err == nil {
		t.Fatal("invalid config unexpectedly recovered")
	}
	if a.env.host.restarts != before {
		t.Fatal("recovery restarted a healthy daemon onto invalid config")
	}
	if a.env.state(t).Pending == nil {
		t.Fatal("invalid config cleared the recovery journal")
	}
	if !strings.HasPrefix(a.env.configOnDisk(t), "invalid TOML\n") {
		t.Fatal("recovery overwrote the unrelated edit")
	}
	// The administrator fixes the unrelated edit. Recovery retries against
	// those bytes and settles on the predecessor without another activation.
	repaired := strings.TrimPrefix(a.env.configOnDisk(t), "invalid TOML\n")
	if err := writeFileAtomic(cfgPath, []byte(repaired)); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Reconcile(t.Context()); err != nil {
		t.Fatalf("recovery failed after fixing the config: %v", err)
	}
	st := a.env.state(t)
	if a.env.host.restarts != before+1 || st.Pending != nil || st.Active == nil || st.Active.Version != a.activeV1.Version {
		t.Fatalf("repaired config did not converge: restarts=%d state=%+v", a.env.host.restarts-before, st)
	}
}
