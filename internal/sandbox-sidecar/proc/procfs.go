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

package proc

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/isola-run/isola/internal/constants"
)

// ErrContainerNotFound is returned when the marked container PID cannot be found.
var ErrContainerNotFound = errors.New("container not found: no process with ISOLA_CONTAINER_NAME")

// ProcFS abstracts /proc filesystem operations for finding container processes.
type ProcFS interface {
	// FindMarkedPID scans /proc/*/environ for a process with ISOLA_CONTAINER_NAME=<containerName>.
	// If containerName is empty and there is only one container, it will return the PID of that container.
	// Otherwise, return an error.
	FindMarkedPID(containerName string) (int, error)
	// GetCwd reads the /proc/<pid>/cwd symlink to get the process working directory.
	GetCwd(pid int) (string, error)
	// GetRoot returns the path to /proc/<pid>/root for the given PID.
	GetRoot(pid int) string
	// GetUIDGID reads the real UID and GID by stat'ing /proc/<pid>.
	GetUIDGID(pid int) (uid, gid int, err error)
	// GetEnviron reads /proc/<pid>/environ and returns all environment variables as "KEY=VALUE" strings.
	GetEnviron(pid int) ([]string, error)
}

// RealProcFS implements ProcFS using the actual /proc filesystem.
type RealProcFS struct{}

// FindMarkedPID scans /proc for a process with ISOLA_CONTAINER_NAME in its environment.
// If containerName is specified, it finds the process with that exact container name.
// If containerName is empty and there is exactly one container, it returns that container's PID.
func (r *RealProcFS) FindMarkedPID(containerName string) (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("read /proc: %w", err)
	}

	var foundPID int
	seenNames := make(map[string]struct{})

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue // not a PID directory
		}

		name, ok := GetContainerName(pid)
		if !ok {
			continue
		}

		if containerName != "" {
			if name == containerName {
				return pid, nil
			}
		} else {
			if _, seen := seenNames[name]; !seen {
				seenNames[name] = struct{}{}
				foundPID = pid
			}
		}
	}

	if containerName != "" {
		return 0, ErrContainerNotFound
	}

	if len(seenNames) == 0 {
		return 0, ErrContainerNotFound
	}
	if len(seenNames) > 1 {
		return 0, ErrContainerNotFound
	}

	// if no container name is specified and only one container is found, return that container's PID
	return foundPID, nil
}

// GetContainerName returns the ISOLA_CONTAINER_NAME value if present in the process environment.
func GetContainerName(pid int) (string, bool) {
	environPath := fmt.Sprintf("/proc/%d/environ", pid)
	data, err := os.ReadFile(environPath) //nolint:gosec // path is constructed from trusted PID
	if err != nil {
		return "", false
	}
	return containerNameFromEnviron(data)
}

func containerNameFromEnviron(data []byte) (string, bool) {
	prefix := []byte(constants.IsolaContainerNameEnv + "=")
	for entry := range bytes.SplitSeq(data, []byte{0}) {
		if bytes.HasPrefix(entry, prefix) {
			return string(entry[len(prefix):]), true
		}
	}
	return "", false
}

// GetCwd reads the cwd symlink for the given PID.
func (r *RealProcFS) GetCwd(pid int) (string, error) {
	cwdPath := fmt.Sprintf("/proc/%d/cwd", pid)
	target, err := os.Readlink(cwdPath)
	if err != nil {
		return "", fmt.Errorf("read cwd symlink: %w", err)
	}
	return target, nil
}

// GetRoot returns the path to /proc/<pid>/root.
func (r *RealProcFS) GetRoot(pid int) string {
	return filepath.Join("/proc", strconv.Itoa(pid), "root")
}

// GetUIDGID returns the UID and GID of the process by stat'ing /proc/<pid>.
// The /proc/<pid> directory is owned by the user running the process.
func (r *RealProcFS) GetUIDGID(pid int) (uid, gid int, err error) {
	procPath := fmt.Sprintf("/proc/%d", pid)
	var stat unix.Stat_t
	if err := unix.Stat(procPath, &stat); err != nil {
		return 0, 0, fmt.Errorf("stat %s: %w", procPath, err)
	}
	return int(stat.Uid), int(stat.Gid), nil
}

// GetEnviron reads all environment variables from /proc/<pid>/environ.
func (r *RealProcFS) GetEnviron(pid int) ([]string, error) {
	environPath := fmt.Sprintf("/proc/%d/environ", pid)
	data, err := os.ReadFile(environPath) //nolint:gosec // path is constructed from trusted PID
	if err != nil {
		return nil, fmt.Errorf("read environ: %w", err)
	}

	var env []string
	for entry := range bytes.SplitSeq(data, []byte{0}) {
		if len(entry) > 0 {
			env = append(env, string(entry))
		}
	}
	return env, nil
}
