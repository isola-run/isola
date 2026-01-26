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

package podutil

import (
	"testing"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "nil pod",
			pod:  nil,
			want: false,
		},
		{
			name: "pending pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			want: false,
		},
		{
			name: "running but not ready",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionFalse},
					},
				},
			},
			want: false,
		},
		{
			name: "running and ready",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "succeeded pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			},
			want: false,
		},
		{
			name: "failed pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(IsPodReady(tt.pod)).To(Equal(tt.want))
		})
	}
}

func TestIsPodTerminated(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want bool
	}{
		{
			name: "nil pod",
			pod:  nil,
			want: false,
		},
		{
			name: "pending pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
				},
			},
			want: false,
		},
		{
			name: "running pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
			want: false,
		},
		{
			name: "succeeded pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodSucceeded,
				},
			},
			want: true,
		},
		{
			name: "failed pod",
			pod: &corev1.Pod{
				Status: corev1.PodStatus{
					Phase: corev1.PodFailed,
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(IsPodTerminated(tt.pod)).To(Equal(tt.want))
		})
	}
}

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
			g := NewWithT(t)
			g.Expect(GetSandboxPodName(tt.sandboxName)).To(Equal(tt.want))
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
			g := NewWithT(t)
			got, err := ExtractContainerID(tt.pod, tt.containerName)

			if tt.wantErr {
				g.Expect(err).To(HaveOccurred())
				return
			}

			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(got).To(Equal(tt.wantID))
		})
	}
}

func TestIsJobComplete(t *testing.T) {
	tests := []struct {
		name string
		job  *batchv1.Job
		want bool
	}{
		{
			name: "nil job",
			job:  nil,
			want: false,
		},
		{
			name: "no conditions",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{},
			},
			want: false,
		},
		{
			name: "complete condition true",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "complete condition false",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobComplete, Status: corev1.ConditionFalse},
					},
				},
			},
			want: false,
		},
		{
			name: "failed condition only",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(IsJobComplete(tt.job)).To(Equal(tt.want))
		})
	}
}

func TestIsJobFailed(t *testing.T) {
	tests := []struct {
		name string
		job  *batchv1.Job
		want bool
	}{
		{
			name: "nil job",
			job:  nil,
			want: false,
		},
		{
			name: "no conditions",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{},
			},
			want: false,
		},
		{
			name: "failed condition true",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
					},
				},
			},
			want: true,
		},
		{
			name: "failed condition false",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobFailed, Status: corev1.ConditionFalse},
					},
				},
			},
			want: false,
		},
		{
			name: "complete condition only",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobComplete, Status: corev1.ConditionTrue},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(IsJobFailed(tt.job)).To(Equal(tt.want))
		})
	}
}

func TestGetJobConditionMessage(t *testing.T) {
	tests := []struct {
		name          string
		job           *batchv1.Job
		conditionType batchv1.JobConditionType
		want          string
	}{
		{
			name:          "nil job",
			job:           nil,
			conditionType: batchv1.JobFailed,
			want:          "",
		},
		{
			name: "no conditions",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{},
			},
			conditionType: batchv1.JobFailed,
			want:          "",
		},
		{
			name: "condition not found",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobComplete, Status: corev1.ConditionTrue, Message: "completed"},
					},
				},
			},
			conditionType: batchv1.JobFailed,
			want:          "",
		},
		{
			name: "condition found but false",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobFailed, Status: corev1.ConditionFalse, Message: "not failed"},
					},
				},
			},
			conditionType: batchv1.JobFailed,
			want:          "",
		},
		{
			name: "condition found and true",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Message: "BackoffLimitExceeded"},
					},
				},
			},
			conditionType: batchv1.JobFailed,
			want:          "BackoffLimitExceeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(GetJobConditionMessage(tt.job, tt.conditionType)).To(Equal(tt.want))
		})
	}
}
