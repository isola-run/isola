package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

var _ = Describe("Command Handlers", func() {
	var commandAPI humatest.TestAPI

	BeforeEach(func() {
		_, commandAPI = humatest.New(GinkgoT(), huma.DefaultConfig("Command Test API", "1.0.0"))

		mockProcFS := &MockProcFS{
			rootDir: testRootDir,
			cwd:     testCwd,
			uid:     0,
			gid:     0,
		}
		commandHandlers := NewCommandHandlersForTest(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), mockProcFS)
		RegisterCommandRoutes(commandAPI, commandHandlers)
	})

	postCommand := func(body string) (int, sidecarapi.CreateCommandResponse) {
		resp := commandAPI.Post("/commands", "Content-Type: application/json", strings.NewReader(body))
		var result sidecarapi.CreateCommandResponse
		if resp.Code < 300 {
			Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
		}
		return resp.Code, result
	}

	Describe("POST /commands", func() {
		It("returns 202 with commandId", func() {
			code, result := postCommand(`{"cmd": "echo", "args": ["hello"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			Expect(result.CommandID).NotTo(BeEmpty())
		})

		It("returns 422 on empty cmd", func() {
			resp := commandAPI.Post("/commands", "Content-Type: application/json", strings.NewReader(`{"cmd": ""}`))
			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

	})

	Describe("GET /commands/{cmdId}/status", func() {
		It("returns null exitCode while running", func() {
			code, result := postCommand(`{"cmd": "sleep", "args": ["60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
			Expect(resp.Code).To(Equal(http.StatusOK))

			var status sidecarapi.CommandStatusResponse
			Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
			Expect(status.ExitCode).To(BeNil())
		})

		It("returns 0 exit code on success", func() {
			code, result := postCommand(`{"cmd": "true"}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).Should(HaveValue(Equal(0)))
		})

		It("returns non-zero exit code on failure", func() {
			code, result := postCommand(`{"cmd": "false"}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).Should(HaveValue(Equal(1)))
		})

		It("returns 404 for unknown command", func() {
			resp := commandAPI.Get("/commands/nonexistent/status")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GET /commands/{cmdId}/stdout", func() {
		It("streams stdout output", func() {
			code, result := postCommand(`{"cmd": "echo", "args": ["-n", "hello world"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return resp.Body.String()
			}).Should(Equal("hello world"))
		})

		It("sets correct streaming headers", func() {
			code, result := postCommand(`{"cmd": "echo", "args": ["test"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return resp.Body.String()
			}).ShouldNot(BeEmpty())

			resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
			Expect(resp.Header().Get("Content-Type")).To(Equal("application/octet-stream"))
			Expect(resp.Header().Get("X-Accel-Buffering")).To(Equal("no"))
			Expect(resp.Header().Get("Cache-Control")).To(Equal("no-cache"))
		})

		It("supports resume via offset", func() {
			code, result := postCommand(`{"cmd": "echo", "args": ["-n", "hello world"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return resp.Body.String()
			}).Should(Equal("hello world"))

			resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout?offset=6", result.CommandID))
			Expect(resp.Body.String()).To(Equal("world"))
		})

		It("preserves binary data", func() {
			code, result := postCommand(`{"cmd": "/bin/sh", "args": ["-c", "printf 'a\\0b\\rc'"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() []byte {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return resp.Body.Bytes()
			}).Should(Equal([]byte{'a', 0, 'b', '\r', 'c'}))
		})

		It("returns 404 for unknown command", func() {
			resp := commandAPI.Get("/commands/nonexistent/stdout")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("GET /commands/{cmdId}/stderr", func() {
		It("routes stderr separately from stdout", func() {
			code, result := postCommand(`{"cmd": "/bin/sh", "args": ["-c", "echo -n err >&2; echo -n out"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stderr", result.CommandID))
				return resp.Body.String()
			}).Should(Equal("err"))

			resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
			Expect(resp.Body.String()).To(Equal("out"))
		})
	})

	Describe("POST /commands/{cmdId}/stdin", func() {
		It("writes to process stdin", func() {
			code, result := postCommand(`{"cmd": "/bin/sh", "args": ["-c", "head -c 5"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Post(
				fmt.Sprintf("/commands/%s/stdin", result.CommandID),
				"Content-Type: application/octet-stream",
				strings.NewReader("hello world"),
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return resp.Body.String()
			}).Should(Equal("hello"))
		})

		It("returns 404 for unknown command", func() {
			resp := commandAPI.Post("/commands/nonexistent/stdin", "Content-Type: application/octet-stream",
				strings.NewReader("data"))
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 409 for exited command", func() {
			code, result := postCommand(`{"cmd": "true"}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			resp := commandAPI.Post(
				fmt.Sprintf("/commands/%s/stdin", result.CommandID),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})
	})

	Describe("DELETE /commands/{cmdId}", func() {
		It("kills a running process", func() {
			code, result := postCommand(`{"cmd": "sleep", "args": ["60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Delete(fmt.Sprintf("/commands/%s", result.CommandID))
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())
		})

		It("is idempotent for exited commands", func() {
			code, result := postCommand(`{"cmd": "true"}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			resp := commandAPI.Delete(fmt.Sprintf("/commands/%s", result.CommandID))
			Expect(resp.Code).To(Equal(http.StatusNoContent))
		})

		It("returns 404 for unknown command", func() {
			resp := commandAPI.Delete("/commands/nonexistent")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Timeout", func() {
		It("kills the process after timeout expires", func() {
			code, result := postCommand(`{"cmd": "sleep", "args": ["60"], "timeout": 1}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}, "5s").ShouldNot(BeNil())
		})
	})

	Describe("Immediate exit", func() {
		It("output is available for already-exited commands", func() {
			code, result := postCommand(`{"cmd": "echo", "args": ["-n", "fast"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			// Wait for it to fully exit
			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			// Output should still be available
			resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.String()).To(Equal("fast"))
		})
	})
})
