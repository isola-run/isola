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

package controller

import (
	"slices"
	"testing"
)

// Without the installer there are no per-release directories to resolve, so
// the job must run the configured runsc directly rather than the launcher.
func TestSnapshotterCommandUnmanagedNode(t *testing.T) {
	r := &RootfsSnapshotReconciler{
		GvisorRunscPath: "/usr/local/bin/runsc",
		GvisorRunscRoot: "/run/containerd/runsc/k8s.io",
		UploaderImage:   "example.com/uploader:v1",
	}
	image, cmd, args := r.snapshotterCommand("abc", "/snapshot/rootfs.tar")
	if image == r.UploaderImage {
		t.Error("unmanaged nodes must not need the uploader image")
	}
	if len(cmd) != 1 || cmd[0] != "/usr/local/bin/runsc" {
		t.Errorf("command = %v, want the configured runsc", cmd)
	}
	if args[0] != "--root=/run/containerd/runsc/k8s.io" {
		t.Errorf("args = %v", args)
	}
	for _, m := range r.snapshotterMounts(r.GvisorRunscRoot) {
		if m.MountPath == "" {
			t.Errorf("mount %q has an empty path", m.Name)
		}
	}
	for _, v := range r.snapshotterHostVolumes() {
		if v.HostPath == nil || v.HostPath.Path == "" {
			t.Errorf("volume %q has an empty host path", v.Name)
		}
	}
}

func TestSnapshotterCommandManagedNode(t *testing.T) {
	r := &RootfsSnapshotReconciler{
		GvisorRunscRoot:    "/run/containerd/runsc/k8s.io",
		GvisorInstallDir:   "/opt/isola/bin",
		ContainerdStateDir: "/run/containerd",
		UploaderImage:      "example.com/uploader:v1",
	}
	image, cmd, args := r.snapshotterCommand("abc", "/snapshot/rootfs.tar")
	if image != r.UploaderImage || len(cmd) != 1 || cmd[0] != runscLauncherPath {
		t.Fatalf("image/command = %q/%v, want the launcher", image, cmd)
	}
	if !slices.Contains(args, "--container-id=abc") || !slices.Contains(args, "--") {
		t.Errorf("args = %v", args)
	}
	for _, m := range r.snapshotterMounts(r.GvisorRunscRoot) {
		if m.MountPath == "" {
			t.Errorf("mount %q has an empty path", m.Name)
		}
	}
}
