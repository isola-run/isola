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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testNS  = "k8s.io"
	testCID = "abc123"
	testGen = "20260721.0-deadbeef"
)

func newBundle(t *testing.T, shimPath, stateBinaryName string) (bundleRoot, installDir string) {
	t.Helper()
	root := t.TempDir()
	bundleRoot = filepath.Join(root, "task")
	installDir = filepath.Join(root, "opt", "isola", "bin")
	bundle := filepath.Join(bundleRoot, testNS, testCID)
	if err := os.MkdirAll(bundle, 0o750); err != nil {
		t.Fatal(err)
	}
	if shimPath != "" {
		shimPath = strings.ReplaceAll(shimPath, "$INSTALL", installDir)
		if err := os.WriteFile(filepath.Join(bundle, shimBinaryPathFile), []byte(shimPath), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if stateBinaryName != "" {
		stateBinaryName = strings.ReplaceAll(stateBinaryName, "$INSTALL", installDir)
		body := `{"Rootfs":"/x","Options":{"BinaryName":"` + stateBinaryName + `"}}`
		if err := os.WriteFile(filepath.Join(bundle, "state.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return bundleRoot, installDir
}

func TestResolveRunscUsesTheSandboxGeneration(t *testing.T) {
	bundleRoot, installDir := newBundle(t,
		"$INSTALL/releases/"+testGen+"/containerd-shim-runsc-v1",
		"$INSTALL/releases/"+testGen+"/runsc")

	got, err := resolveRunsc(options{
		bundleRoot: bundleRoot, namespace: testNS, containerID: testCID,
		installDir: installDir, fallback: "/usr/local/bin/runsc",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(installDir, "releases", testGen, "runsc")
	if got != want {
		t.Errorf("resolveRunsc() = %q, want %q", got, want)
	}
}

// The whole point is not silently running the node's newest release.
func TestResolveRunscIgnoresOtherGenerations(t *testing.T) {
	bundleRoot, installDir := newBundle(t,
		"$INSTALL/releases/20260101.0-old/containerd-shim-runsc-v1", "")

	got, err := resolveRunsc(options{
		bundleRoot: bundleRoot, namespace: testNS, containerID: testCID,
		installDir: installDir, fallback: "/usr/local/bin/runsc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(installDir, "releases", "20260101.0-old", "runsc"); got != want {
		t.Errorf("resolveRunsc() = %q, want %q", got, want)
	}
}

func TestResolveRunscFallsBackForUnmanagedSandboxes(t *testing.T) {
	tests := map[string]string{
		"no bundle recorded":   "",
		"shim outside install": "/usr/local/bin/containerd-shim-runsc-v1",
	}
	for name, shim := range tests {
		t.Run(name, func(t *testing.T) {
			bundleRoot, installDir := newBundle(t, shim, "")
			got, err := resolveRunsc(options{
				bundleRoot: bundleRoot, namespace: testNS, containerID: testCID,
				installDir: installDir, fallback: "/usr/local/bin/runsc",
			})
			if err != nil || got != "/usr/local/bin/runsc" {
				t.Errorf("resolveRunsc() = (%q, %v), want the fallback", got, err)
			}
		})
	}
}

func TestResolveRunscFailsClosedWithoutFallback(t *testing.T) {
	bundleRoot, installDir := newBundle(t, "/usr/local/bin/containerd-shim-runsc-v1", "")
	if _, err := resolveRunsc(options{
		bundleRoot: bundleRoot, namespace: testNS, containerID: testCID,
		installDir: installDir,
	}); err == nil {
		t.Fatal("expected an error when nothing is resolvable and no fallback is set")
	}
}

// Disagreement means one of the records was rewritten, and picking either
// could pair a sentry with a different release.
func TestResolveRunscRejectsConflictingRecords(t *testing.T) {
	bundleRoot, installDir := newBundle(t,
		"$INSTALL/releases/"+testGen+"/containerd-shim-runsc-v1",
		"$INSTALL/releases/20260101.0-other/runsc")

	_, err := resolveRunsc(options{
		bundleRoot: bundleRoot, namespace: testNS, containerID: testCID,
		installDir: installDir, fallback: "/usr/local/bin/runsc",
	})
	if err == nil || !strings.Contains(err.Error(), "shim state records") {
		t.Fatalf("error = %v, want a conflict rejection", err)
	}
}

func TestResolveRunscRejectsUnsafeContainerID(t *testing.T) {
	bundleRoot, installDir := newBundle(t, "", "")
	for _, id := range []string{"..", ".", "", "../../etc"} {
		if _, err := resolveRunsc(options{
			bundleRoot: bundleRoot, namespace: testNS, containerID: id,
			installDir: installDir, fallback: "/usr/local/bin/runsc",
		}); err == nil {
			t.Errorf("container id %q was accepted", id)
		}
	}
}

func TestParseArgs(t *testing.T) {
	ok, err := parseArgs([]string{
		"--bundle-root=/run/containerd/task", "--namespace=k8s.io",
		"--container-id=abc", "--install-dir=/opt/isola/bin",
		"--", "--root=/run/containerd/runsc/k8s.io", "tar",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ok.args) != 2 || ok.args[0] != "--root=/run/containerd/runsc/k8s.io" {
		t.Errorf("runsc args = %v", ok.args)
	}
	if ok.fallback != "" {
		t.Errorf("fallback = %q, want it to be optional", ok.fallback)
	}

	for name, argv := range map[string][]string{
		"missing install dir": {"--bundle-root=/a", "--namespace=k8s.io", "--container-id=abc", "--", "x"},
		"no runsc args":       {"--bundle-root=/a", "--namespace=k8s.io", "--container-id=abc", "--install-dir=/b"},
		"unknown flag":        {"--nope=1", "--", "x"},
		"bare argument":       {"positional", "--", "x"},
	} {
		if _, err := parseArgs(argv); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
