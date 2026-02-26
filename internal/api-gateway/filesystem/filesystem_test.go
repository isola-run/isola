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

package filesystem

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

	sandboxv1alpha1 "github.com/isola-ai/isola/api/v1alpha1"
	apigateway "github.com/isola-ai/isola/internal/api-gateway"
	sidecarapi "github.com/isola-ai/isola/internal/sidecar-api"
)

func createSandboxCR() string {
	name := generateName()

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

var nameCounter int

func generateName() string {
	nameCounter++
	return fmt.Sprintf("testfs%015d", nameCounter)
}

// errReader is an io.Reader whose Read always fails.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// brokenBodyDoer is an HTTPDoer that returns a response with an unreadable body.
type brokenBodyDoer struct{ statusCode int }

func (d *brokenBodyDoer) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: d.statusCode,
		Body:       io.NopCloser(errReader{}),
	}, nil
}

func newFilesystemTestAPI(httpClient apigateway.HTTPDoer, sidecarPort int) humatest.TestAPI {
	_, api := humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "1.0.0"))
	h := New(
		slog.New(slog.NewTextHandler(GinkgoWriter, nil)),
		testNamespace,
		k8sClient,
		httpClient,
	)
	h.sidecarPort = sidecarPort
	Register(api, h)
	return api
}

var _ = Describe("Filesystem Proxy", func() {
	Describe("GET /sandboxes/{id}/filesystem", func() {
		It("proxies file read and returns body", func() {
			fileContent := []byte{0xDE, 0xAD, 0xBE, 0xEF}
			var capturedPath, capturedContainer string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Query().Get("path")
				capturedContainer = r.URL.Query().Get("container")

				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(fileContent)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(fileContent)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/filesystem?path=/workspace/hello.bin&container=main", sbName))

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(fileContent))
			Expect(capturedPath).To(Equal("/workspace/hello.bin"))
			Expect(capturedContainer).To(Equal("main"))
		})

		It("forwards Content-Type and Content-Length headers", func() {
			content := []byte("some file content")
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(content)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName))

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Header().Get("Content-Type")).To(Equal("application/octet-stream"))
			Expect(resp.Header().Get("Content-Length")).To(Equal(fmt.Sprintf("%d", len(content))))
		})

		It("omits container param when not specified", func() {
			var hasContainer bool
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hasContainer = r.URL.Query().Has("container")
				w.WriteHeader(http.StatusOK)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName))

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(hasContainer).To(BeFalse())
		})

		It("returns 404 for nonexistent sandbox", func() {
			api := newFilesystemTestAPI(&http.Client{}, 0)

			resp := api.Get("/sandboxes/nonexistent/filesystem?path=/tmp/test.txt")

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 409 for not-running sandbox", func() {
			api := newFilesystemTestAPI(&http.Client{}, 0)
			sbName := createSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName))

			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("returns 502 when sidecar unreachable", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			mockSidecar.Close()

			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName))

			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("forwards sidecar 404 errors", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"detail": "file not found: /tmp/missing.txt",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/missing.txt", sbName))

			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 502 for sidecar 500 errors", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/test.txt", sbName))

			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})
	})

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
				w.WriteHeader(http.StatusCreated)
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

			var body FilesystemWriteResponse
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
				w.WriteHeader(http.StatusCreated)
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

		It("returns 502 when sidecar returns invalid JSON on success", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("not json"))
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

		It("falls back to status text when sidecar error body is unreadable", func() {
			api := newFilesystemTestAPI(&brokenBodyDoer{statusCode: http.StatusBadRequest}, 0)
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

	Describe("GET /sandboxes/{id}/filesystem/list", func() {
		It("proxies list request and returns entries", func() {
			var capturedPath string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Query().Get("path")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(sidecarapi.FilesystemListResponse{
					Entries: []sidecarapi.FileInfo{
						{Name: "a.txt", Path: "/workspace/a.txt", IsDir: false, Size: 10, Mode: "-rw-r--r--"},
						{Name: "subdir", Path: "/workspace/subdir", IsDir: true, Size: 0, Mode: "drwxr-xr-x"},
					},
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/filesystem/list?path=/workspace", sbName))

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(capturedPath).To(Equal("/workspace"))

			var body FilesystemListResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Entries).To(HaveLen(2))
			Expect(body.Entries[0].Name).To(Equal("a.txt"))
			Expect(body.Entries[1].IsDir).To(BeTrue())
		})

		It("returns 404 for nonexistent sandbox", func() {
			api := newFilesystemTestAPI(&http.Client{}, 0)
			resp := api.Get("/sandboxes/nonexistent/filesystem/list?path=/tmp")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GET /sandboxes/{id}/filesystem/stat", func() {
		It("proxies stat request and returns file info", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(sidecarapi.FileInfo{
					Name: "test.txt", Path: "/tmp/test.txt", IsDir: false, Size: 42, Mode: "-rw-r--r--",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/filesystem/stat?path=/tmp/test.txt", sbName))

			Expect(resp.Code).To(Equal(http.StatusOK))
			var body FileInfo
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.Name).To(Equal("test.txt"))
			Expect(body.Size).To(Equal(int64(42)))
		})
	})

	Describe("POST /sandboxes/{id}/filesystem/mkdir", func() {
		It("proxies mkdir request and returns created path", func() {
			var capturedPath string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Query().Get("path")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(sidecarapi.FilesystemMkdirResponse{
					AbsolutePath: "/workspace/newdir",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(fmt.Sprintf("/sandboxes/%s/filesystem/mkdir?path=/workspace/newdir", sbName), "", nil)

			Expect(resp.Code).To(Equal(http.StatusCreated))
			Expect(capturedPath).To(Equal("/workspace/newdir"))

			var body FilesystemMkdirResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.AbsolutePath).To(Equal("/workspace/newdir"))
		})
	})

	Describe("POST /sandboxes/{id}/filesystem/rename", func() {
		It("proxies rename request with both paths", func() {
			var capturedPath, capturedNewPath string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Query().Get("path")
				capturedNewPath = r.URL.Query().Get("newPath")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(sidecarapi.FilesystemRenameResponse{
					AbsolutePath: "/workspace/new-name.txt",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(fmt.Sprintf("/sandboxes/%s/filesystem/rename?path=/workspace/old.txt&newPath=/workspace/new-name.txt", sbName), "", nil)

			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(capturedPath).To(Equal("/workspace/old.txt"))
			Expect(capturedNewPath).To(Equal("/workspace/new-name.txt"))
		})
	})

	Describe("DELETE /sandboxes/{id}/filesystem", func() {
		It("proxies remove request", func() {
			var capturedPath string
			var capturedMethod string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Query().Get("path")
				capturedMethod = r.Method
				w.WriteHeader(http.StatusNoContent)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Delete(fmt.Sprintf("/sandboxes/%s/filesystem?path=/workspace/remove-me.txt", sbName))

			Expect(resp.Code).To(Equal(http.StatusNoContent))
			Expect(capturedPath).To(Equal("/workspace/remove-me.txt"))
			Expect(capturedMethod).To(Equal(http.MethodDelete))
		})

		It("returns 404 for nonexistent sandbox", func() {
			api := newFilesystemTestAPI(&http.Client{}, 0)
			resp := api.Delete("/sandboxes/nonexistent/filesystem?path=/tmp/test.txt")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("forwards sidecar 404 errors", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"detail": "path not found: /tmp/missing.txt",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newFilesystemTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Delete(fmt.Sprintf("/sandboxes/%s/filesystem?path=/tmp/missing.txt", sbName))
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})
})
