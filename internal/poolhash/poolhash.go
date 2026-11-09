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

// Package poolhash computes a canonical hash over a SandboxSpec for warm-pool
// matching. The hash is computed identically by the api-gateway (when
// matching an incoming request to a pool) and by the WarmSandboxPool
// controller (when stamping pool-template-hash labels on children).
//
// Included fields are those baked into the Pod and thus immutable post-start:
// PodTemplate.Spec, Network, RootfsSnapshotSources, TerminationPolicy.
//
// Excluded fields are those settable or re-anchored at adoption:
// TimeoutSeconds, StartupTimeoutSeconds, and metadata labels/annotations.
package poolhash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	corev1 "k8s.io/api/core/v1"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
)

// hashInput is the struct whose JSON encoding feeds the hasher. Field names
// are fixed (not taken from SandboxSpec json tags) so renames on the CRD do
// not invalidate existing pool hashes unless the semantic of an included
// field actually changes.
type hashInput struct {
	PodSpec               corev1.PodSpec                          `json:"podSpec"`
	Network               *sandboxv1alpha1.Network                `json:"network,omitempty"`
	RootfsSnapshotSources []sandboxv1alpha1.RootfsSnapshotSource  `json:"rootfsSnapshotSources,omitempty"`
	TerminationPolicy     *sandboxv1alpha1.TerminationPolicy      `json:"terminationPolicy,omitempty"`
}

// Compute returns the canonical pool-template hash of a SandboxSpec, truncated
// to 10 hex chars so it is safe as a Kubernetes label value.
func Compute(spec sandboxv1alpha1.SandboxSpec) string {
	in := hashInput{
		PodSpec:               spec.PodTemplate.Spec,
		Network:               spec.Network,
		RootfsSnapshotSources: spec.RootfsSnapshotSources,
		TerminationPolicy:     spec.TerminationPolicy,
	}
	// json.Marshal on structs emits fields in declaration order and never
	// returns an error for well-formed Go structs built from Kubernetes API
	// types; a hard failure here would indicate a programmer error, so we
	// fall back to a sentinel rather than propagate.
	data, err := json.Marshal(in)
	if err != nil {
		return "errhash0000"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:10]
}
