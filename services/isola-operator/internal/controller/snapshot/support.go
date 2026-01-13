/*
Copyright 2025 isola.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package snapshot

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/omereli/dev-isola/services/isola-operator/internal/controller/podutil"
)

// GetSandboxPodName returns the pod name for a sandbox
func GetSandboxPodName(sandboxName string) string {
	return sandboxName + "-pod"
}

// CheckRootfsSnapshotSupport validates the pod can be snapshotted.
// Returns:
//   - supported: true if the pod uses a gvisor/runsc runtime
//   - retryable: true if failure is transient (e.g., pod not ready yet)
//   - err: non-nil only for unexpected API failures
func CheckRootfsSnapshotSupport(ctx context.Context, c client.Client, pod *corev1.Pod) (supported bool, retryable bool, err error) {
	if pod == nil {
		return false, false, nil
	}
	if !podutil.IsPodReady(pod) {
		return false, true, nil
	}

	runtimeClassName := pod.Spec.RuntimeClassName
	if runtimeClassName == nil || *runtimeClassName == "" {
		return false, false, nil
	}

	runtimeClass := &nodev1.RuntimeClass{}
	if err := c.Get(ctx, types.NamespacedName{Name: *runtimeClassName}, runtimeClass); err != nil {
		if apierrors.IsNotFound(err) {
			return false, false, nil
		}
		return false, false, err
	}

	if runtimeClass.Handler == "runsc" || runtimeClass.Handler == "gvisor" {
		return true, false, nil
	}

	return false, false, nil
}
