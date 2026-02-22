package command

import (
	"context"
	"os"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sidecarapi "github.com/isola-ai/isola-sb/internal/sidecar-api"
)

var (
	testRootDir string
	testCwd     string
)

// DirectCommandBuilder runs commands directly without nsenter (for testing).
// This mimics nsenter's behavior when --pid is NOT specified: nsenter calls
// execvp() directly (no fork), so the Go child process IS the target command.
type DirectCommandBuilder struct{}

func (b *DirectCommandBuilder) Build(ctx context.Context, _ int, req sidecarapi.CreateCommandRequest, env []string, stdoutFile, stderrFile *os.File) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, req.Args[0], req.Args[1:]...) //nolint:gosec // test-only builder; inputs are test fixtures
	cmd.Stdout = stdoutFile
	cmd.Stderr = stderrFile
	cmd.Env = env
	cmd.WaitDelay = waitDelayGracePeriod
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	return cmd, nil
}

// MockProcFS implements proc.ProcFS for testing.
type MockProcFS struct {
	rootDir       string
	cwd           string
	uid           int
	gid           int
	findMarkedErr error
	getEnvironErr error
}

func (m *MockProcFS) FindMarkedPID(containerName string) (int, error) {
	if m.findMarkedErr != nil {
		return 0, m.findMarkedErr
	}
	return 1, nil
}

func (m *MockProcFS) GetCwd(pid int) (string, error) {
	return m.cwd, nil
}

func (m *MockProcFS) GetRoot(pid int) string {
	return m.rootDir
}

func (m *MockProcFS) GetUIDGID(pid int) (int, int, error) {
	return m.uid, m.gid, nil
}

func (m *MockProcFS) GetEnviron(pid int) ([]string, error) {
	if m.getEnvironErr != nil {
		return nil, m.getEnvironErr
	}
	return []string{"PATH=/usr/bin:/bin", "HOME=/root"}, nil
}

// FailingCommandBuilder is a CommandBuilder that always returns an error.
type FailingCommandBuilder struct {
	err error
}

func (b *FailingCommandBuilder) Build(_ context.Context, _ int, _ sidecarapi.CreateCommandRequest, _ []string, _ *os.File, _ *os.File) (*exec.Cmd, error) {
	return nil, b.err
}

func TestCommand(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sidecar Command Suite")
}

var _ = BeforeSuite(func() {
	// Create temp directories for testing
	var err error
	testRootDir, err = os.MkdirTemp("", "sidecar-test-root-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = os.RemoveAll(testRootDir) })

	testCwd = "/workspace"
	// Create the cwd directory in the mock root
	err = os.MkdirAll(testRootDir+testCwd, 0750)
	Expect(err).NotTo(HaveOccurred())
})
