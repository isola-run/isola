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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-run/isola/api/v1alpha1"
	apigateway "github.com/isola-run/isola/internal/api-gateway"
)

var _ = Describe("Sandbox Endpoints", func() {
	Describe("POST /sandboxes", func() {
		It("creates a sandbox with minimal request", func() {
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"python:3.12"}}}`))
			Expect(resp.Code).To(Equal(201))

			var body SandboxResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.ID).To(HaveLen(22))
			Expect(body.ID).To(MatchRegexp(`^[a-z][a-z0-9]{21}$`))
			Expect(body.PodTemplate.Container.Image).To(Equal("python:3.12"))
			Expect(body.Status).To(Equal("unknown"))
			_, err := time.Parse(time.RFC3339, body.CreationTimestamp)
			Expect(err).NotTo(HaveOccurred())

			// No defaults applied — gateway is a pure passthrough
			Expect(body.PodTemplate.Container.Resources).To(BeNil())

			// Omitted fields not in response
			Expect(body.TimeoutSeconds).To(BeNil())
			Expect(body.Network).To(BeNil())
		})

		It("creates a sandbox with all fields", func() {
			reqBody := `{
				"podTemplate": {
					"container": {
						"image": "node:20",
						"env": {"NODE_ENV": "production", "PORT": "3000"},
						"resources": {
							"limits": {"cpu": "2000m", "memory": "1Gi", "ephemeralStorage": "5Gi"},
							"requests": {"cpu": "500m", "memory": "512Mi", "ephemeralStorage": "1Gi"}
						}
					}
				},
				"timeoutSeconds": 600,
				"network": {
					"allowInternetEgress": true,
					"allowClusterDNS": true,
					"allowedEgressCIDRs": ["10.0.0.0/8"],
					"nameservers": ["8.8.8.8"]
				}
			}`

			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(201))

			// Capture raw bytes before decoding (resp.Body is a *bytes.Buffer)
			rawBytes := resp.Body.Bytes()

			var body SandboxResponse
			Expect(json.Unmarshal(rawBytes, &body)).To(Succeed())
			Expect(body.PodTemplate.Container.Image).To(Equal("node:20"))
			Expect(body.PodTemplate.Container.Resources.Limits.CPU).To(Equal("2"))
			Expect(body.PodTemplate.Container.Resources.Limits.Memory).To(Equal("1Gi"))
			Expect(body.PodTemplate.Container.Resources.Limits.EphemeralStorage).To(Equal("5Gi"))
			Expect(body.PodTemplate.Container.Resources.Requests.CPU).To(Equal("500m"))
			Expect(body.PodTemplate.Container.Resources.Requests.Memory).To(Equal("512Mi"))
			Expect(body.PodTemplate.Container.Resources.Requests.EphemeralStorage).To(Equal("1Gi"))
			Expect(*body.TimeoutSeconds).To(Equal(int64(600)))
			Expect(body.Network).NotTo(BeNil())
			Expect(*body.Network.AllowInternetEgress).To(BeTrue())
			Expect(*body.Network.AllowClusterDNS).To(BeTrue())
			Expect(body.Network.AllowedEgressCIDRs).To(ConsistOf("10.0.0.0/8"))
			Expect(body.Network.Nameservers).To(ConsistOf("8.8.8.8"))

			// Env vars are write-only — response must not leak them
			var raw map[string]json.RawMessage
			Expect(json.Unmarshal(rawBytes, &raw)).To(Succeed())
			var podTpl map[string]json.RawMessage
			Expect(json.Unmarshal(raw["podTemplate"], &podTpl)).To(Succeed())
			var container map[string]json.RawMessage
			Expect(json.Unmarshal(podTpl["container"], &container)).To(Succeed())
			Expect(container).NotTo(HaveKey("env"))

			// Also verify via GET read-back (covers sandboxToResponse on the GET path)
			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/v1/sandboxes/%s", body.ID)).Code
			}).Should(Equal(200))
			getResp := testAPI.Get(fmt.Sprintf("/v1/sandboxes/%s", body.ID))
			var getRaw map[string]json.RawMessage
			Expect(json.Unmarshal(getResp.Body.Bytes(), &getRaw)).To(Succeed())
			var getPodTpl map[string]json.RawMessage
			Expect(json.Unmarshal(getRaw["podTemplate"], &getPodTpl)).To(Succeed())
			var getContainer map[string]json.RawMessage
			Expect(json.Unmarshal(getPodTpl["container"], &getContainer)).To(Succeed())
			Expect(getContainer).NotTo(HaveKey("env"))
		})

		It("round-trips command through create and get", func() {
			reqBody := `{"podTemplate":{"container":{"image":"python:3.12","command":["python","-c","print('hello')"]}}}`
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(201))

			var body SandboxResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.PodTemplate.Container.Command).To(Equal([]string{"python", "-c", "print('hello')"}))

			// Verify via GET read-back
			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/v1/sandboxes/%s", body.ID)).Code
			}).Should(Equal(200))
			getResp := testAPI.Get(fmt.Sprintf("/v1/sandboxes/%s", body.ID))
			var got SandboxResponse
			Expect(json.NewDecoder(getResp.Body).Decode(&got)).To(Succeed())
			Expect(got.PodTemplate.Container.Command).To(Equal([]string{"python", "-c", "print('hello')"}))
		})

		It("omits command from response when not specified", func() {
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"alpine:latest"}}}`))
			Expect(resp.Code).To(Equal(201))

			var body SandboxResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.PodTemplate.Container.Command).To(BeNil())

			// Verify via GET read-back
			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/v1/sandboxes/%s", body.ID)).Code
			}).Should(Equal(200))
			getResp := testAPI.Get(fmt.Sprintf("/v1/sandboxes/%s", body.ID))
			var got SandboxResponse
			Expect(json.NewDecoder(getResp.Body).Decode(&got)).To(Succeed())
			Expect(got.PodTemplate.Container.Command).To(BeNil())
		})

		It("rejects missing podTemplate with 422", func() {
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(`{}`))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects invalid resource quantity with 400", func() {
			reqBody := `{"podTemplate":{"container":{"image":"x","resources":{"limits":{"cpu":"banana"}}}}}`
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(400))
		})

		It("rejects empty image with 422", func() {
			reqBody := `{"podTemplate":{"container":{"image":""}}}`
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects more than 3 nameservers with 422", func() {
			reqBody := `{"podTemplate":{"container":{"image":"alpine"}},"network":{"nameservers":["1.1.1.1","8.8.8.8","9.9.9.9","208.67.222.222"]}}`
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(422))
		})

		It("rejects timeoutSeconds of 0", func() {
			reqBody := `{"podTemplate":{"container":{"image":"x"}},"timeoutSeconds":0}`
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(reqBody))
			Expect(resp.Code).To(Equal(422))
		})

		It("accepts omitted timeoutSeconds as no timeout", func() {
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"alpine:latest"}}}`))
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
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"alpine:latest"}}}`))
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
			resp := testAPI.Post("/v1/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"redis:7"}}}`))
			Expect(resp.Code).To(Equal(201))

			var created SandboxResponse
			Expect(json.NewDecoder(resp.Body).Decode(&created)).To(Succeed())

			// GET it back
			Eventually(func() int {
				return testAPI.Get(fmt.Sprintf("/v1/sandboxes/%s", created.ID)).Code
			}).Should(Equal(200))

			getResp := testAPI.Get(fmt.Sprintf("/v1/sandboxes/%s", created.ID))
			var got SandboxResponse
			Expect(json.NewDecoder(getResp.Body).Decode(&got)).To(Succeed())
			Expect(got.ID).To(Equal(created.ID))
			Expect(got.PodTemplate.Container.Image).To(Equal("redis:7"))
		})

		It("returns 404 for nonexistent sandbox", func() {
			resp := testAPI.Get("/v1/sandboxes/nonexistent")
			Expect(resp.Code).To(Equal(404))
		})
	})

	Describe("GET /sandboxes", func() {
		It("returns sandbox summaries", func() {
			// Create a sandbox
			createResp := testAPI.Post("/v1/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"nginx:latest"}}}`))
			Expect(createResp.Code).To(Equal(201))

			var created SandboxResponse
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			// List should include it (eventually, due to cache)
			Eventually(func() bool {
				listResp := testAPI.Get("/v1/sandboxes")
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
		It("deletes a sandbox and returns 204", func() {
			createResp := testAPI.Post("/v1/sandboxes", strings.NewReader(`{"podTemplate":{"container":{"image":"busybox:latest"}}}`))
			Expect(createResp.Code).To(Equal(201))

			var created SandboxResponse
			Expect(json.NewDecoder(createResp.Body).Decode(&created)).To(Succeed())

			delResp := testAPI.Delete(fmt.Sprintf("/v1/sandboxes/%s", created.ID))
			Expect(delResp.Code).To(Equal(204))
		})

		It("returns 204 for nonexistent sandbox (idempotent)", func() {
			resp := testAPI.Delete("/v1/sandboxes/nonexistent")
			Expect(resp.Code).To(Equal(204))
		})
	})

	Describe("Status mapping", func() {
		It("maps Ready=True to running regardless of reason", func() {
			Expect(apigateway.ConditionsToStatus([]metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "PodRunning"},
			})).To(Equal("running"))
			Expect(apigateway.ConditionsToStatus([]metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AnythingElse"},
			})).To(Equal("running"))
		})

		It("maps PodPending to creating", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "PodPending"},
			}
			Expect(apigateway.ConditionsToStatus(conditions)).To(Equal("creating"))
		})

		// Temporary: snapshot-related reasons should be removed from the Sandbox CRD
		// and encapsulated in the RootfsSnapshot CRD only (see convert.go TODO).
		It("maps RootfsSnapshottingInProgress to running", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "RootfsSnapshottingInProgress"},
			}
			Expect(apigateway.ConditionsToStatus(conditions)).To(Equal("running"))
		})

		It("maps NetworkPolicyApplied to creating", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "NetworkPolicyApplied"},
			}
			Expect(apigateway.ConditionsToStatus(conditions)).To(Equal("creating"))
		})

		It("maps Deleting to shuttingDown", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Deleting"},
			}
			Expect(apigateway.ConditionsToStatus(conditions)).To(Equal("shuttingDown"))
		})

		It("maps PodFailed to failed", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "PodFailed"},
			}
			Expect(apigateway.ConditionsToStatus(conditions)).To(Equal("failed"))
		})

		It("maps PodSucceeded to stopped", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "PodSucceeded"},
			}
			Expect(apigateway.ConditionsToStatus(conditions)).To(Equal("stopped"))
		})

		// Temporary: snapshot-related reasons should be removed from the Sandbox CRD
		// and encapsulated in the RootfsSnapshot CRD only (see convert.go TODO).
		It("maps RootfsSnapshotComplete to stopped", func() {
			conditions := []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionFalse, Reason: "RootfsSnapshotComplete"},
			}
			Expect(apigateway.ConditionsToStatus(conditions)).To(Equal("stopped"))
		})

		It("maps no conditions to unknown", func() {
			Expect(apigateway.ConditionsToStatus(nil)).To(Equal("unknown"))
		})
	})
})

func keyFor(id string) client.ObjectKey {
	return client.ObjectKey{Name: id, Namespace: testNamespace}
}
