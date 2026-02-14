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
)

func newCommandTestAPI(httpClient HTTPDoer, sidecarPort int) humatest.TestAPI {
	_, api := humatest.New(GinkgoT(), huma.DefaultConfig("Test API", "1.0.0"))
	h := NewCommandHandlers(
		slog.New(slog.NewTextHandler(GinkgoWriter, nil)),
		testNamespace,
		k8sClient,
		httpClient,
	)
	h.sidecarPort = sidecarPort
	RegisterCommandRoutes(api, h)
	return api
}

var _ = Describe("Command Proxy", func() {
	Describe("POST /sandboxes/{id}/commands", func() {
		It("proxies to sidecar and returns 202", func() {
			var capturedBody []byte
			var capturedContentType string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedContentType = r.Header.Get("Content-Type")
				var err error
				capturedBody, err = io.ReadAll(r.Body)
				Expect(err).NotTo(HaveOccurred())

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(CreateCommandResponse{
					CommandID: "test-cmd-id",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands", sbName),
				"Content-Type: application/json",
				strings.NewReader(`{"cmd":"echo","args":["hello"]}`),
			)
			Expect(resp.Code).To(Equal(http.StatusAccepted))

			var body CreateCommandResponse
			Expect(json.NewDecoder(resp.Body).Decode(&body)).To(Succeed())
			Expect(body.CommandID).To(Equal("test-cmd-id"))

			Expect(capturedContentType).To(Equal("application/json"))

			var capturedReq CreateCommandRequest
			Expect(json.Unmarshal(capturedBody, &capturedReq)).To(Succeed())
			Expect(capturedReq.Cmd).To(Equal("echo"))
			Expect(capturedReq.Args).To(Equal([]string{"hello"}))
		})

		It("forwards container query param", func() {
			var capturedContainer string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedContainer = r.URL.Query().Get("container")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(CreateCommandResponse{CommandID: "id"})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands?container=main", sbName),
				"Content-Type: application/json",
				strings.NewReader(`{"cmd":"echo"}`),
			)
			Expect(resp.Code).To(Equal(http.StatusAccepted))
			Expect(capturedContainer).To(Equal("main"))
		})

		It("returns 404 for nonexistent sandbox", func() {
			api := newCommandTestAPI(&http.Client{}, 0)
			resp := api.Post("/sandboxes/nonexistent/commands", "Content-Type: application/json",
				strings.NewReader(`{"cmd":"echo"}`))
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 409 for not-running sandbox", func() {
			api := newCommandTestAPI(&http.Client{}, 0)
			sbName := createSandboxCR()
			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands", sbName),
				"Content-Type: application/json",
				strings.NewReader(`{"cmd":"echo"}`),
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("returns 502 when sidecar unreachable", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			mockSidecar.Close()

			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands", sbName),
				"Content-Type: application/json",
				strings.NewReader(`{"cmd":"echo"}`),
			)
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})
	})

	Describe("GET /sandboxes/{id}/commands/{cmdId}/status", func() {
		It("proxies status response", func() {
			exitCode := 0
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(CommandStatusResponse{ExitCode: &exitCode})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/status", sbName))
			Expect(resp.Code).To(Equal(http.StatusOK))

			var status CommandStatusResponse
			Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
			Expect(status.ExitCode).To(HaveValue(Equal(0)))
		})

		It("forwards sidecar 404", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"detail": "command not found"})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/00000000-0000-0000-0000-000000000000/status", sbName))
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GET /sandboxes/{id}/commands/{cmdId}/stdout", func() {
		It("proxies chunked byte stream", func() {
			content := []byte("hello from command")
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("X-Accel-Buffering", "no")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(content)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/stdout", sbName))
			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
			Expect(resp.Header().Get("Content-Type")).To(Equal("application/octet-stream"))
			Expect(resp.Header().Get("X-Accel-Buffering")).To(Equal("no"))
		})

		It("forwards offset query param to sidecar", func() {
			var capturedOffset string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedOffset = r.URL.Query().Get("offset")
				w.Header().Set("Content-Type", "application/octet-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("partial"))
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/stdout?offset=42", sbName))
			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(capturedOffset).To(Equal("42"))
		})
	})

	Describe("POST /sandboxes/{id}/commands/{cmdId}/stdin", func() {
		It("proxies raw bytes to sidecar", func() {
			var capturedBody []byte
			var capturedContentType string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedContentType = r.Header.Get("Content-Type")
				var err error
				capturedBody, err = io.ReadAll(r.Body)
				Expect(err).NotTo(HaveOccurred())
				w.WriteHeader(http.StatusNoContent)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/stdin", sbName),
				"Content-Type: application/octet-stream",
				strings.NewReader("input data"),
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))
			Expect(string(capturedBody)).To(Equal("input data"))
			Expect(capturedContentType).To(Equal("application/octet-stream"))
		})
	})

	Describe("DELETE /sandboxes/{id}/commands/{cmdId}", func() {
		It("proxies kill request", func() {
			var capturedMethod string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedMethod = r.Method
				w.WriteHeader(http.StatusNoContent)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Delete(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", sbName))
			Expect(resp.Code).To(Equal(http.StatusNoContent))
			Expect(capturedMethod).To(Equal(http.MethodDelete))
		})

		It("returns 502 when sidecar is unreachable", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			mockSidecar.Close()

			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Delete(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", sbName))
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("returns 502 for sidecar 500 errors", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Delete(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", sbName))
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("forwards sidecar 404", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{"detail": "command not found"})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Delete(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", sbName))
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GET /sandboxes/{id}/commands/{cmdId}/stderr", func() {
		It("proxies stderr byte stream", func() {
			content := []byte("error output here")
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(ContainSubstring("/stderr"))
				w.Header().Set("Content-Type", "application/octet-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(content)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/stderr", sbName))
			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.Bytes()).To(Equal(content))
		})

		It("forwards offset query param to sidecar", func() {
			var capturedOffset string
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedOffset = r.URL.Query().Get("offset")
				w.Header().Set("Content-Type", "application/octet-stream")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("partial"))
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/stderr?offset=10", sbName))
			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(capturedOffset).To(Equal("10"))
		})
	})

	Describe("sidecar error handling", func() {
		It("returns 502 when sidecar returns invalid JSON on status success", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("not json"))
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/status", sbName))
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("returns 502 when sidecar returns invalid JSON on command creation", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte("not json"))
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands", sbName),
				"Content-Type: application/json",
				strings.NewReader(`{"cmd":"echo"}`),
			)
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("forwards sidecar 400 errors with detail", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"detail": "failed to determine container pid",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands", sbName),
				"Content-Type: application/json",
				strings.NewReader(`{"cmd":"echo"}`),
			)
			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 502 for sidecar 500 errors on command creation", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands", sbName),
				"Content-Type: application/json",
				strings.NewReader(`{"cmd":"echo"}`),
			)
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("falls back to status text when sidecar error body is unreadable", func() {
			api := newCommandTestAPI(&brokenBodyDoer{statusCode: http.StatusBadRequest}, 0)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands", sbName),
				"Content-Type: application/json",
				strings.NewReader(`{"cmd":"echo"}`),
			)
			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})

		It("returns 502 when sidecar is unreachable for status", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			mockSidecar.Close()

			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/status", sbName))
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("returns 502 when sidecar is unreachable for stdin", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			mockSidecar.Close()

			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/stdin", sbName),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("returns 502 when sidecar is unreachable for stdout stream", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			mockSidecar.Close()

			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/stdout", sbName))
			Expect(resp.Code).To(Equal(http.StatusBadGateway))
		})

		It("forwards sidecar 409 conflict for stdin", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"detail": "command has already exited",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Post(
				fmt.Sprintf("/sandboxes/%s/commands/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/stdin", sbName),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("forwards sidecar 404 for stream endpoints", func() {
			mockSidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"detail": "command not found",
				})
			}))
			defer mockSidecar.Close()

			port := mockSidecar.Listener.Addr().(*net.TCPAddr).Port
			api := newCommandTestAPI(&http.Client{}, port)
			sbName := createRunningSandboxCR()

			resp := api.Get(fmt.Sprintf("/sandboxes/%s/commands/00000000-0000-0000-0000-000000000000/stdout", sbName))
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})
})
