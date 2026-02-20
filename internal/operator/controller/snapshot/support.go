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

package snapshot

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CheckRootfsSnapshotSupport checks if the pod's runtime class supports snapshotting.
// Returns true if the pod uses a gvisor/runsc runtime.
// Caller should check pod readiness before calling this.
func CheckRootfsSnapshotSupport(ctx context.Context, c client.Client, pod *corev1.Pod) (bool, error) {
	if pod == nil {
		return false, nil
	}

	runtimeClassName := pod.Spec.RuntimeClassName
	if runtimeClassName == nil || *runtimeClassName == "" {
		return false, nil
	}

	runtimeClass := &nodev1.RuntimeClass{}
	if err := c.Get(ctx, types.NamespacedName{Name: *runtimeClassName}, runtimeClass); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return runtimeClass.Handler == "runsc" || runtimeClass.Handler == "gvisor", nil
}
