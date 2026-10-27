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
	"os"
	"testing"
	"time"
)

func TestConvergenceFailedRestartRecovers(t *testing.T) {
	for _, failure := range []string{"restart command", "health check"} {
		for _, freshProcess := range []bool{false, true} {
			name := failure + "/same process"
			if freshProcess {
				name = failure + "/new process"
			}
			t.Run(name, func(t *testing.T) {
				env := newTestEnv(t)
				if err := env.i.Reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
				active := env.activeGen(t)
				restart := env.host.onRestart
				env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)
				env.host.onRestart = func() {
					if env.state(t).Pending == nil {
						t.Error("restart began without a durable recovery journal")
					}
					env.host.unitShow = "LoadState=loaded\nActiveState=failed\nMainPID=0\n" +
						"ExecStart={ path=/usr/bin/containerd ; argv[]=/usr/bin/containerd ; ignore_errors=no }\n"
					env.cri.set(criStatus{}, errors.New("containerd stopped"))
				}
				if failure == "restart command" {
					env.host.restartErr = errors.New("containerd start failed")
				}
				if err := env.i.Reconcile(t.Context()); err == nil {
					t.Fatal("expected convergence restart to fail")
				}
				if st := env.state(t); st.Pending == nil || st.Pending.Target != active || st.Active == nil || *st.Active != active {
					t.Fatalf("failed convergence lost its recovery intent: %+v", st)
				}
				if env.i.Ready() || env.node.label(LabelGVisorReady) != "false" {
					t.Fatal("stopped runtime remains ready")
				}
				attempts := env.host.restarts
				for range 2 {
					if err := env.i.Reconcile(t.Context()); err == nil {
						t.Fatal("expected pending recovery to remain blocked")
					}
				}
				if env.host.restarts != attempts || env.state(t).Pending == nil {
					t.Fatal("recovery bypassed its cooldown or lost the journal")
				}

				// Once containerd can start again, recovery must work whether
				// the installer kept running or restarted.
				env.host.restartErr = nil
				env.host.onRestart = func() {
					env.host.unitShow = healthyUnitShow
					restart()
				}
				recovering := env.i
				if freshProcess {
					recovering = newInstallerLike(env.i)
				} else {
					recovering.recoveryGate.notBefore = time.Now().Add(-time.Second)
				}
				if err := recovering.Reconcile(t.Context()); err != nil {
					t.Fatalf("could not recover stopped containerd: %v", err)
				}
				if st := env.state(t); st.Pending != nil || st.Active == nil || *st.Active != active {
					t.Fatalf("recovery did not settle the original generation: %+v", st)
				}
				if !recovering.Ready() || env.host.restarts != attempts+1 {
					t.Fatal("runtime did not recover with exactly one restart")
				}

				// A later loss of the handler must allow another containerd restart.
				env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)
				if err := recovering.Reconcile(t.Context()); err != nil {
					t.Fatalf("later regression did not converge: %v", err)
				}
				if env.host.restarts != attempts+2 {
					t.Fatal("later regression did not restart containerd")
				}
			})
		}
	}
}

func TestConvergenceDoesNotRestartWithoutJournal(t *testing.T) {
	env := newTestEnv(t)
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	st := env.state(t)
	gen, err := env.i.loadGeneration(st.Active.GenerationPath)
	if err != nil {
		t.Fatal(err)
	}
	target := runtimeTarget{ShimPath: gen.shimPath(), ConfigPath: st.Active.ConfigPath}
	raw := []byte(env.configOnDisk(t))
	env.cri.set(criStatus{RuntimeReady: true, Handlers: []string{"runc"}}, nil)

	statePath := env.i.statePath()
	saved := statePath + ".saved"
	if err := os.Rename(statePath, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	attempts := env.host.restarts
	if _, err := env.i.ensureContainerdConfig(t.Context(), &st, raw, gen, target); err == nil {
		t.Fatal("expected journal write to fail")
	}
	if env.host.restarts != attempts || env.i.convergeRestartedFor != "" {
		t.Fatal("journal failure restarted containerd or spent the restart budget")
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, statePath); err != nil {
		t.Fatal(err)
	}
	if err := env.i.Reconcile(t.Context()); err != nil {
		t.Fatalf("convergence did not retry after journal writes recovered: %v", err)
	}
}
