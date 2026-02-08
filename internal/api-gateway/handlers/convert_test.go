package handlers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
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
			Expect(conditionsToStatus(conditions)).To(Equal("unknown"))
		})
	})
})
