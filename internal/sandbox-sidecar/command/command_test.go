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

package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isola-run/isola/internal/constants"
	sandboxsidecar "github.com/isola-run/isola/internal/sandbox-sidecar"
	sidecarapi "github.com/isola-run/isola/internal/sidecar-api"
)

// failAfterFirstWrite serves the first write and fails every one after it,
// simulating a stream that dies partway through.
type failAfterFirstWrite struct {
	httptest.ResponseRecorder
	err   error
	wrote bool
}

func (w *failAfterFirstWrite) Write(p []byte) (int, error) {
	if w.wrote {
		return 0, w.err
	}
	w.wrote = true
	return w.ResponseRecorder.Write(p)
}

// extractSSEData parses SSE events from a response body and returns the concatenated data.
func extractSSEData(body string) string {
	var result strings.Builder
	var dataParts []string

	for line := range strings.SplitSeq(body, "\n") {
		switch {
		case strings.HasPrefix(line, "data: "):
			dataParts = append(dataParts, line[6:])
		case line == "data:":
			dataParts = append(dataParts, "")
		case line == "":
			if len(dataParts) > 0 {
				result.WriteString(strings.Join(dataParts, "\n"))
				dataParts = dataParts[:0]
			}
		}
	}

	return result.String()
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

var _ = Describe("Command Handlers", func() {
	var (
		commandAPI      humatest.TestAPI
		commandHandler  http.Handler
		commandHandlers *Handlers
	)

	BeforeEach(func() {
		commandHandler, commandAPI = humatest.New(GinkgoT(), huma.DefaultConfig("Command Test API", "0.1.0"))

		v1 := huma.NewGroup(commandAPI, "/v1")
		mockProcFS := &MockProcFS{
			rootDir: testRootDir,
			cwd:     testCwd,
			uid:     0,
			gid:     0,
		}
		commandHandlers = New(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), mockProcFS, sandboxsidecar.NewPIDResolver(mockProcFS), &DirectCommandBuilder{})
		Register(v1, commandHandlers)
	})

	postCommand := func(body string) (int, sidecarapi.CreateCommandResponse) {
		resp := commandAPI.Post("/v1/commands", "Content-Type: application/json", strings.NewReader(body))
		var result sidecarapi.CreateCommandResponse
		if resp.Code < 300 {
			Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())
		}
		return resp.Code, result
	}

	Describe("POST /commands", func() {
		It("returns 202 with id", func() {
			code, result := postCommand(`{"args": ["echo", "hello"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			Expect(result.ID).NotTo(BeEmpty())
		})

		It("returns 422 on missing args", func() {
			resp := commandAPI.Post("/v1/commands", "Content-Type: application/json", strings.NewReader(`{}`))
			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

	})

	Describe("GET /commands/{id}/status", func() {
		It("returns null exitCode while running", func() {
			code, result := postCommand(`{"args": ["sleep", "60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
			Expect(resp.Code).To(Equal(http.StatusOK))

			var status sidecarapi.CommandStatusResponse
			Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
			Expect(status.ExitCode).To(BeNil())
		})

		It("returns 0 exit code on success", func() {
			code, result := postCommand(`{"args": ["true"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).Should(HaveValue(Equal(0)))
		})

		It("returns non-zero exit code on failure", func() {
			code, result := postCommand(`{"args": ["false"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).Should(HaveValue(Equal(1)))
		})

		It("returns 404 for unknown command", func() {
			resp := commandAPI.Get("/v1/commands/00000000-0000-0000-0000-000000000000/status")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 422 for invalid id format", func() {
			resp := commandAPI.Get("/v1/commands/not-a-uuid/status")
			Expect(resp.Code).To(Equal(http.StatusUnprocessableEntity))
		})

		It("blocks until process exits when waitSeconds is set", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "sleep 0.3"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			start := time.Now()
			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status?waitSeconds=5", result.ID))
			elapsed := time.Since(start)

			Expect(resp.Code).To(Equal(http.StatusOK))
			var status sidecarapi.CommandStatusResponse
			Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
			Expect(status.ExitCode).To(HaveValue(Equal(0)))
			Expect(elapsed).To(BeNumerically(">=", 200*time.Millisecond))
		})

		It("returns immediately when waitSeconds is set and process already exited", func() {
			code, result := postCommand(`{"args": ["true"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			start := time.Now()
			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status?waitSeconds=5", result.ID))
			elapsed := time.Since(start)

			Expect(resp.Code).To(Equal(http.StatusOK))
			var status sidecarapi.CommandStatusResponse
			Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
			Expect(status.ExitCode).To(HaveValue(Equal(0)))
			Expect(elapsed).To(BeNumerically("<", 100*time.Millisecond))
		})

		It("returns null exitCode when waitSeconds expires", func() {
			code, result := postCommand(`{"args": ["sleep", "60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			})

			start := time.Now()
			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status?waitSeconds=1", result.ID))
			elapsed := time.Since(start)

			Expect(resp.Code).To(Equal(http.StatusOK))
			var status sidecarapi.CommandStatusResponse
			Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
			Expect(status.ExitCode).To(BeNil())
			Expect(elapsed).To(BeNumerically(">=", 900*time.Millisecond))
		})

		It("returns client closed when a long poll disconnects", func() {
			code, result := postCommand(`{"args": ["sleep", "60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			})

			ctx, cancel := context.WithCancel(context.Background())
			req := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/status?waitSeconds=30", result.ID), nil).WithContext(ctx)
			w := httptest.NewRecorder()

			done := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				commandHandler.ServeHTTP(w, req)
				close(done)
			}()

			time.Sleep(100 * time.Millisecond)
			cancel()

			Eventually(done, "1s").Should(BeClosed())
			Expect(w.Code).To(Equal(statusClientClosedRequest))
		})
	})

	Describe("GET /commands/{id}/stdout", func() {
		It("streams stdout output", func() {
			code, result := postCommand(`{"args": ["echo", "-n", "hello world"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("hello world"))
		})

		It("sets correct streaming headers", func() {
			code, result := postCommand(`{"args": ["echo", "test"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).ShouldNot(BeEmpty())

			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
			Expect(resp.Header().Get("Content-Type")).To(Equal("text/event-stream"))
			Expect(resp.Header().Get("X-Accel-Buffering")).To(Equal("no"))
			Expect(resp.Header().Get("Cache-Control")).To(Equal("no-cache"))
		})

		It("supports resume via Last-Event-ID", func() {
			code, result := postCommand(`{"args": ["echo", "-n", "hello world"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("hello world"))

			req := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stdout", result.ID), nil)
			req.Header.Set("Last-Event-ID", "6")
			w := httptest.NewRecorder()
			commandHandler.ServeHTTP(w, req)
			Expect(extractSSEData(w.Body.String())).To(Equal("world"))
		})

		It("replaces invalid UTF-8 with replacement character", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "printf 'a\\377b'"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("a\uFFFDb"))
		})

		It("returns 404 for unknown command", func() {
			resp := commandAPI.Get("/v1/commands/00000000-0000-0000-0000-000000000000/stdout")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("emits id: field with byte offset in each event", func() {
			code, result := postCommand(`{"args": ["echo", "-n", "hello"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			var body string
			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				body = resp.Body.String()
				return extractSSEData(body)
			}).Should(Equal("hello"))

			Expect(body).To(MatchRegexp(`(?m)^id: \d+$`))
		})

		It("preserves multiline output through SSE", func() {
			code, result := postCommand(`{"args": ["printf", "line1\\nline2"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("line1\nline2"))
		})

		It("preserves trailing newline in output", func() {
			code, result := postCommand(`{"args": ["echo", "hello"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("hello\n"))
		})
	})

	Describe("GET /commands/{id}/stderr", func() {
		It("routes stderr separately from stdout", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "echo -n err >&2; echo -n out"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stderr", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("err"))

			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
			Expect(extractSSEData(resp.Body.String())).To(Equal("out"))
		})
	})

	Describe("POST /commands/{id}/stdin", func() {
		It("writes to process stdin", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "head -c 5"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin", result.ID),
				"Content-Type: application/octet-stream",
				strings.NewReader("hello world"),
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("hello"))
		})

		It("returns 404 for unknown command", func() {
			resp := commandAPI.Post("/v1/commands/00000000-0000-0000-0000-000000000000/stdin", "Content-Type: application/octet-stream",
				strings.NewReader("data"))
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})

		It("returns 409 for exited command", func() {
			code, result := postCommand(`{"args": ["true"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			resp := commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin", result.ID),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})
	})

	Describe("POST /commands/{id}/stdin error paths", func() {
		It("returns 409 when process closes its stdin while still running (EPIPE)", func() {
			// "exec 0<&-" closes fd 0 (stdin) in the shell process, which closes the
			// pipe's read end. The process stays alive (sleep 60) so <-entry.done and
			// stdinClosed checks pass, but io.Copy hits EPIPE from the kernel because
			// there's no reader left on the pipe.
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "exec 0<&-; echo -n ready >&2; sleep 60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			})

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stderr", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("ready"))

			resp := commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin", result.ID),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("returns 409 when stdin pipe handle is closed (ErrClosed)", func() {
			code, result := postCommand(`{"args": ["sleep", "60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			})

			// Directly close the write end of the pipe without setting stdinClosed.
			// This simulates the race where cmd.Wait() closes the pipe handle
			// (via closeDescriptors) before PostCommandStdin checks stdinClosed.
			commandHandlers.cmdMu.RLock()
			entry := commandHandlers.commands[result.ID]
			commandHandlers.cmdMu.RUnlock()
			Expect(entry.stdinPipe.Close()).To(Succeed())

			resp := commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin", result.ID),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})
	})

	Describe("POST /commands/{id}/stdin/close", func() {
		It("closes stdin and sends EOF", func() {
			code, result := postCommand(`{"args": ["cat"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			// Write some data to stdin
			resp := commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin", result.ID),
				"Content-Type: application/octet-stream",
				strings.NewReader("hello"),
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			// Close stdin — cat should see EOF and exit
			resp = commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin/close", result.ID),
				"",
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			// cat should exit with 0 after receiving EOF
			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).Should(HaveValue(Equal(0)))

			// Verify the data was echoed
			resp = commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
			Expect(extractSSEData(resp.Body.String())).To(Equal("hello"))
		})

		It("returns 409 when closing already closed stdin", func() {
			code, result := postCommand(`{"args": ["cat"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin/close", result.ID),
				"",
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			// Second close should return 409
			resp = commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin/close", result.ID),
				"",
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("returns 409 for exited command", func() {
			code, result := postCommand(`{"args": ["true"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			resp := commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin/close", result.ID),
				"",
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("write after close returns 409", func() {
			code, result := postCommand(`{"args": ["cat"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin/close", result.ID),
				"",
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			resp = commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin", result.ID),
				"Content-Type: application/octet-stream",
				strings.NewReader("data"),
			)
			Expect(resp.Code).To(Equal(http.StatusConflict))
		})

		It("returns 404 for unknown command", func() {
			resp := commandAPI.Post("/v1/commands/00000000-0000-0000-0000-000000000000/stdin/close", "")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("DELETE /commands/{id}", func() {
		It("kills a running process", func() {
			code, result := postCommand(`{"args": ["sleep", "60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())
		})

		It("is idempotent for exited commands", func() {
			code, result := postCommand(`{"args": ["true"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			resp := commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			Expect(resp.Code).To(Equal(http.StatusNoContent))
		})

		It("returns 404 for unknown command", func() {
			resp := commandAPI.Delete("/v1/commands/00000000-0000-0000-0000-000000000000")
			Expect(resp.Code).To(Equal(http.StatusNotFound))
		})
	})

	Describe("Timeout", func() {
		It("kills the process after timeout expires", func() {
			code, result := postCommand(`{"args": ["sleep", "60"], "timeoutSeconds": 1}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}, "5s").ShouldNot(BeNil())
		})

		It("preserves partial output and reports kill exit code", func() {
			code, result := postCommand(`{
				"args": ["/bin/sh", "-c", "echo -n before-timeout; sleep 60"],
				"timeoutSeconds": 1
			}`)
			Expect(code).To(Equal(http.StatusAccepted))

			var exitCode *int
			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				exitCode = status.ExitCode
				return status.ExitCode
			}, "5s").ShouldNot(BeNil())

			// SIGKILL: Go reports -1 for signal kills
			Expect(*exitCode).To(Equal(-1))

			// Output written before the kill should be preserved
			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
			Expect(extractSSEData(resp.Body.String())).To(Equal("before-timeout"))
		})
	})

	Describe("empty stream", func() {
		It("writes a keepalive comment so the response status is committed", func() {
			// A command that writes to stdout only — stderr is empty.
			code, result := postCommand(`{"args": ["echo", "-n", "hello"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			// Wait for process to fully exit
			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			// Stream stderr — it will be empty since the command wrote nothing to stderr.
			// The response body must still be non-empty (at least a keepalive comment)
			// so that WriteHeader(200) fires and the httplog middleware records the
			// correct status code instead of "0 Unknown".
			req := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stderr", result.ID), nil)
			w := httptest.NewRecorder()
			commandHandler.ServeHTTP(w, req)
			Expect(w.Body.String()).To(ContainSubstring(": keepalive"))
			Expect(extractSSEData(w.Body.String())).To(BeEmpty())
		})
	})

	Describe("Immediate exit", func() {
		It("output is available for already-exited commands", func() {
			code, result := postCommand(`{"args": ["echo", "-n", "fast"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			// Wait for it to fully exit
			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			// Output should still be available
			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(extractSSEData(resp.Body.String())).To(Equal("fast"))
		})
	})

	Describe("stream termination on process exit", func() {
		It("stream response closes when process exits", func() {
			code, result := postCommand(`{"args": ["echo", "-n", "hello"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			// Start streaming stdout via ServeHTTP directly (like client disconnect test)
			req := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stdout", result.ID), nil)
			w := httptest.NewRecorder()

			done := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				commandHandler.ServeHTTP(w, req)
				close(done)
			}()

			// The goroutine should complete once the process exits and the stream closes
			Eventually(done, "2s").Should(BeClosed())
			Expect(extractSSEData(w.Body.String())).To(Equal("hello"))
		})

		It("stream delivers final output written just before exit", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "sleep 0.2; echo -n final"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			// Start streaming immediately while the process is still sleeping
			req := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stdout", result.ID), nil)
			w := httptest.NewRecorder()

			done := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				commandHandler.ServeHTTP(w, req)
				close(done)
			}()

			Eventually(done, "5s").Should(BeClosed())
			Expect(extractSSEData(w.Body.String())).To(Equal("final"))
		})

		It("concurrent stdout and stderr streams both close on exit", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "echo -n out; echo -n err >&2"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			stdoutReq := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stdout", result.ID), nil)
			stdoutW := httptest.NewRecorder()

			stderrReq := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stderr", result.ID), nil)
			stderrW := httptest.NewRecorder()

			stdoutDone := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				commandHandler.ServeHTTP(stdoutW, stdoutReq)
				close(stdoutDone)
			}()

			stderrDone := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				commandHandler.ServeHTTP(stderrW, stderrReq)
				close(stderrDone)
			}()

			Eventually(stdoutDone, "2s").Should(BeClosed())
			Eventually(stderrDone, "2s").Should(BeClosed())
			Expect(extractSSEData(stdoutW.Body.String())).To(Equal("out"))
			Expect(extractSSEData(stderrW.Body.String())).To(Equal("err"))
		})
	})

	Describe("output directory creation failure", func() {
		It("returns 500 when MkdirAll fails", func() {
			// Create a separate mock whose rootDir has a file blocking the path.
			// MkdirAll will fail because "var" is a regular file, not a directory.
			blockedRoot, err := os.MkdirTemp("", "sidecar-test-blocked-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(blockedRoot) })

			// Place a regular file at <root>/var so MkdirAll(<root>/var/run/isola/...) fails
			Expect(os.WriteFile(filepath.Join(blockedRoot, "var"), []byte("blocker"), 0600)).To(Succeed())

			_, blockedAPI := humatest.New(GinkgoT(), huma.DefaultConfig("Blocked Test API", "0.1.0"))
			blockedV1 := huma.NewGroup(blockedAPI, "/v1")
			blockedMock := &MockProcFS{rootDir: blockedRoot, cwd: testCwd}
			blockedHandlers := New(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), blockedMock, sandboxsidecar.NewPIDResolver(blockedMock), &DirectCommandBuilder{})
			Register(blockedV1, blockedHandlers)

			resp := blockedAPI.Post("/v1/commands", "Content-Type: application/json",
				strings.NewReader(`{"args": ["echo", "hello"]}`))
			Expect(resp.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("streaming output file errors", func() {
		It("aborts streaming on file read error instead of spinning", func() {
			code, result := postCommand(`{"args": ["sleep", "60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			})

			// Get the stdout path from the command entry
			commandHandlers.cmdMu.RLock()
			entry := commandHandlers.commands[result.ID]
			commandHandlers.cmdMu.RUnlock()

			// Replace the stdout file with a directory.
			// os.Open on a directory succeeds, but f.Read returns EISDIR.
			stdoutPath := filepath.Join(entry.outputDir, "stdout")
			Expect(os.Remove(stdoutPath)).To(Succeed())
			Expect(os.Mkdir(stdoutPath, 0750)).To(Succeed())
			DeferCleanup(func() { _ = os.Remove(stdoutPath) })

			// Run the streaming request in a goroutine since with the bug
			// it spins forever retrying the failing read.
			done := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				defer close(done)
				Expect(func() {
					commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				}).To(PanicWith(http.ErrAbortHandler))
			}()

			// With the fix, streamOutput detects the non-EOF read error and aborts.
			// Without the fix, it loops forever (sleep 100ms between retries).
			Eventually(done, "2s").Should(BeClosed())
		})

		It("returns 500 when the output file cannot be opened", func() {
			code, result := postCommand(`{"args": ["sleep", "60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			})

			commandHandlers.cmdMu.RLock()
			entry := commandHandlers.commands[result.ID]
			commandHandlers.cmdMu.RUnlock()

			// Nothing has been streamed yet, so the failure is reportable as a status
			// rather than an abort.
			Expect(os.Remove(filepath.Join(entry.outputDir, "stderr"))).To(Succeed())

			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stderr", result.ID))
			Expect(resp.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("streaming write errors", func() {
		// The initial keepalive lands, then the first data write fails.
		streamOnto := func(writeErr error) func() {
			code, result := postCommand(`{"args": ["echo", "hello"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			})

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			mock := &failAfterFirstWrite{ResponseRecorder: *httptest.NewRecorder(), err: writeErr}
			req := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stdout", result.ID), nil)
			return func() { commandHandler.ServeHTTP(mock, req) }
		}

		It("aborts the response when an SSE write fails", func() {
			Expect(streamOnto(errors.New("stream broke"))).To(PanicWith(http.ErrAbortHandler))
		})

		It("ends quietly when the client disconnects mid-stream", func() {
			Expect(streamOnto(syscall.EPIPE)).NotTo(Panic())
		})
	})

	Describe("output directory cleanup on start failure", func() {
		It("exits with code 127 when command binary does not exist", func() {
			// With shell-wrapping, /bin/sh starts successfully (202 returned) and
			// then fails to exec the missing binary, exiting with code 127.
			// The output directory is NOT cleaned up in this case (cmd.Start succeeded).
			code, result := postCommand(`{"args": ["/nonexistent/binary"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).Should(HaveValue(Equal(127)))
		})
	})

	Describe("Last-Event-ID beyond file size", func() {
		It("returns empty body when offset exceeds output size for exited command", func() {
			code, result := postCommand(`{"args": ["echo", "-n", "short"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			req := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stdout", result.ID), nil)
			req.Header.Set("Last-Event-ID", "9999")
			w := httptest.NewRecorder()
			commandHandler.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(extractSSEData(w.Body.String())).To(BeEmpty())
		})
	})

	Describe("multiple stdin writes", func() {
		It("delivers sequential writes in order", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "head -c 10"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin", result.ID),
				"Content-Type: application/octet-stream",
				strings.NewReader("hello"),
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			resp = commandAPI.Post(
				fmt.Sprintf("/v1/commands/%s/stdin", result.ID),
				"Content-Type: application/octet-stream",
				strings.NewReader("world"),
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("helloworld"))
		})
	})

	Describe("client disconnect", func() {
		It("stops streaming when request context is cancelled", func() {
			code, result := postCommand(`{"args": ["sleep", "60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			})

			// humatest.TestAPI.Get() doesn't let us control the request context,
			// so we call ServeHTTP directly with a cancellable context
			ctx, cancel := context.WithCancel(context.Background())
			req := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stdout", result.ID), nil).WithContext(ctx)
			w := httptest.NewRecorder()

			done := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				commandHandler.ServeHTTP(w, req)
				close(done)
			}()

			time.Sleep(100 * time.Millisecond)
			cancel()

			Eventually(done, "1s").Should(BeClosed())
		})
	})

	Describe("exec shell-wrapping correctness", func() {
		It("passes arguments with spaces and special characters verbatim", func() {
			// exec "$@" passes each element of req.Args as a separate word — no shell
			// word-splitting or globbing. This verifies that args are forwarded correctly
			// through the /bin/sh -c 'exec "$@"' -- wrapper.
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "printf '%s\n' \"$@\"", "--", "hello world", "foo  bar", "a*b"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("hello world\nfoo  bar\na*b\n"))
		})

		It("resolves bare command names via PATH from cmd.Env", func() {
			// Production behavior: a bare name like "echo" is resolved by the shell
			// using the PATH from the container's environment. This test verifies that
			// the shell-wrapper (not Go's LookPath) performs the resolution.
			code, result := postCommand(`{"args": ["echo", "-n", "via-path"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("via-path"))
		})

		It("exec replaces the shell so the user process is killed directly", func() {
			// With exec "$@", the shell is replaced by the user process (same PID).
			// Cancelling the context sends SIGKILL to that process directly, not to a
			// shell parent that would need to propagate the signal.
			code, result := postCommand(`{"args": ["sleep", "60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", result.ID))
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			// Exit code -1 means the process was killed by a signal (SIGKILL).
			// If exec didn't replace the shell, we might get the shell's exit code instead.
			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).Should(HaveValue(Equal(-1)))
		})
	})

	Describe("process group termination", func() {
		startWithChild := func(extra map[string]any) (string, int) {
			f, err := os.CreateTemp("", "sidecar-child-pid-*")
			Expect(err).NotTo(HaveOccurred())
			pidFile := f.Name()
			Expect(f.Close()).To(Succeed())
			DeferCleanup(func() { _ = os.Remove(pidFile) })

			req := map[string]any{
				"args": []string{"/bin/sh", "-c", `sleep 300 & echo $! > "$CHILD_PID_FILE"; wait`},
				"env":  map[string]string{"CHILD_PID_FILE": pidFile},
			}
			for k, v := range extra {
				req[k] = v
			}
			body, err := json.Marshal(req)
			Expect(err).NotTo(HaveOccurred())

			resp := commandAPI.Post("/v1/commands", "Content-Type: application/json", strings.NewReader(string(body)))
			Expect(resp.Code).To(Equal(http.StatusAccepted))
			var result sidecarapi.CreateCommandResponse
			Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())

			var childPID int
			Eventually(func() bool {
				data, readErr := os.ReadFile(pidFile) //nolint:gosec // test-created temp file path
				if readErr != nil {
					return false
				}
				pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
				if convErr != nil {
					return false
				}
				childPID = pid
				return true
			}, "3s", "20ms").Should(BeTrue())

			DeferCleanup(func() { _ = syscall.Kill(childPID, syscall.SIGKILL) })
			return result.ID, childPID
		}

		waitExited := func(id string) {
			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", id))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}, "5s").ShouldNot(BeNil())
		}

		It("kills the whole process tree on DELETE", func() {
			id, childPID := startWithChild(nil)
			Expect(processAlive(childPID)).To(BeTrue(), "child should be running before kill")

			resp := commandAPI.Delete(fmt.Sprintf("/v1/commands/%s", id))
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			waitExited(id)
			Eventually(func() bool { return processAlive(childPID) }, "3s", "20ms").
				Should(BeFalse(), "forked child must not survive after the command is killed")
		})

		It("kills the whole process tree on timeout", func() {
			id, childPID := startWithChild(map[string]any{"timeoutSeconds": 1})
			Expect(processAlive(childPID)).To(BeTrue(), "child should be running before timeout")

			waitExited(id)
			Eventually(func() bool { return processAlive(childPID) }, "3s", "20ms").
				Should(BeFalse(), "forked child must not survive after the command times out")
		})
	})

	Describe("working directory", func() {
		It("runs command in specified cwd", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "pwd"], "cwd": "/tmp"}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return strings.TrimSpace(extractSSEData(resp.Body.String()))
			}).Should(Equal("/tmp"))
		})

		It("returns 400 when cwd does not exist", func() {
			resp := commandAPI.Post("/v1/commands", "Content-Type: application/json",
				strings.NewReader(`{"args": ["pwd"], "cwd": "/nonexistent_path"}`))
			Expect(resp.Code).To(Equal(http.StatusBadRequest))
			Expect(resp.Body.String()).To(ContainSubstring("working directory does not exist"))
		})

		It("returns 400 with distinct message when cwd is a file", func() {
			f, err := os.Create(filepath.Join(testRootDir, "afile"))
			Expect(err).NotTo(HaveOccurred())
			_ = f.Close()
			DeferCleanup(func() { _ = os.Remove(filepath.Join(testRootDir, "afile")) })

			resp := commandAPI.Post("/v1/commands", "Content-Type: application/json",
				strings.NewReader(`{"args": ["pwd"], "cwd": "/afile"}`))
			Expect(resp.Code).To(Equal(http.StatusBadRequest))
			Expect(resp.Body.String()).To(ContainSubstring("not a directory"))
		})

		It("returns 500 when cwd stat fails for reasons other than not-exist", func() {
			isolatedRoot, err := os.MkdirTemp("", "sidecar-test-stat-fail-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(isolatedRoot) })

			// Symlink loop: stat returns ELOOP, not ENOENT.
			Expect(os.Symlink("loop", filepath.Join(isolatedRoot, "loop"))).To(Succeed())

			_, isolatedAPI := humatest.New(GinkgoT(), huma.DefaultConfig("Stat Fail API", "0.1.0"))
			isolatedV1 := huma.NewGroup(isolatedAPI, "/v1")
			isolatedMock := &MockProcFS{rootDir: isolatedRoot, cwd: testCwd}
			isolatedHandlers := New(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), isolatedMock, sandboxsidecar.NewPIDResolver(isolatedMock), &DirectCommandBuilder{})
			Register(isolatedV1, isolatedHandlers)

			resp := isolatedAPI.Post("/v1/commands", "Content-Type: application/json",
				strings.NewReader(`{"args": ["pwd"], "cwd": "/loop/sub"}`))
			Expect(resp.Code).To(Equal(http.StatusInternalServerError))
		})

		It("resolves .. in cwd against the container root, not the sidecar filesystem", func() {
			// /usr exists on the sidecar but not under testRootDir. Without cleaning,
			// filepath.Join(testRootDir, "../../../../../../usr") escapes testRootDir
			// and hits the sidecar's /usr, making validation pass incorrectly.
			resp := commandAPI.Post("/v1/commands", "Content-Type: application/json",
				strings.NewReader(`{"args": ["pwd"], "cwd": "../../../../../../usr"}`))
			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("environment variables", func() {
		It("makes user-specified env vars available to the process", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "echo -n $MY_VAR"], "env": {"MY_VAR": "hello"}}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("hello"))
		})

		It("inherits container environment variables", func() {
			// MockProcFS.GetEnviron returns PATH=/usr/bin:/bin and HOME=/root
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "echo -n $HOME"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("/root"))
		})
	})

	Describe("concurrent commands", func() {
		It("runs multiple commands independently", func() {
			const n = 5
			type cmdResult struct {
				id   string
				want string
			}
			cmds := make([]cmdResult, n)

			for i := range n {
				body := fmt.Sprintf(`{"args": ["echo", "-n", "cmd-%d"]}`, i)
				code, result := postCommand(body)
				Expect(code).To(Equal(http.StatusAccepted))
				cmds[i] = cmdResult{id: result.ID, want: fmt.Sprintf("cmd-%d", i)}
			}

			for _, c := range cmds {
				Eventually(func() string {
					resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", c.id))
					return extractSSEData(resp.Body.String())
				}).Should(Equal(c.want))
			}
		})
	})

	Describe("PID resolution failure", func() {
		It("returns 400 when container PID cannot be found", func() {
			_, failingAPI := humatest.New(GinkgoT(), huma.DefaultConfig("PID Fail Test API", "0.1.0"))
			failingV1 := huma.NewGroup(failingAPI, "/v1")
			failingMock := &MockProcFS{
				rootDir:       testRootDir,
				cwd:           testCwd,
				findMarkedErr: fmt.Errorf("container not found"),
			}
			failingHandlers := New(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), failingMock, sandboxsidecar.NewPIDResolver(failingMock), &DirectCommandBuilder{})
			Register(failingV1, failingHandlers)

			resp := failingAPI.Post("/v1/commands", "Content-Type: application/json",
				strings.NewReader(`{"args": ["echo"]}`))
			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("stderr Last-Event-ID", func() {
		It("supports resume via Last-Event-ID on stderr", func() {
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "echo -n 'hello world' >&2"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stderr", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("hello world"))

			req := httptest.NewRequest("GET", fmt.Sprintf("/v1/commands/%s/stderr", result.ID), nil)
			req.Header.Set("Last-Event-ID", "6")
			w := httptest.NewRecorder()
			commandHandler.ServeHTTP(w, req)
			Expect(extractSSEData(w.Body.String())).To(Equal("world"))
		})
	})

	Describe("large output", func() {
		It("streams large output without data loss", func() {
			// Generate 1MB of output via dd
			code, result := postCommand(`{"args": ["/bin/sh", "-c", "dd if=/dev/zero bs=1024 count=1024 2>/dev/null"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/status", result.ID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}, "5s").ShouldNot(BeNil())

			resp := commandAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
			Expect(extractSSEData(resp.Body.String())).To(HaveLen(1024 * 1024))
		})
	})

	Describe("command start failure cleanup", func() {
		It("cleans up output directory when command builder fails", func() {
			isolatedRoot, err := os.MkdirTemp("", "sidecar-test-builder-fail-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(isolatedRoot) })

			_, isolatedAPI := humatest.New(GinkgoT(), huma.DefaultConfig("Builder Fail API", "0.1.0"))
			isolatedV1 := huma.NewGroup(isolatedAPI, "/v1")
			isolatedMock := &MockProcFS{rootDir: isolatedRoot, cwd: testCwd}
			failBuilder := &FailingCommandBuilder{err: fmt.Errorf("build error")}
			isolatedHandlers := New(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), isolatedMock, sandboxsidecar.NewPIDResolver(isolatedMock), failBuilder)
			Register(isolatedV1, isolatedHandlers)

			resp := isolatedAPI.Post("/v1/commands", "Content-Type: application/json",
				strings.NewReader(`{"args": ["echo"]}`))
			Expect(resp.Code).To(Equal(http.StatusInternalServerError))

			// Verify cleanup happened
			commandsDir := filepath.Join(isolatedRoot, "var", "run", "isola", "commands")
			entries, err := os.ReadDir(commandsDir)
			if err != nil && !os.IsNotExist(err) {
				Fail(fmt.Sprintf("unexpected error reading commands dir: %v", err))
			}
			if err == nil {
				Expect(entries).To(BeEmpty(), "orphaned command output directory after build failure")
			}
		})
	})

	Describe("GetEnviron failure", func() {
		It("proceeds with empty env when container environ is unreadable", func() {
			_, envFailAPI := humatest.New(GinkgoT(), huma.DefaultConfig("Env Fail API", "0.1.0"))
			envFailV1 := huma.NewGroup(envFailAPI, "/v1")
			envFailMock := &MockProcFS{
				rootDir:       testRootDir,
				cwd:           testCwd,
				getEnvironErr: fmt.Errorf("permission denied"),
			}
			envFailHandlers := New(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), envFailMock, sandboxsidecar.NewPIDResolver(envFailMock), &DirectCommandBuilder{})
			Register(envFailV1, envFailHandlers)

			resp := envFailAPI.Post("/v1/commands", "Content-Type: application/json",
				strings.NewReader(`{"args": ["echo", "-n", "ok"]}`))
			Expect(resp.Code).To(Equal(http.StatusAccepted))

			var result sidecarapi.CreateCommandResponse
			Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())

			Eventually(func() string {
				resp := envFailAPI.Get(fmt.Sprintf("/v1/commands/%s/stdout", result.ID))
				return extractSSEData(resp.Body.String())
			}).Should(Equal("ok"))
		})
	})
})

var _ = Describe("buildCmdEnv", func() {
	sorted := func(env []string) []string {
		sort.Strings(env)
		return env
	}

	It("merges container env with overrides", func() {
		containerEnv := []string{"PATH=/usr/bin", "HOME=/root"}
		overrides := map[string]string{"FOO": "bar"}

		result := sorted(buildCmdEnv(containerEnv, overrides))
		Expect(result).To(Equal([]string{"FOO=bar", "HOME=/root", "PATH=/usr/bin"}))
	})

	It("overrides take precedence over container env", func() {
		containerEnv := []string{"PATH=/usr/bin", "HOME=/root"}
		overrides := map[string]string{"PATH": "/custom/bin"}

		result := sorted(buildCmdEnv(containerEnv, overrides))
		Expect(result).To(Equal([]string{"HOME=/root", "PATH=/custom/bin"}))
	})

	It("strips ISOLA_CONTAINER_NAME from output", func() {
		containerEnv := []string{
			"PATH=/usr/bin",
			constants.IsolaContainerNameEnv + "=mycontainer",
		}

		result := sorted(buildCmdEnv(containerEnv, nil))
		Expect(result).To(Equal([]string{"PATH=/usr/bin"}))
	})

	It("strips ISOLA_CONTAINER_NAME even if set via overrides", func() {
		containerEnv := []string{"PATH=/usr/bin"}
		overrides := map[string]string{constants.IsolaContainerNameEnv: "sneaky"}

		result := sorted(buildCmdEnv(containerEnv, overrides))
		Expect(result).To(Equal([]string{"PATH=/usr/bin"}))
	})

	It("handles nil containerEnv", func() {
		overrides := map[string]string{"FOO": "bar"}
		result := buildCmdEnv(nil, overrides)
		Expect(result).To(Equal([]string{"FOO=bar"}))
	})

	It("handles nil overrides", func() {
		containerEnv := []string{"PATH=/usr/bin"}
		result := buildCmdEnv(containerEnv, nil)
		Expect(result).To(Equal([]string{"PATH=/usr/bin"}))
	})

	It("skips malformed env entries without '='", func() {
		containerEnv := []string{"GOOD=value", "MALFORMED"}
		result := sorted(buildCmdEnv(containerEnv, nil))
		Expect(result).To(Equal([]string{"GOOD=value"}))
	})

	It("handles env values containing '='", func() {
		containerEnv := []string{"CONFIG=key=value"}
		result := buildCmdEnv(containerEnv, nil)
		Expect(result).To(Equal([]string{"CONFIG=key=value"}))
	})
})
