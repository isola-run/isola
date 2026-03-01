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
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCheckRootfsSnapshotSupport(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = nodev1.AddToScheme(scheme)

	runscRuntimeClass := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "runsc"},
		Handler:    "runsc",
	}
	gvisorRuntimeClass := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gvisor"},
		Handler:    "gvisor",
	}
	runcRuntimeClass := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "runc"},
		Handler:    "runc",
	}

	runscName := "runsc"
	gvisorName := "gvisor"
	runcName := "runc"
	nonexistentName := "nonexistent"

	readyPod := func(runtimeClassName *string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
			Spec: corev1.PodSpec{
				RuntimeClassName: runtimeClassName,
				Containers:       []corev1.Container{{Name: "main", Image: "busybox"}},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}
	}

	tests := []struct {
		name           string
		pod            *corev1.Pod
		runtimeClasses []runtime.Object
		wantSupported  bool
		wantErr        bool
	}{
		{
			name:          "nil pod",
			pod:           nil,
			wantSupported: false,
		},
		{
			name:          "no runtime class",
			pod:           readyPod(nil),
			wantSupported: false,
		},
		{
			name:          "empty runtime class",
			pod:           readyPod(func() *string { s := ""; return &s }()),
			wantSupported: false,
		},
		{
			name:          "runtime class not found",
			pod:           readyPod(&nonexistentName),
			wantSupported: false,
			wantErr:       false, // not-found is not an error, just unsupported
		},
		{
			name:           "runsc runtime - supported",
			pod:            readyPod(&runscName),
			runtimeClasses: []runtime.Object{runscRuntimeClass},
			wantSupported:  true,
		},
		{
			name:           "gvisor runtime - supported",
			pod:            readyPod(&gvisorName),
			runtimeClasses: []runtime.Object{gvisorRuntimeClass},
			wantSupported:  true,
		},
		{
			name:           "runc runtime - unsupported",
			pod:            readyPod(&runcName),
			runtimeClasses: []runtime.Object{runcRuntimeClass},
			wantSupported:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			objs := tt.runtimeClasses
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			supported, err := CheckRootfsSnapshotSupport(context.Background(), c, tt.pod)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}

			g.Expect(supported).To(Equal(tt.wantSupported))
		})
	}
}
