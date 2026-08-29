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

package poolhash

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

// baseSpec returns a concrete SandboxSpec that test cases mutate via the
// mutator; keeping the baseline fully populated exercises every included
// field in the hash input.
func baseSpec() sandboxv1alpha1.SandboxSpec {
	return sandboxv1alpha1.SandboxSpec{
		PodTemplate: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "c1",
						Image: "busybox:latest",
						Env: []corev1.EnvVar{
							{Name: "FOO", Value: "bar"},
						},
					},
				},
			},
		},
		TimeoutSeconds:        ptr.To[int64](60),
		StartupTimeoutSeconds: ptr.To[int64](90),
		TerminationPolicy: &sandboxv1alpha1.TerminationPolicy{
			Type: sandboxv1alpha1.TerminationTypeDelete,
		},
		Network: &sandboxv1alpha1.Network{
			AllowClusterDNS: ptr.To(true),
		},
		RootfsSnapshotSources: []sandboxv1alpha1.RootfsSnapshotSource{
			{SnapshotName: "snap1"},
		},
	}
}

func TestCompute(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*sandboxv1alpha1.SandboxSpec)
		sameAsA bool
	}{
		{
			name:    "identical spec",
			mutate:  func(*sandboxv1alpha1.SandboxSpec) {},
			sameAsA: true,
		},
		{
			name: "differing TimeoutSeconds is ignored",
			mutate: func(s *sandboxv1alpha1.SandboxSpec) {
				s.TimeoutSeconds = ptr.To[int64](120)
			},
			sameAsA: true,
		},
		{
			name: "differing StartupTimeoutSeconds is ignored",
			mutate: func(s *sandboxv1alpha1.SandboxSpec) {
				s.StartupTimeoutSeconds = ptr.To[int64](30)
			},
			sameAsA: true,
		},
		{
			name: "differing Network changes the hash",
			mutate: func(s *sandboxv1alpha1.SandboxSpec) {
				s.Network = &sandboxv1alpha1.Network{AllowClusterDNS: ptr.To(false)}
			},
			sameAsA: false,
		},
		{
			name: "differing RootfsSnapshotSources changes the hash",
			mutate: func(s *sandboxv1alpha1.SandboxSpec) {
				s.RootfsSnapshotSources = []sandboxv1alpha1.RootfsSnapshotSource{
					{SnapshotName: "snap2"},
				}
			},
			sameAsA: false,
		},
		{
			name: "differing TerminationPolicy changes the hash",
			mutate: func(s *sandboxv1alpha1.SandboxSpec) {
				s.TerminationPolicy = &sandboxv1alpha1.TerminationPolicy{
					Type: sandboxv1alpha1.TerminationTypeSnapshotRootfs,
					SnapshotRootfs: &sandboxv1alpha1.SnapshotRootfsTermination{
						SnapshotName: "snap-term",
					},
				}
			},
			sameAsA: false,
		},
		{
			name: "differing container Image changes the hash",
			mutate: func(s *sandboxv1alpha1.SandboxSpec) {
				s.PodTemplate.Spec.Containers[0].Image = "alpine:latest"
			},
			sameAsA: false,
		},
		{
			name: "differing container Env changes the hash",
			mutate: func(s *sandboxv1alpha1.SandboxSpec) {
				s.PodTemplate.Spec.Containers[0].Env = []corev1.EnvVar{
					{Name: "FOO", Value: "different"},
				}
			},
			sameAsA: false,
		},
	}

	base := Compute(baseSpec())
	if base == "" {
		t.Fatal("base hash is empty")
	}
	if len(base) != 10 {
		t.Fatalf("base hash length = %d, want 10", len(base))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := baseSpec()
			tc.mutate(&s)
			got := Compute(s)
			if tc.sameAsA && got != base {
				t.Errorf("expected same hash as base %q, got %q", base, got)
			}
			if !tc.sameAsA && got == base {
				t.Errorf("expected different hash from base %q", base)
			}
		})
	}
}

// TestComputeStable pins the hash of a fixed spec. If this test breaks, the
// hash function changed semantics — existing pool-template-hash labels on
// warm-pool children will no longer match, triggering a full pool rollout.
// Update the literal deliberately when that is the intent.
func TestComputeStable(t *testing.T) {
	spec := sandboxv1alpha1.SandboxSpec{
		PodTemplate: corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "c1", Image: "busybox:1.36"},
				},
			},
		},
	}
	const want = "88e998b48f"
	got := Compute(spec)
	if got != want {
		t.Errorf("Compute() = %q, want %q", got, want)
	}
}
