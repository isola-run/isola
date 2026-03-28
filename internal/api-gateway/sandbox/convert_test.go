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

		It("returns error for invalid resource limits in full request flow", func() {
			req := CreateSandboxRequest{
				PodTemplate: PodTemplate{
					Container: ContainerSpec{
						Image: "python:3.12",
						Resources: &ResourcesSpec{
							Limits: &ResourceList{CPU: "500m", Memory: "not-valid"},
						},
					},
				},
			}
			_, err := requestToSandboxCR(req, "test-sb", "default")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid resource limits"))
			Expect(err.Error()).To(ContainSubstring("memory"))
		})

		It("returns error for invalid resource requests in full request flow", func() {
			req := CreateSandboxRequest{
				PodTemplate: PodTemplate{
					Container: ContainerSpec{
						Image: "python:3.12",
						Resources: &ResourcesSpec{
							Limits:   &ResourceList{CPU: "1"},
							Requests: &ResourceList{EphemeralStorage: "garbage"},
						},
					},
				},
			}
			_, err := requestToSandboxCR(req, "test-sb", "default")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid resource requests"))
			Expect(err.Error()).To(ContainSubstring("ephemeralStorage"))
		})

		It("converts valid resources through the full request flow", func() {
			req := CreateSandboxRequest{
				PodTemplate: PodTemplate{
					Container: ContainerSpec{
						Image: "python:3.12",
						Resources: &ResourcesSpec{
							Limits:   &ResourceList{CPU: "2", Memory: "4Gi"},
							Requests: &ResourceList{CPU: "500m", Memory: "1Gi"},
						},
					},
				},
			}
			sb, err := requestToSandboxCR(req, "test-sb", "default")
			Expect(err).NotTo(HaveOccurred())
			c := sb.Spec.PodTemplate.Spec.Containers[0]
			Expect(c.Resources.Limits[corev1.ResourceCPU]).To(Equal(resource.MustParse("2")))
			Expect(c.Resources.Limits[corev1.ResourceMemory]).To(Equal(resource.MustParse("4Gi")))
			Expect(c.Resources.Requests[corev1.ResourceCPU]).To(Equal(resource.MustParse("500m")))
			Expect(c.Resources.Requests[corev1.ResourceMemory]).To(Equal(resource.MustParse("1Gi")))
		})

		It("passes rootfsSnapshotSources through to the CRD", func() {
			req := CreateSandboxRequest{
				PodTemplate: PodTemplate{
					Container: ContainerSpec{
						Image: "python:3.12",
					},
				},
				RootfsSnapshotSources: []RootfsSnapshotSource{
					{
						SnapshotName:  "my-snapshot",
						ContainerName: "my-container",
					},
				},
			}
			sb, err := requestToSandboxCR(req, "test-sb", "default")
			Expect(err).NotTo(HaveOccurred())
			Expect(sb.Spec.RootfsSnapshotSources).To(HaveLen(1))
			Expect(sb.Spec.RootfsSnapshotSources[0].SnapshotName).To(Equal("my-snapshot"))
			Expect(sb.Spec.RootfsSnapshotSources[0].ContainerName).To(Equal("my-container"))
		})
	})

	Describe("restRootfsSnapshotSourcesToCRD", func() {
		It("returns nil for nil input", func() {
			Expect(restRootfsSnapshotSourcesToCRD(nil)).To(BeNil())
		})

		It("converts RootfsSnapshotSource list to CRD type", func() {
			input := []RootfsSnapshotSource{
				{
					SnapshotName:  "my-snapshot",
					ContainerName: "my-container",
				},
			}
			result := restRootfsSnapshotSourcesToCRD(input)
			Expect(result).To(HaveLen(1))
			Expect(result[0].SnapshotName).To(Equal("my-snapshot"))
			Expect(result[0].ContainerName).To(Equal("my-container"))
		})
	})

	Describe("crdRootfsSnapshotSourcesToREST", func() {
		It("returns nil for nil input", func() {
			Expect(crdRootfsSnapshotSourcesToREST(nil)).To(BeNil())
		})

		It("converts CRD RootfsSnapshotSource list to REST type", func() {
			input := []sandboxv1alpha1.RootfsSnapshotSource{
				{
					SnapshotName:  "my-snapshot",
					ContainerName: "my-container",
				},
			}
			result := crdRootfsSnapshotSourcesToREST(input)
			Expect(result).To(HaveLen(1))
			Expect(result[0].SnapshotName).To(Equal("my-snapshot"))
			Expect(result[0].ContainerName).To(Equal("my-container"))
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

		It("uses only the first container when multiple containers exist", func() {
			sb := &sandboxv1alpha1.Sandbox{
				Spec: sandboxv1alpha1.SandboxSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "sandbox",
									Image: "python:3.12",
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("2"),
											corev1.ResourceMemory: resource.MustParse("4Gi"),
										},
									},
								},
								{
									Name:  "sidecar",
									Image: "nginx:latest",
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:    resource.MustParse("100m"),
											corev1.ResourceMemory: resource.MustParse("128Mi"),
										},
									},
								},
							},
						},
					},
				},
			}
			resp := sandboxToResponse(sb)
			Expect(resp.PodTemplate.Container.Image).To(Equal("python:3.12"))
			Expect(resp.PodTemplate.Container.Resources).NotTo(BeNil())
			Expect(resp.PodTemplate.Container.Resources.Limits).NotTo(BeNil())
			Expect(resp.PodTemplate.Container.Resources.Limits.CPU).To(Equal("2"))
			Expect(resp.PodTemplate.Container.Resources.Limits.Memory).To(Equal("4Gi"))
		})

		It("returns resources with limits only when requests are empty", func() {
			sb := &sandboxv1alpha1.Sandbox{
				Spec: sandboxv1alpha1.SandboxSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "sandbox",
									Image: "alpine:latest",
									Resources: corev1.ResourceRequirements{
										Limits: corev1.ResourceList{
											corev1.ResourceCPU:              resource.MustParse("1"),
											corev1.ResourceEphemeralStorage: resource.MustParse("5Gi"),
										},
									},
								},
							},
						},
					},
				},
			}
			resp := sandboxToResponse(sb)
			Expect(resp.PodTemplate.Container.Resources).NotTo(BeNil())
			Expect(resp.PodTemplate.Container.Resources.Limits).NotTo(BeNil())
			Expect(resp.PodTemplate.Container.Resources.Limits.CPU).To(Equal("1"))
			Expect(resp.PodTemplate.Container.Resources.Limits.EphemeralStorage).To(Equal("5Gi"))
			Expect(resp.PodTemplate.Container.Resources.Requests).To(BeNil())
		})

		It("includes rootfsSnapshotSources in response", func() {
			sb := &sandboxv1alpha1.Sandbox{
				Spec: sandboxv1alpha1.SandboxSpec{
					PodTemplate: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "sandbox", Image: "alpine:latest"},
							},
						},
					},
					RootfsSnapshotSources: []sandboxv1alpha1.RootfsSnapshotSource{
						{
							SnapshotName:  "my-snapshot",
							ContainerName: "my-container",
						},
					},
				},
			}
			resp := sandboxToResponse(sb)
			Expect(resp.RootfsSnapshotSources).To(HaveLen(1))
			Expect(resp.RootfsSnapshotSources[0].SnapshotName).To(Equal("my-snapshot"))
			Expect(resp.RootfsSnapshotSources[0].ContainerName).To(Equal("my-container"))
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

		It("returns both limits and requests when both are populated", func() {
			r := corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("500m"),
					corev1.ResourceEphemeralStorage: resource.MustParse("10Gi"),
				},
			}
			spec := containerResourcesToSpec(r)
			Expect(spec).NotTo(BeNil())
			Expect(spec.Limits).NotTo(BeNil())
			Expect(spec.Limits.CPU).To(Equal("2"))
			Expect(spec.Limits.Memory).To(Equal("1Gi"))
			Expect(spec.Requests).NotTo(BeNil())
			Expect(spec.Requests.CPU).To(Equal("500m"))
			Expect(spec.Requests.EphemeralStorage).To(Equal("10Gi"))
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
			Entry("only allowIPv6Egress", &sandboxv1alpha1.NetworkSpec{AllowIPv6Egress: ptr.To(true)}, false),
		)
	})

	Describe("resourceListToREST", func() {
		It("returns only recognized resources when mixed with unrecognized types", func() {
			rl := corev1.ResourceList{
				corev1.ResourceCPU:                    resource.MustParse("4"),
				corev1.ResourceMemory:                 resource.MustParse("8Gi"),
				corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
				corev1.ResourceName("hugepages-2Mi"):  resource.MustParse("100Mi"),
			}
			result := resourceListToREST(rl)
			Expect(result).NotTo(BeNil())
			Expect(result.CPU).To(Equal("4"))
			Expect(result.Memory).To(Equal("8Gi"))
			Expect(result.EphemeralStorage).To(BeEmpty())
		})

		It("returns nil when all resources are unrecognized", func() {
			rl := corev1.ResourceList{
				corev1.ResourceName("nvidia.com/gpu"):  resource.MustParse("1"),
				corev1.ResourceName("example.com/foo"): resource.MustParse("5"),
			}
			result := resourceListToREST(rl)
			Expect(result).To(BeNil())
		})
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
