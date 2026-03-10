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

package sandbox

import (
	"io"
	"log/slog"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	apigateway "github.com/isola-ai/isola/internal/api-gateway"
)

var _ = Describe("Conversion functions", func() {

	Describe("requestToSandboxCR", func() {
		It("passes command through to the container", func() {
			req := CreateSandboxRequest{
				PodTemplate: PodTemplate{
					Container: ContainerSpec{
						Image:   "python:3.12",
						Command: []string{"python", "-c", "print('hello')"},
					},
				},
			}
			sb, err := requestToSandboxCR(req, "test-sb", "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(sb.Spec.PodTemplate.Spec.Containers[0].Command).To(Equal([]string{"python", "-c", "print('hello')"}))
		})

		It("leaves command nil when not specified", func() {
			req := CreateSandboxRequest{
				PodTemplate: PodTemplate{
					Container: ContainerSpec{
						Image: "alpine:latest",
					},
				},
			}
			sb, err := requestToSandboxCR(req, "test-sb", "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(sb.Spec.PodTemplate.Spec.Containers[0].Command).To(BeNil())
		})

		It("passes restoreRootfsFrom through to the CRD", func() {
			req := CreateSandboxRequest{
				PodTemplate: PodTemplate{
					Container: ContainerSpec{
						Image: "python:3.12",
					},
				},
				RestoreRootfsFrom: &RootfsRestoreSpec{
					RootfsSnapshotName: "my-snapshot",
					Container:          "my-container",
				},
			}
			sb, err := requestToSandboxCR(req, "test-sb", "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(sb.Spec.RestoreRootfsFrom).NotTo(BeNil())
			Expect(sb.Spec.RestoreRootfsFrom.RootfsSnapshotName).To(Equal("my-snapshot"))
			Expect(sb.Spec.RestoreRootfsFrom.Container).To(Equal("my-container"))
		})
	})

	Describe("restRootfsRestoreToCRD", func() {
		It("returns nil for nil input", func() {
			Expect(restRootfsRestoreToCRD(nil)).To(BeNil())
		})

		It("converts RootfsRestoreSpec to CRD type", func() {
			input := &RootfsRestoreSpec{
				RootfsSnapshotName: "my-snapshot",
				Container:          "my-container",
			}
			result := restRootfsRestoreToCRD(input)
			Expect(result).NotTo(BeNil())
			Expect(result.RootfsSnapshotName).To(Equal("my-snapshot"))
			Expect(result.Container).To(Equal("my-container"))
		})
	})

	Describe("crdRootfsRestoreToREST", func() {
		It("returns nil for nil input", func() {
			Expect(crdRootfsRestoreToREST(nil)).To(BeNil())
		})

		It("converts CRD RootfsRestoreSpec to REST type", func() {
			input := &sandboxv1alpha1.RootfsRestoreSpec{
				RootfsSnapshotName: "my-snapshot",
				Container:          "my-container",
			}
			result := crdRootfsRestoreToREST(input)
			Expect(result).NotTo(BeNil())
			Expect(result.RootfsSnapshotName).To(Equal("my-snapshot"))
			Expect(result.Container).To(Equal("my-container"))
		})
	})

	Describe("sandboxToResponse", func() {
		It("returns command from the CRD container", func() {
			sb := &sandboxv1alpha1.Sandbox{
				Spec: sandboxv1alpha1.SandboxSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:    "sandbox",
									Image:   "python:3.12",
									Command: []string{"python", "-c", "print('hello')"},
								},
							},
						},
					},
				},
			}
			resp := sandboxToResponse(sb)
			Expect(resp.PodTemplate.Container.Command).To(Equal([]string{"python", "-c", "print('hello')"}))
		})

		It("returns nil command when not set on container", func() {
			sb := &sandboxv1alpha1.Sandbox{
				Spec: sandboxv1alpha1.SandboxSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "sandbox",
									Image: "alpine:latest",
								},
							},
						},
					},
				},
			}
			resp := sandboxToResponse(sb)
			Expect(resp.PodTemplate.Container.Command).To(BeNil())
		})

		It("includes restoreRootfsFrom in response", func() {
			sb := &sandboxv1alpha1.Sandbox{
				Spec: sandboxv1alpha1.SandboxSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "sandbox", Image: "alpine:latest"},
							},
						},
					},
					RestoreRootfsFrom: &sandboxv1alpha1.RootfsRestoreSpec{
						RootfsSnapshotName: "my-snapshot",
						Container:          "my-container",
					},
				},
			}
			resp := sandboxToResponse(sb)
			Expect(resp.RestoreRootfsFrom).NotTo(BeNil())
			Expect(resp.RestoreRootfsFrom.RootfsSnapshotName).To(Equal("my-snapshot"))
			Expect(resp.RestoreRootfsFrom.Container).To(Equal("my-container"))
		})
	})

	Describe("containerResourcesToSpec", func() {
		It("returns nil for empty ResourceRequirements", func() {
			Expect(containerResourcesToSpec(corev1.ResourceRequirements{})).To(BeNil())
		})

		It("returns nil when limits contain only unrecognized resource types", func() {
			r := corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
				},
			}
			Expect(containerResourcesToSpec(r)).To(BeNil())
		})
	})

	Describe("crdNetworkToREST", func() {
		DescribeTable("nil-coalescing",
			func(input *sandboxv1alpha1.NetworkSpec, expectNil bool) {
				result := crdNetworkToREST(input)
				if expectNil {
					Expect(result).To(BeNil())
				} else {
					Expect(result).NotTo(BeNil())
				}
			},
			Entry("nil input", nil, true),
			Entry("all-empty struct", &sandboxv1alpha1.NetworkSpec{}, false),
			Entry("only nameservers", &sandboxv1alpha1.NetworkSpec{Nameservers: []string{"8.8.8.8"}}, false),
			Entry("only allowClusterDNS", &sandboxv1alpha1.NetworkSpec{AllowClusterDNS: ptr.To(true)}, false),
			Entry("only allowedEgressCIDRs", &sandboxv1alpha1.NetworkSpec{AllowedEgressCIDRs: []string{"10.0.0.0/8"}}, false),
		)
	})

	Describe("restResourceListToK8s", func() {
		It("returns error mentioning cpu for invalid cpu", func() {
			_, err := restResourceListToK8s(&ResourceList{CPU: "banana"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cpu"))
		})

		It("returns error mentioning memory for invalid memory", func() {
			_, err := restResourceListToK8s(&ResourceList{Memory: "lots"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("memory"))
		})

		It("returns error mentioning ephemeralStorage for invalid ephemeralStorage", func() {
			_, err := restResourceListToK8s(&ResourceList{EphemeralStorage: "nope"})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("ephemeralStorage"))
		})
	})

	Describe("mapToEnvVars", func() {
		It("returns nil for empty map", func() {
			Expect(mapToEnvVars(map[string]string{})).To(BeNil())
		})

		It("returns nil for nil map", func() {
			Expect(mapToEnvVars(nil)).To(BeNil())
		})

		It("sorts keys deterministically", func() {
			m := map[string]string{"Z": "26", "A": "1", "B": "2"}
			result := mapToEnvVars(m)
			Expect(result).To(HaveLen(3))
			Expect(result[0].Name).To(Equal("A"))
			Expect(result[1].Name).To(Equal("B"))
			Expect(result[2].Name).To(Equal("Z"))
		})
	})

	Describe("conditionsToStatus", func() {
		It("maps unrecognized reason to unknown", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "SomethingNew"},
			}
			Expect(apigateway.ConditionsToStatus(conditions)).To(Equal("unknown"))
		})
	})

	Describe("handleSidecarError", func() {
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))

		It("returns 502 for 5xx and reads the body", func() {
			body := `{"status":500,"title":"Internal Server Error","detail":"failed to start command: exec: not found"}`
			resp := &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(body)),
			}
			err := apigateway.HandleSidecarError(resp, "sb-123", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("sidecar internal error"))

			// Body should be fully consumed
			remaining, _ := io.ReadAll(resp.Body)
			Expect(remaining).To(BeEmpty())
		})

		It("forwards 4xx status and detail from sidecar", func() {
			body := `{"status":404,"title":"Not Found","detail":"command \"abc\" not found"}`
			resp := &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader(body)),
			}
			err := apigateway.HandleSidecarError(resp, "sb-123", logger)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(`command "abc" not found`))
		})
	})
})
