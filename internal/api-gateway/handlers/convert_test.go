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

	Describe("envVarsToMap", func() {
		It("filters out ValueFrom entries, keeps plain values", func() {
			envVars := []corev1.EnvVar{
				{Name: "PLAIN", Value: "val"},
				{Name: "FROM_SECRET", ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "s"},
						Key:                  "k",
					},
				}},
				{Name: "ALSO_PLAIN", Value: "val2"},
			}
			m := envVarsToMap(envVars)
			Expect(m).To(Equal(map[string]string{"PLAIN": "val", "ALSO_PLAIN": "val2"}))
		})

		It("returns nil when all entries are ValueFrom", func() {
			envVars := []corev1.EnvVar{
				{Name: "FROM_FIELD", ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				}},
			}
			Expect(envVarsToMap(envVars)).To(BeNil())
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

	Describe("mapToEnvVars + envVarsToMap round-trip", func() {
		It("round-trips plain env vars", func() {
			m := map[string]string{"A": "1", "B": "2", "Z": "26"}
			Expect(envVarsToMap(mapToEnvVars(m))).To(Equal(m))
		})

		It("returns nil for empty map", func() {
			Expect(mapToEnvVars(map[string]string{})).To(BeNil())
		})

		It("returns nil for nil map", func() {
			Expect(mapToEnvVars(nil)).To(BeNil())
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
