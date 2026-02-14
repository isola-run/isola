package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/isola-ai/isola-sb/internal/constants"
	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

var _ = Describe("Command Handlers", func() {
	var (
		commandAPI      humatest.TestAPI
		commandHandler  http.Handler
		commandHandlers *CommandHandlers
	)

	BeforeEach(func() {
		commandHandler, commandAPI = humatest.New(GinkgoT(), huma.DefaultConfig("Command Test API", "1.0.0"))

		mockProcFS := &MockProcFS{
			rootDir: testRootDir,
			cwd:     testCwd,
			uid:     0,
			gid:     0,
		}
		commandHandlers = NewCommandHandlers(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), mockProcFS, &DirectCommandBuilder{})
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

		It("preserves partial output and reports kill exit code", func() {
			code, result := postCommand(`{
				"cmd": "/bin/sh",
				"args": ["-c", "echo -n before-timeout; sleep 60"],
				"timeout": 1
			}`)
			Expect(code).To(Equal(http.StatusAccepted))

			var exitCode *int
			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				exitCode = status.ExitCode
				return status.ExitCode
			}, "5s").ShouldNot(BeNil())

			// SIGKILL: Go reports -1 for signal kills
			Expect(*exitCode).To(Equal(-1))

			// Output written before the kill should be preserved
			resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
			Expect(resp.Body.String()).To(Equal("before-timeout"))
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

	Describe("output directory creation failure", func() {
		It("returns 500 when MkdirAll fails", func() {
			// Create a separate mock whose rootDir has a file blocking the path.
			// MkdirAll will fail because "var" is a regular file, not a directory.
			blockedRoot, err := os.MkdirTemp("", "sidecar-test-blocked-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(blockedRoot) })

			// Place a regular file at <root>/var so MkdirAll(<root>/var/run/isola/...) fails
			Expect(os.WriteFile(filepath.Join(blockedRoot, "var"), []byte("blocker"), 0600)).To(Succeed())

			_, blockedAPI := humatest.New(GinkgoT(), huma.DefaultConfig("Blocked Test API", "1.0.0"))
			blockedMock := &MockProcFS{rootDir: blockedRoot, cwd: testCwd}
			blockedHandlers := NewCommandHandlers(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), blockedMock, &DirectCommandBuilder{})
			RegisterCommandRoutes(blockedAPI, blockedHandlers)

			resp := blockedAPI.Post("/commands", "Content-Type: application/json",
				strings.NewReader(`{"cmd": "echo", "args": ["hello"]}`))
			Expect(resp.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("streaming file read errors", func() {
		It("aborts streaming on file read error instead of spinning", func() {
			code, result := postCommand(`{"cmd": "sleep", "args": ["60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/commands/%s", result.CommandID))
			})

			// Get the stdout path from the command entry
			commandHandlers.cmdMu.RLock()
			entry := commandHandlers.commands[result.CommandID]
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
				commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				close(done)
			}()

			// With the fix, streamOutput detects the non-EOF read error and returns.
			// Without the fix, it loops forever (sleep 100ms between retries).
			Eventually(done, "2s").Should(BeClosed())
		})
	})

	Describe("output directory cleanup on start failure", func() {
		It("cleans up output directory when command binary does not exist", func() {
			// Use an isolated rootDir so other tests' command dirs don't interfere
			isolatedRoot, err := os.MkdirTemp("", "sidecar-test-cleanup-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(isolatedRoot) })

			_, isolatedAPI := humatest.New(GinkgoT(), huma.DefaultConfig("Cleanup Test API", "1.0.0"))
			isolatedMock := &MockProcFS{rootDir: isolatedRoot, cwd: testCwd}
			isolatedHandlers := NewCommandHandlers(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), isolatedMock, &DirectCommandBuilder{})
			RegisterCommandRoutes(isolatedAPI, isolatedHandlers)

			resp := isolatedAPI.Post("/commands", "Content-Type: application/json",
				strings.NewReader(`{"cmd": "/nonexistent/binary"}`))
			Expect(resp.Code).To(Equal(http.StatusInternalServerError))

			// Directory may not exist (RemoveAll removed it) or may be empty — either is acceptable
			commandsDir := filepath.Join(isolatedRoot, "var", "run", "isola", "commands")
			entries, err := os.ReadDir(commandsDir)
			if err != nil && !os.IsNotExist(err) {
				Fail(fmt.Sprintf("unexpected error reading commands dir: %v", err))
			}
			if err == nil {
				Expect(entries).To(BeEmpty(), "orphaned command output directory after failed start")
			}
		})
	})

	Describe("offset beyond file size", func() {
		It("returns empty body when offset exceeds output size for exited command", func() {
			code, result := postCommand(`{"cmd": "echo", "args": ["-n", "short"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}).ShouldNot(BeNil())

			resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout?offset=9999", result.CommandID))
			Expect(resp.Code).To(Equal(http.StatusOK))
			Expect(resp.Body.String()).To(BeEmpty())
		})
	})

	Describe("multiple stdin writes", func() {
		It("delivers sequential writes in order", func() {
			code, result := postCommand(`{"cmd": "/bin/sh", "args": ["-c", "head -c 10"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Post(
				fmt.Sprintf("/commands/%s/stdin", result.CommandID),
				"Content-Type: application/octet-stream",
				strings.NewReader("hello"),
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			resp = commandAPI.Post(
				fmt.Sprintf("/commands/%s/stdin", result.CommandID),
				"Content-Type: application/octet-stream",
				strings.NewReader("world"),
			)
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return resp.Body.String()
			}).Should(Equal("helloworld"))
		})
	})

	Describe("client disconnect", func() {
		It("stops streaming when request context is cancelled", func() {
			code, result := postCommand(`{"cmd": "sleep", "args": ["60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))
			DeferCleanup(func() {
				commandAPI.Delete(fmt.Sprintf("/commands/%s", result.CommandID))
			})

			// humatest.TestAPI.Get() doesn't let us control the request context,
			// so we call ServeHTTP directly with a cancellable context
			ctx, cancel := context.WithCancel(context.Background())
			req := httptest.NewRequest("GET", fmt.Sprintf("/commands/%s/stdout", result.CommandID), nil).WithContext(ctx)
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

	Describe("working directory", func() {
		It("runs command in specified cwd", func() {
			code, result := postCommand(`{"cmd": "/bin/sh", "args": ["-c", "pwd"], "cwd": "/tmp"}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return strings.TrimSpace(resp.Body.String())
			}).Should(Equal("/tmp"))
		})
	})

	Describe("environment variables", func() {
		It("makes user-specified env vars available to the process", func() {
			code, result := postCommand(`{"cmd": "/bin/sh", "args": ["-c", "echo -n $MY_VAR"], "env": {"MY_VAR": "hello"}}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return resp.Body.String()
			}).Should(Equal("hello"))
		})

		It("inherits container environment variables", func() {
			// MockProcFS.GetEnviron returns PATH=/usr/bin:/bin and HOME=/root
			code, result := postCommand(`{"cmd": "/bin/sh", "args": ["-c", "echo -n $HOME"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return resp.Body.String()
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
				body := fmt.Sprintf(`{"cmd": "echo", "args": ["-n", "cmd-%d"]}`, i)
				code, result := postCommand(body)
				Expect(code).To(Equal(http.StatusAccepted))
				cmds[i] = cmdResult{id: result.CommandID, want: fmt.Sprintf("cmd-%d", i)}
			}

			for _, c := range cmds {
				Eventually(func() string {
					resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", c.id))
					return resp.Body.String()
				}).Should(Equal(c.want))
			}
		})
	})

	Describe("PID resolution failure", func() {
		It("returns 400 when container PID cannot be found", func() {
			_, failingAPI := humatest.New(GinkgoT(), huma.DefaultConfig("PID Fail Test API", "1.0.0"))
			failingMock := &MockProcFS{
				rootDir:       testRootDir,
				cwd:           testCwd,
				findMarkedErr: fmt.Errorf("container not found"),
			}
			failingHandlers := NewCommandHandlers(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), failingMock, &DirectCommandBuilder{})
			RegisterCommandRoutes(failingAPI, failingHandlers)

			resp := failingAPI.Post("/commands", "Content-Type: application/json",
				strings.NewReader(`{"cmd": "echo"}`))
			Expect(resp.Code).To(Equal(http.StatusBadRequest))
		})
	})

	Describe("stderr offset", func() {
		It("supports resume via offset on stderr", func() {
			code, result := postCommand(`{"cmd": "/bin/sh", "args": ["-c", "echo -n 'hello world' >&2"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() string {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stderr", result.CommandID))
				return resp.Body.String()
			}).Should(Equal("hello world"))

			resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stderr?offset=6", result.CommandID))
			Expect(resp.Body.String()).To(Equal("world"))
		})
	})

	Describe("kill exit code", func() {
		It("reports signal kill exit code as -1", func() {
			code, result := postCommand(`{"cmd": "sleep", "args": ["60"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			resp := commandAPI.Delete(fmt.Sprintf("/commands/%s", result.CommandID))
			Expect(resp.Code).To(Equal(http.StatusNoContent))

			var exitCode *int
			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				exitCode = status.ExitCode
				return exitCode
			}).ShouldNot(BeNil())

			Expect(*exitCode).To(Equal(-1))
		})
	})

	Describe("large output", func() {
		It("streams large output without data loss", func() {
			// Generate 1MB of output via dd
			code, result := postCommand(`{"cmd": "/bin/sh", "args": ["-c", "dd if=/dev/zero bs=1024 count=1024 2>/dev/null"]}`)
			Expect(code).To(Equal(http.StatusAccepted))

			Eventually(func() *int {
				resp := commandAPI.Get(fmt.Sprintf("/commands/%s/status", result.CommandID))
				var status sidecarapi.CommandStatusResponse
				Expect(json.NewDecoder(resp.Body).Decode(&status)).To(Succeed())
				return status.ExitCode
			}, "5s").ShouldNot(BeNil())

			resp := commandAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
			Expect(resp.Body.Len()).To(Equal(1024 * 1024))
		})
	})

	Describe("command start failure cleanup", func() {
		It("cleans up output directory when command builder fails", func() {
			isolatedRoot, err := os.MkdirTemp("", "sidecar-test-builder-fail-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = os.RemoveAll(isolatedRoot) })

			_, isolatedAPI := humatest.New(GinkgoT(), huma.DefaultConfig("Builder Fail API", "1.0.0"))
			isolatedMock := &MockProcFS{rootDir: isolatedRoot, cwd: testCwd}
			failBuilder := &FailingCommandBuilder{err: fmt.Errorf("build error")}
			isolatedHandlers := NewCommandHandlers(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), isolatedMock, failBuilder)
			RegisterCommandRoutes(isolatedAPI, isolatedHandlers)

			resp := isolatedAPI.Post("/commands", "Content-Type: application/json",
				strings.NewReader(`{"cmd": "echo"}`))
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
			_, envFailAPI := humatest.New(GinkgoT(), huma.DefaultConfig("Env Fail API", "1.0.0"))
			envFailMock := &MockProcFS{
				rootDir:       testRootDir,
				cwd:           testCwd,
				getEnvironErr: fmt.Errorf("permission denied"),
			}
			envFailHandlers := NewCommandHandlers(slog.New(slog.NewTextHandler(GinkgoWriter, nil)), envFailMock, &DirectCommandBuilder{})
			RegisterCommandRoutes(envFailAPI, envFailHandlers)

			resp := envFailAPI.Post("/commands", "Content-Type: application/json",
				strings.NewReader(`{"cmd": "echo", "args": ["-n", "ok"]}`))
			Expect(resp.Code).To(Equal(http.StatusAccepted))

			var result sidecarapi.CreateCommandResponse
			Expect(json.NewDecoder(resp.Body).Decode(&result)).To(Succeed())

			Eventually(func() string {
				resp := envFailAPI.Get(fmt.Sprintf("/commands/%s/stdout", result.CommandID))
				return resp.Body.String()
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
