package handlers

import (
	"encoding/json"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
)

var _ = Describe("Sandbox Endpoints", func() {
	Describe("POST /sandboxes", func() {
		It("creates a sandbox with minimal request", func() {
			resp := testAPI.Post("/sandboxes", strings.NewReader(`{"image":"python:3.12"}`))
			Expect(resp.Code).To(Equal(201))

			var body SandboxResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.ID).NotTo(BeEmpty())
			Expect(body.Image).To(Equal("python:3.12"))
			Expect(body.Status).To(Equal("unknown"))
			Expect(body.CreationTimestamp).NotTo(BeEmpty())

			// Defaults applied
			Expect(body.Resources).NotTo(BeNil())
			Expect(body.Resources.Limits).NotTo(BeNil())
			Expect(body.Resources.Limits.CPU).To(Equal("125m"))
			Expect(body.Resources.Limits.Memory).To(Equal("512Mi"))
			Expect(body.Resources.Limits.EphemeralStorage).To(Equal("1Gi"))
			// Requests default to limits
			Expect(body.Resources.Requests).NotTo(BeNil())
			Expect(body.Resources.Requests.CPU).To(Equal("125m"))
			Expect(body.Resources.Requests.Memory).To(Equal("512Mi"))
			Expect(body.Resources.Requests.EphemeralStorage).To(Equal("1Gi"))

			// Omitted fields not in response
			Expect(body.Env).To(BeNil())
			Expect(body.TimeoutSeconds).To(BeNil())
			Expect(body.Network).To(BeNil())
		})

		It("creates a sandbox with all fields", func() {
			reqBody := `{
				"image": "node:20",
				"env": {"NODE_ENV": "production", "PORT": "3000"},
				"resources": {
					"limits": {"cpu": "2000m", "memory": "1Gi", "ephemeralStorage": "5Gi"},
					"requests": {"cpu": "500m", "memory": "512Mi", "ephemeralStorage": "1Gi"}
				},
				"timeoutSeconds": 600,
				"network": {
					"allowAllInternet": true,
					"allowedEgressCIDRs": ["10.0.0.0/8"]
				}
			}`

			resp := testAPI.Post("/sandboxes", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(201))

			var body SandboxResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Image).To(Equal("node:20"))
			Expect(body.Env).To(HaveKeyWithValue("NODE_ENV", "production"))
			Expect(body.Env).To(HaveKeyWithValue("PORT", "3000"))
			Expect(body.Resources.Limits.CPU).To(Equal("2"))
			Expect(body.Resources.Limits.Memory).To(Equal("1Gi"))
			Expect(body.Resources.Requests.CPU).To(Equal("500m"))
			Expect(body.Resources.Requests.Memory).To(Equal("512Mi"))
			Expect(*body.TimeoutSeconds).To(Equal(int64(600)))
			Expect(body.Network).NotTo(BeNil())
			Expect(*body.Network.AllowAllInternet).To(BeTrue())
			Expect(body.Network.AllowedEgressCIDRs).To(ConsistOf("10.0.0.0/8"))
		})

		It("rejects missing image with 400", func() {
			resp := testAPI.Post("/sandboxes", strings.NewReader(`{}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects invalid resource quantity with 400", func() {
			reqBody := `{"image": "x", "resources": {"limits": {"cpu": "banana"}}}`
			resp := testAPI.Post("/sandboxes", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(400))
		})

		It("rejects timeoutSeconds of 0", func() {
			reqBody := `{"image": "x", "timeoutSeconds": 0}`
			resp := testAPI.Post("/sandboxes", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(422))
		})

		It("accepts omitted timeoutSeconds as no timeout", func() {
			resp := testAPI.Post("/sandboxes", strings.NewReader(`{"image":"alpine:latest"}`))
			Expect(resp.Code).To(Equal(201))

			var body SandboxResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.TimeoutSeconds).To(BeNil())

			// Verify the CR also has nil timeoutSeconds (wait for cache sync)
			sb := &sandboxv1alpha1.Sandbox{}
			Eventually(func() error {
				return k8sClient.Get(ctx, keyFor(body.ID), sb)
			}).Should(Succeed())
			Expect(sb.Spec.TimeoutSeconds).To(BeNil())
		})

		It("omits network from response when not specified", func() {
			resp := testAPI.Post("/sandboxes", strings.NewReader(`{"image":"alpine:latest"}`))
			Expect(resp.Code).To(Equal(201))

			var body SandboxResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Network).To(BeNil())

			sb := &sandboxv1alpha1.Sandbox{}
			Eventually(func() error {
				return k8sClient.Get(ctx, keyFor(body.ID), sb)
			}).Should(Succeed())
			Expect(sb.Spec.Network).To(BeNil())
		})
	})

	Describe("GET /sandboxes/{id}", func() {
		It("returns sandbox details", func() {
			// Create a sandbox first
			resp := testAPI.Post("/sandboxes", strings.NewReader(`{"image":"redis:7"}`))
			Expect(resp.Code).To(Equal(201))

			var created SandboxResponse
			Expect(json.NewDecoder(resp.Body).Decode(&created)).To(Succeed())

			// GET it back
			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/sandboxes/%s", created.ID)).Code
			}).Should(Equal(200))

			getResp := testAPI.Get(fmt.Sprintf("/sandboxes/%s", created.ID))
			var got SandboxResponse
			Expect(json.NewDecoder(getResp.Body).Decode(&got)).To(Succeed())
			Expect(got.ID).To(Equal(created.ID))
			Expect(got.Image).To(Equal("redis:7"))
			Expect(got.Resources).NotTo(BeNil())
		})

		It("returns 404 for nonexistent sandbox", func() {
			resp := testAPI.Get("/sandboxes/nonexistent")
			Expect(resp.Code).To(Equal(404))
		})
	})

	Describe("GET /sandboxes", func() {
		It("returns sandbox summaries", func() {
			// Create a sandbox
			createResp := testAPI.Post("/sandboxes", strings.NewReader(`{"image":"nginx:latest"}`))
			Expect(createResp.Code).To(Equal(201))

			var created SandboxResponse
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			// List should include it (eventually, due to cache)
			Eventually(func() bool {
				listResp := testAPI.Get("/sandboxes")
				if listResp.Code != 200 {
					return false
				}
				var list ListSandboxesResponse
				if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
					return false
				}
				for _, s := range list.Sandboxes {
					if s.ID == created.ID {
						return true
					}
				}
				return false
			}).Should(BeTrue())
		})
	})

	Describe("DELETE /sandboxes/{id}", func() {
		It("deletes a sandbox and returns shuttingDown", func() {
			// Create first
			createResp := testAPI.Post("/sandboxes", strings.NewReader(`{"image":"busybox:latest"}`))
			Expect(createResp.Code).To(Equal(201))

			var created SandboxResponse
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			// Wait for cache to sync, then delete
			Eventually(func() int {
				return testAPI.Delete(fmt.Sprintf("/sandboxes/%s", created.ID)).Code
			}).Should(Equal(200))

			// Re-create to verify response shape (previous delete consumed the sandbox)
			createResp = testAPI.Post("/sandboxes", strings.NewReader(`{"image":"busybox:latest"}`))
			Expect(createResp.Code).To(Equal(201))
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/sandboxes/%s", created.ID)).Code
			}).Should(Equal(200))

			delResp := testAPI.Delete(fmt.Sprintf("/sandboxes/%s", created.ID))
			Expect(delResp.Code).To(Equal(200))

			var deleted DeleteSandboxResponse
			Expect(json.NewDecoder(delResp.Body).Decode(&deleted)).To(Succeed())
			Expect(deleted.ID).To(Equal(created.ID))
			Expect(deleted.Status).To(Equal("shuttingDown"))
		})

		It("returns 404 for nonexistent sandbox", func() {
			resp := testAPI.Delete("/sandboxes/nonexistent")
			Expect(resp.Code).To(Equal(404))
		})
	})

	Describe("Status mapping", func() {
		It("maps Ready=True to running", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "PodRunning"},
			}
			Expect(conditionsToStatus(conditions)).To(Equal("running"))
		})

		It("maps PodPending to creating", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "PodPending"},
			}
			Expect(conditionsToStatus(conditions)).To(Equal("creating"))
		})

		It("maps RootfsSnapshottingInProgress to running", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "RootfsSnapshottingInProgress"},
			}
			Expect(conditionsToStatus(conditions)).To(Equal("running"))
		})

		It("maps NetworkPolicyApplied to creating", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "NetworkPolicyApplied"},
			}
			Expect(conditionsToStatus(conditions)).To(Equal("creating"))
		})

		It("maps Deleting to shuttingDown", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Deleting"},
			}
			Expect(conditionsToStatus(conditions)).To(Equal("shuttingDown"))
		})

		It("maps PodFailed to failed", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "PodFailed"},
			}
			Expect(conditionsToStatus(conditions)).To(Equal("failed"))
		})

		It("maps PodSucceeded to stopped", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "PodSucceeded"},
			}
			Expect(conditionsToStatus(conditions)).To(Equal("stopped"))
		})

		It("maps no conditions to unknown", func() {
			Expect(conditionsToStatus(nil)).To(Equal("unknown"))
		})
	})
})

func keyFor(id string) client.ObjectKey {
	return client.ObjectKey{Name: id, Namespace: testNamespace}
}
