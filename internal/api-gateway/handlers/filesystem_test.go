package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	sandboxv1alpha1 "github.com/isola-ai/isola-sb/api/v1alpha1"
	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

// createSandboxCR creates a Sandbox CR and waits for it to appear in the cache.
func createSandboxCR() string {
	name, err := generateSandboxName()
	Expect(err).NotTo(HaveOccurred())

	sb := &sandboxv1alpha1.Sandbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: sandboxv1alpha1.SandboxSpec{
			PodTemplate: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "sandbox", Image: "alpine:latest"},
					},
				},
			},
		},
	}
	Expect(k8sClient.Create(ctx, sb)).To(Succeed())

	Eventually(func() error {
		return k8sClient.Get(ctx, client.ObjectKeyFromObject(sb), &sandboxv1alpha1.Sandbox{})
	}).Should(Succeed())

	return name
}

// createRunningSandboxCR creates a Sandbox CR with Ready=True status and PodIP=127.0.0.1,
// simulating a sandbox that the operator has fully reconciled.
func createRunningSandboxCR() string {
	name := createSandboxCR()
	podIP := "127.0.0.1"

	sb := &sandboxv1alpha1.Sandbox{}
	Expect(k8sClient.Get(ctx, client.ObjectKey{Name: name, Namespace: testNamespace}, sb)).To(Succeed())

	sb.Status.PodIP = podIP
	sb.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "PodRunning",
			LastTransitionTime: metav1.Now(),
		},
	}
	Expect(k8sClient.Status().Update(ctx, sb)).To(Succeed())

	Eventually(func() string {
		got := &sandboxv1alpha1.Sandbox{}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(sb), got); err != nil {
			return ""
		}
		return got.Status.PodIP
	}).Should(Equal(podIP))

	return name
}

// newFilesystemTestAPI creates a test API wired to the real envtest k8s client
// and a custom HTTP client / sidecar port.
func newFilesystemTestAPI(httpClient HTTPDoer, sidecarPort int) humatest.TestAPI {
	_, api := humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "1.0.0"))
	h := NewFilesystemHandlers(
		slog.New(slog.NewTextHandler(GinkgoWriter, nil)),
		testNamespace,
		k8sClient,
		httpClient,
	)
	h.sidecarPort = sidecarPort
	RegisterFilesystemRoutes(api, h)
	return api
}

var _ = Describe("Filesystem Proxy", func() {
	Describe("POST /sandboxes/{id}/filesystem", func() {
		It("proxies file write to sidecar and returns response", func() {
			var capturedBody []byte
			var capturedPath, capturedContainer, capturedContentType string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Query().Get("path")
				capturedContainer = r.URL.Query().Get("container")
				capturedContentType = r.Header.Get("Content-Type")
				var err error
				capturedBody, err = io.ReadAll(r.Body)
				Expect(err).NotTo(HaveOccurred())

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(sidecarapi.FilesystemWriteResponse{
					AbsolutePath: "/workspace/hello.txt",
					BytesWritten: int64(len(capturedBody)),
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)

			// Use 127.0.0.1 as PodIP since mock sidecar is local
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/filesystem?path=/workspace/hello.txt&container=main", sbName),
				"Content-Type: application/octet-stream",
				strings.NewReader("file content here"),
			)
			Expect(resp.Code).To(Equal(http.StatusCreated))

			var body sidecarapi.FilesystemWriteResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.AbsolutePath).To(Equal("/workspace/hello.txt"))
			Expect(body.BytesWritten).To(Equal(int64(len("file content here"))))

			// Verify query params were forwarded
			Expect(capturedPath).To(Equal("/workspace/hello.txt"))
			Expect(capturedContainer).To(Equal("main"))

			// Verify body was streamed correctly
			Expect(string(capturedBody)).To(Equal("file content here"))

			// Verify Content-Type was forwarded to sidecar
			Expect(capturedContentType).To(Equal("application/octet-stream"))
		})

		It("omits container query param when not specified", func() {
			var capturedContainer string
			var hasContainer bool
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedContainer = r.URL.Query().Get("container")
				hasContainer = r.URL.Query().Has("container")

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(sidecarapi.FilesystemWriteResponse{
					AbsolutePath: "/tmp/test.txt",
					BytesWritten: 4,
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusCreated))
			Expect(hasContainer).To(BeFalse())
			Expect(capturedContainer).To(BeEmpty())
		})

		It("returns 404 for nonexistent sandbox", func() {
			api := newFilesystemTestAPI(&http.Client{}, 0)

			resp := api.Post(
				"/sandboxes/nonexistent/filesystem?path=/tmp/test.txt",
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 409 when sandbox has no PodIP", func() {
			api := newFilesystemTestAPI(&http.Client{}, 0)
			sbName := createSandboxCR() // no PodIP or Ready condition

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("returns 502 when sidecar is unreachable", func() {
			// Start and immediately close a server to get an assigned port that's no longer listening
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			mockSidecar.Close()

			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("forwards sidecar 400 errors", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"detail": "path contains invalid characters",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 502 for sidecar 500 errors", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})
	})
})
