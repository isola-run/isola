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
	"testing"

	corev1 "k8s.io/api/core/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetSandboxPodName(t *testing.T) {
	tests := []struct {
		name        string
		sandboxName string
		want        string
	}{
		{"simple name", "my-sandbox", "my-sandbox-pod"},
		{"empty name", "", "-pod"},
		{"with dashes", "test-sandbox-123", "test-sandbox-123-pod"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetSandboxPodName(tt.sandboxName)
			if got != tt.want {
				t.Errorf("GetSandboxPodName(%q) = %q, want %q", tt.sandboxName, got, tt.want)
			}
		})
	}
}

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

	pendingPod := func(runtimeClassName *string) *corev1.Pod {
		return &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
			Spec: corev1.PodSpec{
				RuntimeClassName: runtimeClassName,
				Containers:       []corev1.Container{{Name: "main", Image: "busybox"}},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodPending,
			},
		}
	}

	tests := []struct {
		name            string
		pod             *corev1.Pod
		runtimeClasses  []runtime.Object
		wantSupported   bool
		wantReason      string
		wantErrContains string
	}{
		{
			name:          "nil pod",
			pod:           nil,
			wantSupported: false,
			wantReason:    ReasonPodDoesNotExist,
		},
		{
			name:          "pod not ready",
			pod:           pendingPod(&runscName),
			wantSupported: false,
			wantReason:    ReasonPodNotReady,
		},
		{
			name:          "no runtime class",
			pod:           readyPod(nil),
			wantSupported: false,
			wantReason:    ReasonRuntimeClassMissing,
		},
		{
			name:          "empty runtime class",
			pod:           readyPod(func() *string { s := ""; return &s }()),
			wantSupported: false,
			wantReason:    ReasonRuntimeClassMissing,
		},
		{
			name:            "runtime class not found",
			pod:             readyPod(&nonexistentName),
			wantSupported:   false,
			wantReason:      ReasonRuntimeClassNotFound,
			wantErrContains: "not found",
		},
		{
			name:           "runsc runtime - supported",
			pod:            readyPod(&runscName),
			runtimeClasses: []runtime.Object{runscRuntimeClass},
			wantSupported:  true,
			wantReason:     ReasonSupported,
		},
		{
			name:           "gvisor runtime - supported",
			pod:            readyPod(&gvisorName),
			runtimeClasses: []runtime.Object{gvisorRuntimeClass},
			wantSupported:  true,
			wantReason:     ReasonSupported,
		},
		{
			name:           "runc runtime - unsupported",
			pod:            readyPod(&runcName),
			runtimeClasses: []runtime.Object{runcRuntimeClass},
			wantSupported:  false,
			wantReason:     ReasonRuntimeUnsupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs := tt.runtimeClasses
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).Build()

			supported, reason, err := CheckRootfsSnapshotSupport(context.Background(), c, tt.pod)

			if tt.wantErrContains != "" {
				if err == nil || !contains(err.Error(), tt.wantErrContains) {
					t.Errorf("CheckRootfsSnapshotSupport() error = %v, want error containing %q", err, tt.wantErrContains)
				}
			} else if err != nil {
				t.Errorf("CheckRootfsSnapshotSupport() unexpected error = %v", err)
			}

			if supported != tt.wantSupported {
				t.Errorf("CheckRootfsSnapshotSupport() supported = %v, want %v", supported, tt.wantSupported)
			}

			if reason != tt.wantReason {
				t.Errorf("CheckRootfsSnapshotSupport() reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestExtractContainerID(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		containerName string
		wantID        string
		wantErr       bool
	}{
		{
			name:          "nil pod",
			pod:           nil,
			containerName: "main",
			wantErr:       true,
		},
		{
			name: "no container statuses",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status:     corev1.PodStatus{},
			},
			containerName: "main",
			wantErr:       true,
		},
		{
			name: "container not found",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "other", ContainerID: "containerd://abc123"},
					},
				},
			},
			containerName: "main",
			wantErr:       true,
		},
		{
			name: "container has no ID",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "main", ContainerID: ""},
					},
				},
			},
			containerName: "main",
			wantErr:       true,
		},
		{
			name: "invalid container ID format",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "main", ContainerID: "invalid-format"},
					},
				},
			},
			containerName: "main",
			wantErr:       true,
		},
		{
			name: "valid container ID - containerd",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "main", ContainerID: "containerd://abc123def456"},
					},
				},
			},
			containerName: "main",
			wantID:        "abc123def456",
			wantErr:       false,
		},
		{
			name: "valid container ID - docker",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "sidecar", ContainerID: "docker://xyz789"},
					},
				},
			},
			containerName: "sidecar",
			wantID:        "xyz789",
			wantErr:       false,
		},
		{
			name: "multiple containers - get correct one",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
				Status: corev1.PodStatus{
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "first", ContainerID: "containerd://111"},
						{Name: "second", ContainerID: "containerd://222"},
						{Name: "third", ContainerID: "containerd://333"},
					},
				},
			},
			containerName: "second",
			wantID:        "222",
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractContainerID(tt.pod, tt.containerName)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ExtractContainerID() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("ExtractContainerID() unexpected error = %v", err)
				return
			}

			if got != tt.wantID {
				t.Errorf("ExtractContainerID() = %q, want %q", got, tt.wantID)
			}
		})
	}
}

func TestGetSnapshotableContainers(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want []string
	}{
		{
			name: "nil pod",
			pod:  nil,
			want: nil,
		},
		{
			name: "single container",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "main"},
					},
				},
			},
			want: []string{"main"},
		},
		{
			name: "multiple containers",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "first"},
						{Name: "second"},
						{Name: "third"},
					},
				},
			},
			want: []string{"first", "second", "third"},
		},
		{
			name: "init containers excluded",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "init1"},
						{Name: "init2"},
					},
					Containers: []corev1.Container{
						{Name: "main"},
						{Name: "sidecar"},
					},
				},
			},
			want: []string{"main", "sidecar"},
		},
		{
			name: "no containers",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{},
				},
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetSnapshotableContainers(tt.pod)

			if tt.want == nil {
				if got != nil {
					t.Errorf("GetSnapshotableContainers() = %v, want nil", got)
				}
				return
			}

			if len(got) != len(tt.want) {
				t.Errorf("GetSnapshotableContainers() returned %d containers, want %d", len(got), len(tt.want))
				return
			}

			for i, name := range got {
				if name != tt.want[i] {
					t.Errorf("GetSnapshotableContainers()[%d] = %q, want %q", i, name, tt.want[i])
				}
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
