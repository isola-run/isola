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

package gvisorinstaller

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// HostExec runs a command in the host's mount namespace, so host binaries
// (containerd, systemctl) and host paths resolve as the node sees them.
type HostExec interface {
	// Run executes the command and returns its stdout. Stderr is included in
	// the returned error on failure.
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// nsenterExec enters PID 1's mount namespace via nsenter. PID 1 is the host's
// init because the DaemonSet pod runs with hostPID: true (and privileged, so
// setns is permitted). This is the same mechanism kata-deploy uses.
type nsenterExec struct{}

// NewNsenterExec returns the production HostExec implementation.
func NewNsenterExec() HostExec { return nsenterExec{} }

func (nsenterExec) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	nsenterArgs := append([]string{"--target", "1", "--mount", "--", name}, args...)
	cmd := exec.CommandContext(ctx, "nsenter", nsenterArgs...) //nolint:gosec // running host commands via nsenter is this component's purpose; callers pass fixed binaries
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("host command %q failed: %w (stderr: %s)", name, err, truncateOutput(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}

// truncateOutput bounds command output embedded in errors (which end up in
// node events and logs).
func truncateOutput(b []byte) string {
	const maxLen = 1024
	s := string(bytes.TrimSpace(b))
	if len(s) > maxLen {
		return s[:maxLen] + "...(truncated)"
	}
	return s
}
