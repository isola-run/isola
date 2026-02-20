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

package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Sandbox Controller", func() {

	// ============================================
	// Network Configuration Tests
	// ============================================
	Context("Network Configuration", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should create custom NetworkPolicy when allowedEgressCIDRs is specified", func() {
			sandboxName := "sandbox-netpol-cidr"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicy(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeEgress))

			// Verify CIDR rule exists
			Expect(np.Spec.Egress).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.0/24"))

			// Verify pod selector uses sandbox instance
			Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app.kubernetes.io/instance", sandboxName))

			sandbox := getSandbox(ctx, sandboxName)
			Expect(hasConditionWithReason(sandbox, SandboxNetworkReadyCondition, metav1.ConditionTrue, CondReasonNetworkPolicyApplied)).To(BeTrue())
		})

		It("should not create custom NetworkPolicy when network spec is nil", func() {
			sandboxName := "sandbox-no-netpol"

			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())
		})

		It("should not create custom NetworkPolicy when only allowInternetEgress is set", func() {
			// Internet access is handled by Helm-installed NetworkPolicy, no custom policy needed
			sandboxName := "sandbox-internet-only"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowInternetEgress: ptr.To(true),
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())
		})

		It("should create custom NetworkPolicy when nameservers specified without internet access", func() {
			sandboxName := "sandbox-dns-allowed"

			network := &sandboxv1alpha1.NetworkSpec{
				Nameservers: []string{"8.8.8.8"},
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicy(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify DNS egress rule exists
			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.Egress).To(HaveLen(1))
			// Verify it targets the DNS server IP as /32 CIDR
			Expect(np.Spec.Egress[0].To).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock).NotTo(BeNil())
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.8/32"))
			// Verify port 53 UDP and TCP
			Expect(np.Spec.Egress[0].Ports).To(HaveLen(2))
		})

		It("should block risky CIDRs (169.254.0.0/16) when egress allows 0.0.0.0/0", func() {
			sandboxName := "sandbox-block-metadata"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"0.0.0.0/0"},
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicy(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify NetworkPolicy has 169.254.0.0/16 in except
			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.Egress).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("0.0.0.0/0"))
			Expect(np.Spec.Egress[0].To[0].IPBlock.Except).To(ContainElement("169.254.0.0/16"))
		})

		It("should not add except for public CIDRs that don't overlap blocked ranges", func() {
			sandboxName := "sandbox-public-range"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicy(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
			Expect(np.Spec.Egress).To(HaveLen(1))
			Expect(np.Spec.Egress[0].To[0].IPBlock.CIDR).To(Equal("8.8.8.0/24"))
			Expect(np.Spec.Egress[0].To[0].IPBlock.Except).To(BeEmpty())
		})

		It("should recreate custom NetworkPolicy if deleted on next reconcile", func() {
			sandboxName := "sandbox-np-recreate"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"8.8.8.0/24"},
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicy(ctx, sandboxName+"-custom-netpol")

			// Initial reconcile - creates Pod and NetworkPolicy
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify NetworkPolicy exists
			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())

			// Delete the NetworkPolicy externally
			Expect(k8sClient.Delete(ctx, np)).To(Succeed())

			// Verify it's gone
			np = getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())

			// Reconcile again - should recreate NetworkPolicy
			_, err = doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// Verify NetworkPolicy is recreated
			np = getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).NotTo(BeNil())
		})

		It("should add network labels to pod for allowInternetEgress", func() {
			sandboxName := "sandbox-internet-labels"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowInternetEgress: ptr.To(true),
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Labels).To(HaveKeyWithValue(LabelAllowInternet, "true"))
		})

		It("should add network labels to pod for allowClusterDNS", func() {
			sandboxName := "sandbox-dns-labels"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowClusterDNS: ptr.To(true),
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Labels).To(HaveKeyWithValue(LabelAllowClusterDNS, "true"))
		})

		It("should set DNSPolicy to ClusterFirst when allowClusterDNS is true", func() {
			sandboxName := "sandbox-dns-cluster"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowClusterDNS: ptr.To(true),
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
		})

		It("should set DNSPolicy to None with sink DNS when no network config", func() {
			sandboxName := "sandbox-dns-sink"

			// No network config - should use sink DNS
			createSandbox(ctx, sandboxName)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
			Expect(pod.Spec.DNSConfig).NotTo(BeNil())
			Expect(pod.Spec.DNSConfig.Nameservers).To(ContainElement("127.0.0.1"))
		})

		It("should set custom nameservers when specified", func() {
			sandboxName := "sandbox-dns-custom"

			network := &sandboxv1alpha1.NetworkSpec{
				Nameservers: []string{"1.1.1.1", "8.8.8.8"},
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")
			defer deleteNetworkPolicy(ctx, sandboxName+"-custom-netpol")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
			Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"1.1.1.1", "8.8.8.8"}))
		})

		// Note: Invalid CIDR format is rejected by CRD validation, so we can't test that path here.
		// The CRD regex validates CIDR format at creation time.

		It("should set NetworkConfigured condition to false with blocked CIDR", func() {
			sandboxName := "sandbox-blocked-cidr"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowedEgressCIDRs: []string{"10.0.0.0/8"}, // Private range - blocked
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			// Reconcile returns error for blocked CIDR, but status is updated first
			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("blocked range"))

			sandbox := getSandbox(ctx, sandboxName)
			Expect(sandbox).NotTo(BeNil())

			// NetworkConfigured condition should be False with error message
			cond := meta.FindStatusCondition(sandbox.Status.Conditions, SandboxNetworkReadyCondition)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(CondReasonNetworkPolicyFailed))
			Expect(cond.Message).To(ContainSubstring("blocked range"))
		})
	})

	// Combined network configuration tests - testing various network specs together
	Context("Combined Network Configuration", func() {
		var (
			reconciler *SandboxReconciler
			fakeClock  *FakeClock
		)

		BeforeEach(func() {
			fakeClock = NewFakeClock(time.Now())
			reconciler = newTestReconciler(fakeClock)
		})

		It("should not create custom NetworkPolicy for public nameservers with internet access", func() {
			// Public nameservers (8.8.8.8) already reachable via static allow-internet-egress policy
			sandboxName := "sandbox-internet-dns"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowInternetEgress: ptr.To(true),
				Nameservers:         []string{"8.8.8.8"},
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// No custom NetworkPolicy — public NS already covered by static policy
			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())

			// Verify pod has correct labels and DNS config
			pod := getPod(ctx, sandboxName+"-pod")
			Expect(pod).NotTo(BeNil())
			Expect(pod.Labels).To(HaveKeyWithValue(LabelAllowInternet, "true"))
			Expect(pod.Spec.DNSConfig.Nameservers).To(ContainElement("8.8.8.8"))
		})

		It("should not create custom NetworkPolicy for CIDRs with internet access", func() {
			sandboxName := "sandbox-internet-cidrs"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowInternetEgress: ptr.To(true),
				AllowedEgressCIDRs:  []string{"1.1.1.0/24"},
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			// CIDRs already reachable via static internet policy — no custom NP
			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())
		})

		It("should not create custom NetworkPolicy for private nameservers with internet access", func() {
			// All nameservers skipped when internet is allowed — no custom NP
			sandboxName := "sandbox-internet-private-dns"

			network := &sandboxv1alpha1.NetworkSpec{
				AllowInternetEgress: ptr.To(true),
				Nameservers:         []string{"10.0.0.53"},
			}
			createSandboxWithNetwork(ctx, sandboxName, network)
			defer deleteSandbox(ctx, sandboxName)
			defer deletePod(ctx, sandboxName+"-pod")

			_, err := doReconcile(ctx, reconciler, sandboxName)
			Expect(err).NotTo(HaveOccurred())

			np := getNetworkPolicy(ctx, sandboxName+"-custom-netpol")
			Expect(np).To(BeNil())
		})
	})

})

var _ = Describe("configureDNS function", func() {
	It("should configure DNSPolicy None with sink nameserver when network is nil", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		configureDNS(pod, nil)
		Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
		Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"127.0.0.1"}))
	})

	It("should configure DNSPolicy ClusterFirst when allowClusterDNS is true", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		network := &sandboxv1alpha1.NetworkSpec{
			AllowClusterDNS: ptr.To(true),
		}

		configureDNS(pod, network)
		Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
	})

	It("should configure custom nameservers when specified", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		network := &sandboxv1alpha1.NetworkSpec{
			Nameservers: []string{"8.8.8.8", "1.1.1.1"},
		}

		configureDNS(pod, network)
		Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSNone))
		Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"8.8.8.8", "1.1.1.1"}))
	})

	It("should add nameservers to cluster DNS when both are specified", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "test", Image: "busybox"},
				},
			},
		}

		network := &sandboxv1alpha1.NetworkSpec{
			AllowClusterDNS: ptr.To(true),
			Nameservers:     []string{"8.8.8.8"},
		}

		configureDNS(pod, network)
		Expect(pod.Spec.DNSPolicy).To(Equal(corev1.DNSClusterFirst))
		Expect(pod.Spec.DNSConfig.Nameservers).To(Equal([]string{"8.8.8.8"}))
	})
})
