package handlers

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests validate the exec handler's validation logic and helpers
// without requiring a real PTY or WebSocket connection.

var _ = Describe("Exec", func() {
	Describe("appendOrReplaceTERM", func() {
		It("adds TERM when not present", func() {
			env := []string{"PATH=/usr/bin", "HOME=/root"}
			result := appendOrReplaceTERM(env)
			Expect(result).To(ContainElement("TERM=xterm-256color"))
			Expect(result).To(HaveLen(3))
		})

		It("overrides existing TERM", func() {
			env := []string{"PATH=/usr/bin", "TERM=dumb", "HOME=/root"}
			result := appendOrReplaceTERM(env)
			Expect(result).To(ContainElement("TERM=xterm-256color"))
			Expect(result).NotTo(ContainElement("TERM=dumb"))
			Expect(result).To(HaveLen(3))
		})

		It("handles empty environ", func() {
			result := appendOrReplaceTERM(nil)
			Expect(result).To(Equal([]string{"TERM=xterm-256color"}))
		})
	})

	Describe("handleResize", func() {
		// handleResize requires a real PTY fd, so we only test the JSON parsing path
		// via the validation logic. The actual pty.Setsize call is tested in integration.
	})

	Describe("HandleExec input validation", func() {
		// We can't easily test HandleExec with humatest since it requires WebSocket.
		// Instead, test the validation logic that happens before the StreamResponse.

		It("rejects relative shell path", func() {
			input := &ExecInput{Shell: "bin/sh", Cols: 80, Rows: 24}
			_, err := testExecHandlers.HandleExec(testExecCtx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("absolute path"))
		})

		It("rejects shell path with ..", func() {
			input := &ExecInput{Shell: "/bin/../bin/sh", Cols: 80, Rows: 24}
			_, err := testExecHandlers.HandleExec(testExecCtx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("absolute path"))
		})

		It("rejects shell path with null byte", func() {
			input := &ExecInput{Shell: "/bin/sh\x00", Cols: 80, Rows: 24}
			_, err := testExecHandlers.HandleExec(testExecCtx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("absolute path"))
		})

		It("rejects cols=0", func() {
			input := &ExecInput{Shell: "/bin/sh", Cols: 0, Rows: 24}
			_, err := testExecHandlers.HandleExec(testExecCtx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cols/rows"))
		})

		It("rejects rows=0", func() {
			input := &ExecInput{Shell: "/bin/sh", Cols: 80, Rows: 0}
			_, err := testExecHandlers.HandleExec(testExecCtx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cols/rows"))
		})

		It("rejects cols>1000", func() {
			input := &ExecInput{Shell: "/bin/sh", Cols: 1001, Rows: 24}
			_, err := testExecHandlers.HandleExec(testExecCtx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cols/rows"))
		})

		It("accepts valid input and returns stream response", func() {
			input := &ExecInput{Shell: "/bin/sh", Cols: 80, Rows: 24}
			resp, err := testExecHandlers.HandleExec(testExecCtx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
			Expect(resp.Body).NotTo(BeNil())
		})
	})
})

// Verify test compiles but avoid duplicate TestHandlers.
var _ = func() {
	_ = testing.T{}
}
